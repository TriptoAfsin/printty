// Package queue owns the job lifecycle: approval, printing, holding while the
// printer is off, the manual-duplex flip, expiry, and the admin's alerts.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/TriptoAfsin/printty/internal/auth"
	"github.com/TriptoAfsin/printty/internal/memtrim"
	"github.com/TriptoAfsin/printty/internal/notify"
	"github.com/TriptoAfsin/printty/internal/printer"
	"github.com/TriptoAfsin/printty/internal/store"
)

type Queue struct {
	st        *store.Store
	be        printer.Backend
	tg        *notify.Telegram
	publicURL string

	wake chan struct{}

	statusMu   sync.Mutex
	status     printer.Status
	statusAt   time.Time
	offlineMsg time.Time // last "printer is off" alert

	deskMu  sync.Mutex
	deskSeq uint64
	desk    []DesktopAlert // the last few, for the tray icon to show
}

// DesktopAlert is a Windows notification shown by the tray icon.
type DesktopAlert struct {
	Seq   uint64 `json:"seq"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// desktop records an alert for the tray icon (in this process, or the
// "printty tray" helper of a service, which polls for it).
func (q *Queue) desktop(title, text string) {
	if q.st.Settings().DesktopOff {
		return
	}
	q.deskMu.Lock()
	defer q.deskMu.Unlock()
	q.deskSeq++
	q.desk = append(q.desk, DesktopAlert{Seq: q.deskSeq, Title: title, Text: text})
	if len(q.desk) > 10 {
		q.desk = q.desk[len(q.desk)-10:]
	}
}

// DesktopAlerts returns the alerts after seq, and the newest seq.
func (q *Queue) DesktopAlerts(since uint64) (uint64, []DesktopAlert) {
	q.deskMu.Lock()
	defer q.deskMu.Unlock()
	var out []DesktopAlert
	for _, a := range q.desk {
		if a.Seq > since {
			out = append(out, a)
		}
	}
	return q.deskSeq, out
}

func New(st *store.Store, be printer.Backend, tg *notify.Telegram, publicURL string) *Queue {
	q := &Queue{st: st, be: be, tg: tg, publicURL: strings.TrimRight(publicURL, "/"), wake: make(chan struct{}, 1)}
	if tg != nil {
		tg.OnCallback = q.handleCallback
	}
	return q
}

// Kick wakes the worker.
func (q *Queue) Kick() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Printer returns the printer Printty uses: the configured one or the OS default.
func (q *Queue) Printer(ctx context.Context) string {
	if p := q.st.Settings().Printer; p != "" {
		return p
	}
	// Windows moves its default to whichever printer was used last, so pick a
	// real printer once and remember it rather than following the default.
	ps, _ := q.be.Printers(ctx)
	pick := ""
	for _, p := range ps {
		if p.Default && !virtualPrinter(p.Name) {
			pick = p.Name
		}
	}
	for _, p := range ps {
		if pick == "" && !virtualPrinter(p.Name) {
			pick = p.Name
		}
	}
	if pick == "" && len(ps) > 0 {
		return ps[0].Name // only virtual printers: use one, but don't save it
	}
	if pick != "" {
		q.st.UpdateSettings(func(s *store.Settings) { s.Printer = pick })
	}
	return pick
}

func virtualPrinter(name string) bool {
	n := strings.ToLower(name)
	for _, v := range []string{"print to pdf", "xps document writer", "onenote", "fax", "pdf printer", "cups-pdf"} {
		if strings.Contains(n, v) {
			return true
		}
	}
	return false
}

// Status returns the printer status, cached for a few seconds.
func (q *Queue) Status(ctx context.Context) printer.Status {
	q.statusMu.Lock()
	defer q.statusMu.Unlock()
	if time.Since(q.statusAt) < 5*time.Second {
		return q.status
	}
	name := q.Printer(ctx)
	if name == "" {
		q.status = printer.Status{State: printer.Unknown, Detail: "No printer found."}
	} else if s, err := q.be.Status(ctx, name); err == nil {
		q.status = s
	} else {
		q.status = printer.Status{State: printer.Unknown, Detail: err.Error()}
	}
	q.statusAt = time.Now()
	return q.status
}

func (q *Queue) invalidateStatus() {
	q.statusMu.Lock()
	q.statusAt = time.Time{}
	q.statusMu.Unlock()
}

// ---- submission and decisions

// Submit moves a draft to pending or queued depending on who sent it.
func (q *Queue) Submit(ctx context.Context, id string, autoApprove bool, by string) (store.Job, error) {
	printerName := q.Printer(ctx) // outside UpdateJob: it takes the store lock itself
	j, err := q.st.UpdateJob(id, func(j *store.Job) bool {
		if j.State != store.JobDraft {
			return false
		}
		if j.Printer == "" {
			j.Printer = printerName
		}
		if autoApprove {
			j.State, j.DecidedBy = store.JobQueued, by
		} else {
			j.State = store.JobPending
		}
		return true
	})
	if err != nil {
		return j, err
	}
	if j.State == store.JobPending {
		q.alertRequest(ctx, j)
	} else if j.State == store.JobQueued {
		q.alertIncoming(ctx, j)
		q.Kick()
	}
	return j, nil
}

func (q *Queue) Approve(id, by string) (store.Job, error) {
	j, err := q.st.UpdateJob(id, func(j *store.Job) bool {
		if j.State != store.JobPending {
			return false
		}
		j.State, j.DecidedBy = store.JobQueued, by
		return true
	})
	if err == nil && j.State != store.JobQueued {
		return j, fmt.Errorf("this request was already %s", humanState(j.State))
	}
	q.Kick()
	return j, err
}

func (q *Queue) Reject(id, by, reason string) (store.Job, error) {
	j, err := q.st.UpdateJob(id, func(j *store.Job) bool {
		if j.State != store.JobPending {
			return false
		}
		j.State, j.DecidedBy, j.Reason = store.JobRejected, by, reason
		return true
	})
	if err == nil && j.State != store.JobRejected {
		return j, fmt.Errorf("this request was already %s", humanState(j.State))
	}
	q.removeFile(j)
	return j, err
}

// ContinueFlip starts the second pass of a manual-duplex job.
func (q *Queue) ContinueFlip(id string) (store.Job, error) {
	j, err := q.st.UpdateJob(id, func(j *store.Job) bool {
		if j.State != store.JobFlip {
			return false
		}
		j.State, j.Phase, j.Spooled, j.Notified = store.JobQueued, 2, false, false
		return true
	})
	if err == nil && j.State != store.JobQueued {
		return j, fmt.Errorf("this job isn't waiting for a flip")
	}
	q.Kick()
	return j, err
}

func (q *Queue) Cancel(id string) (store.Job, error) {
	j, err := q.st.UpdateJob(id, func(j *store.Job) bool {
		if j.State.Finished() || (j.State == store.JobPrinting && j.Spooled) {
			return false
		}
		j.State = store.JobCanceled
		return true
	})
	if err == nil && j.State != store.JobCanceled {
		return j, fmt.Errorf("this job can't be cancelled now")
	}
	q.removeFile(j)
	return j, err
}

func (q *Queue) removeFile(j store.Job) {
	if j.StoredAs != "" && (j.State.Finished()) {
		os.Remove(q.st.FilePath(j))
	}
}

func humanState(s store.JobState) string {
	switch s {
	case store.JobPending:
		return "waiting for approval"
	case store.JobQueued, store.JobPrinting:
		return "approved"
	case store.JobHeld:
		return "held"
	case store.JobFlip:
		return "waiting for a flip"
	case store.JobDone:
		return "printed"
	}
	return string(s)
}

// ---- worker

// Run processes jobs until ctx ends. It sleeps long when there's nothing to do.
func (q *Queue) Run(ctx context.Context) {
	if q.tg != nil {
		go q.tg.Poll(ctx)
	}
	for {
		active := q.step(ctx)
		wait := 2 * time.Minute
		if active {
			wait = 4 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-time.After(wait):
		}
	}
}

// step does one pass and reports whether any job is in flight.
func (q *Queue) step(ctx context.Context) (active bool) {
	q.expire()
	set := q.st.Settings()
	jobs := q.st.Jobs(func(j store.Job) bool {
		return j.State == store.JobQueued || j.State == store.JobPrinting || j.State == store.JobHeld
	})
	if len(jobs) == 0 {
		return false
	}
	// Oldest first.
	for i, k := 0, len(jobs)-1; i < k; i, k = i+1, k-1 {
		jobs[i], jobs[k] = jobs[k], jobs[i]
	}
	printed := false
	for _, j := range jobs {
		if ctx.Err() != nil {
			return true
		}
		if j.Spooled {
			q.track(ctx, j)
			continue
		}
		// Not yet handed to the OS: print if the printer looks ready.
		q.invalidateStatus()
		st := q.Status(ctx)
		if st.State != printer.Ready && st.State != printer.Unknown {
			q.hold(ctx, j, st.Detail)
			continue
		}
		q.print(ctx, j, set)
		printed = true
	}
	if printed {
		memtrim.Now()
	}
	return true
}

func (q *Queue) print(ctx context.Context, j store.Job, set store.Settings) {
	pages, err := printer.ParsePages(j.Options.Pages, max(j.DocPages, 1))
	if err != nil {
		q.fail(ctx, j, err.Error())
		return
	}
	pass := pages
	phase := 0
	manual := (j.Options.Duplex == printer.LongEdge || j.Options.Duplex == printer.ShortEdge) && !set.AutoDuplex && len(pages) > 1
	if manual {
		fronts, backs := printer.SplitDuplex(pages, set.FlipReverse)
		if j.Phase == 2 {
			pass, phase = backs, 2
		} else {
			pass, phase = fronts, 1
		}
	}
	opts := j.Options
	if manual {
		opts.Duplex = printer.Simplex
	}
	j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool {
		x.State, x.Phase, x.Reason, x.Total = store.JobPrinting, phase, "", len(pass)*max(opts.Copies, 1)
		return true
	})
	err = q.be.Print(ctx, printer.Job{
		ID: j.ID, Title: j.FileName, Path: q.st.FilePath(j), Kind: j.Kind, Printer: j.Printer,
		Options: opts, PageList: pass,
	})
	if err != nil {
		slog.Error("print failed", "job", j.ID, "err", err)
		q.fail(ctx, j, "The printer driver refused the job: "+err.Error())
		return
	}
	q.st.UpdateJob(j.ID, func(x *store.Job) bool { x.Spooled = true; return true })
	slog.Info("job spooled", "job", j.ID, "pages", len(pass), "phase", phase)
}

// track follows a spooled job until the OS finishes it.
func (q *Queue) track(ctx context.Context, j store.Job) {
	s, err := q.be.Spool(ctx, j.Printer, j.ID)
	if err != nil {
		return
	}
	switch s {
	case printer.SpoolGone:
		if j.Phase == 1 {
			j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool {
				x.State, x.Spooled, x.Notified = store.JobFlip, false, false
				return true
			})
			q.alertFlip(ctx, j)
			return
		}
		j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool {
			x.State, x.Spooled, x.Reason, x.Progress = store.JobDone, false, "", x.Total
			return true
		})
		if j.OwnerID != "" {
			q.st.UpdateUser(j.OwnerID, func(u *store.User) { u.LastPrint = time.Now() })
		}
		q.removeFile(j)
	case printer.SpoolStuck:
		if j.State != store.JobHeld {
			q.hold(ctx, j, "The printer seems to be off. It will print when it's back on.")
		}
	case printer.SpoolWaiting:
		if j.State == store.JobHeld {
			q.st.UpdateJob(j.ID, func(x *store.Job) bool { x.State, x.Reason = store.JobPrinting, ""; return true })
		}
	}
}

func (q *Queue) hold(ctx context.Context, j store.Job, why string) {
	if why == "" {
		why = "The printer isn't ready."
	}
	j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool {
		if x.State == store.JobHeld && x.Reason == why {
			return false
		}
		x.State, x.Reason = store.JobHeld, why
		return true
	})
	if !j.Notified {
		q.alertOffline(ctx, why)
		q.st.UpdateJob(j.ID, func(x *store.Job) bool { x.Notified = true; return true })
	}
}

func (q *Queue) fail(ctx context.Context, j store.Job, why string) {
	j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool {
		x.State, x.Reason, x.Spooled = store.JobFailed, why, false
		return true
	})
	q.removeFile(j)
	q.send(ctx, fmt.Sprintf("Printing %s failed.\n%s", j.FileName, why))
}

// expire ends requests nobody acted on and drafts nobody finished.
func (q *Queue) expire() {
	set := q.st.Settings()
	ttl := time.Duration(max(set.RequestTTLHours, 1)) * time.Hour
	for _, j := range q.st.Jobs(func(j store.Job) bool {
		return (j.State == store.JobPending && time.Since(j.CreatedAt) > ttl) ||
			(j.State == store.JobDraft && time.Since(j.CreatedAt) > time.Hour)
	}) {
		j, _ = q.st.UpdateJob(j.ID, func(x *store.Job) bool { x.State = store.JobExpired; return true })
		q.removeFile(j)
	}
	for _, f := range q.st.Prune(time.Duration(max(set.KeepHistoryHours, 1)) * time.Hour) {
		os.Remove(q.st.FilesDir() + string(os.PathSeparator) + f)
	}
}

// ---- alerts

func (q *Queue) send(ctx context.Context, text string, rows ...[]notify.Button) {
	if q.tg == nil || !q.st.Settings().NotifyTelegram {
		return
	}
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := q.tg.Send(c, text, rows...); err != nil {
			slog.Warn("telegram send failed", "err", err)
		}
	}()
}

func (q *Queue) link(path string) string {
	if q.publicURL == "" {
		return ""
	}
	return q.publicURL + path
}

func (q *Queue) action(a, id string) string {
	return a + ":" + id + ":" + auth.Sign(q.st.Secret(), a+":"+id)
}

func who(j store.Job, st *store.Store) string {
	if j.OwnerID != "" {
		if u, err := st.User(j.OwnerID); err == nil {
			return u.Name
		}
	}
	if j.GuestName != "" {
		return j.GuestName + " (guest)"
	}
	return "Someone"
}

func describe(j store.Job) string {
	o := j.Options
	parts := []string{plural(j.DocPages, "page")}
	if o.Pages != "" {
		parts[0] = "pages " + o.Pages + " of " + fmt.Sprint(j.DocPages)
	}
	if o.Color {
		parts = append(parts, "colour")
	} else {
		parts = append(parts, "black and white")
	}
	parts = append(parts, o.Paper)
	if o.Duplex != printer.Simplex && o.Duplex != "" {
		parts = append(parts, "two-sided")
	}
	if o.Copies > 1 {
		parts = append(parts, fmt.Sprintf("%d copies", o.Copies))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return fmt.Sprintf("%d %ss", n, w)
}

func (q *Queue) alertRequest(ctx context.Context, j store.Job) {
	text := fmt.Sprintf("New print request from %s\n%s\n%s", who(j, q.st), j.FileName, describe(j))
	if j.Note != "" {
		text += "\nNote: \"" + j.Note + "\""
	}
	text += "\n\nApprove when the printer is on and has paper."
	q.desktop("Print request from "+who(j, q.st), j.FileName+"\n"+describe(j))
	q.send(ctx, text,
		[]notify.Button{{Text: "Approve", Data: q.action("a", j.ID), URL: q.link("/admin")}, {Text: "Reject", Data: q.action("r", j.ID)}},
	)
}

func (q *Queue) alertIncoming(ctx context.Context, j store.Job) {
	if !q.st.Settings().NotifyIncoming {
		return
	}
	q.desktop("Printing for "+who(j, q.st), j.FileName+"\n"+describe(j)+"\nMake sure the printer is on and has paper.")
	q.send(ctx, fmt.Sprintf("Printing now for %s\n%s\n%s\n\nMake sure the printer is on and has paper.", who(j, q.st), j.FileName, describe(j)))
}

func (q *Queue) alertOffline(ctx context.Context, why string) {
	q.statusMu.Lock()
	recent := time.Since(q.offlineMsg) < 30*time.Minute
	if !recent {
		q.offlineMsg = time.Now()
	}
	q.statusMu.Unlock()
	if recent {
		return
	}
	n := len(q.st.Jobs(func(j store.Job) bool { return j.State == store.JobHeld || j.State == store.JobQueued }))
	q.desktop("The printer seems to be off", plural(max(n, 1), "job")+" waiting. Switch it on and they print by themselves.")
	q.send(ctx, fmt.Sprintf("The printer seems to be off.\n%s\n%s waiting. Please switch it on.", why, plural(max(n, 1), "job")))
}

func (q *Queue) alertFlip(ctx context.Context, j store.Job) {
	q.desktop("Flip needed: "+j.FileName, "The fronts are printed. Put the stack back in the tray, then press Continue on the Requests page.")
	q.send(ctx, fmt.Sprintf("Flip needed: %s\nThe front sides are printed. Put the stack back in the tray as calibrated, then continue.", j.FileName),
		[]notify.Button{{Text: "Continue", Data: q.action("c", j.ID), URL: q.link("/admin")}, {Text: "Cancel", Data: q.action("x", j.ID)}},
	)
}

// handleCallback runs a Telegram button press.
func (q *Queue) handleCallback(data string, fromID int64) (string, error) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || !auth.Verify(q.st.Secret(), parts[0]+":"+parts[1], parts[2]) {
		return "", errors.New("This button has expired")
	}
	by := fmt.Sprintf("telegram:%d", fromID)
	var err error
	var msg string
	switch parts[0] {
	case "a":
		_, err = q.Approve(parts[1], by)
		msg = "Approved. Printing now."
	case "r":
		_, err = q.Reject(parts[1], by, "")
		msg = "Rejected."
	case "c":
		_, err = q.ContinueFlip(parts[1])
		msg = "Printing the back sides."
	case "x":
		_, err = q.st.UpdateJob(parts[1], func(x *store.Job) bool {
			if x.State != store.JobFlip {
				return false
			}
			x.State = store.JobCanceled
			return true
		})
		msg = "Cancelled."
	default:
		return "", errors.New("Unknown action")
	}
	return msg, err
}
