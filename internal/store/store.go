// Package store keeps Printty's state (users, sessions, invites, jobs, settings)
// in one small JSON file, written atomically. No database server.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/TriptoAfsin/printty/internal/printer"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Name      string    `json:"name"`
	PassHash  string    `json:"passHash"`
	Role      Role      `json:"role"`
	Trusted   bool      `json:"trusted"` // auto-approved
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
	LastPrint time.Time `json:"lastPrint,omitempty"`
}

type Session struct {
	TokenHash string    `json:"tokenHash"`
	UserID    string    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

type Invite struct {
	TokenHash string    `json:"tokenHash"`
	CreatedBy string    `json:"createdBy"`
	Note      string    `json:"note"`
	Expires   time.Time `json:"expires"`
	UsedBy    string    `json:"usedBy,omitempty"`
	// NeedsApproval: the member's prints wait for the owner. Off by default:
	// invited people print directly; guests always need approval.
	NeedsApproval bool `json:"needsApproval,omitempty"`
}

type JobState string

const (
	JobDraft    JobState = "draft"            // shared/uploaded, options not confirmed yet
	JobPending  JobState = "pending_approval" // waiting for the admin
	JobQueued   JobState = "queued"           // approved, waiting for the printer worker
	JobPrinting JobState = "printing"         // being rendered / in the OS spooler
	JobHeld     JobState = "held"             // printer off or stuck; resumes by itself
	JobFlip     JobState = "awaiting_flip"    // manual duplex: fronts done
	JobDone     JobState = "done"
	JobFailed   JobState = "failed"
	JobRejected JobState = "rejected"
	JobExpired  JobState = "expired"
	JobCanceled JobState = "canceled"
)

// Finished reports whether the job will never print again.
func (s JobState) Finished() bool {
	switch s {
	case JobDone, JobFailed, JobRejected, JobExpired, JobCanceled:
		return true
	}
	return false
}

type Job struct {
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId,omitempty"` // empty for guests
	GuestName string          `json:"guestName,omitempty"`
	GuestKey  string          `json:"guestKey,omitempty"` // cookie that lets a guest see their jobs
	Note      string          `json:"note,omitempty"`
	ClientIP  string          `json:"clientIp,omitempty"`
	FileName  string          `json:"fileName"`
	Kind      string          `json:"kind"` // pdf, png, jpeg
	StoredAs  string          `json:"storedAs"`
	Size      int64           `json:"size"`
	DocPages  int             `json:"docPages"`
	Options   printer.Options `json:"options"`
	Printer   string          `json:"printer"`
	State     JobState        `json:"state"`
	Reason    string          `json:"reason,omitempty"` // why held/failed/rejected
	Phase     int             `json:"phase,omitempty"`  // manual duplex: 0 single pass, 1 fronts, 2 backs
	Progress  int             `json:"progress,omitempty"`
	Total     int             `json:"total,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	DecidedBy string          `json:"decidedBy,omitempty"`
	Notified  bool            `json:"notified,omitempty"` // offline alert sent for this hold
	Spooled   bool            `json:"spooled,omitempty"`  // the current pass is in the OS spooler
}

type Settings struct {
	Printer          string `json:"printer"`
	GuestsEnabled    bool   `json:"guestsEnabled"`
	GuestPerHour     int    `json:"guestPerHour"`
	GuestMaxPages    int    `json:"guestMaxPages"`
	MaxUploadMB      int    `json:"maxUploadMB"`
	RequestTTLHours  int    `json:"requestTTLHours"`
	FlipReverse      bool   `json:"flipReverse"`
	AutoDuplex       bool   `json:"autoDuplex"` // printer has a duplex unit
	NotifyTelegram   bool   `json:"notifyTelegram"`
	NotifyIncoming   bool   `json:"notifyIncoming"`
	DesktopOff       bool   `json:"desktopOff,omitempty"` // no Windows notifications on this PC
	Clock12          bool   `json:"clock12,omitempty"`    // show times as 9:01 PM instead of 21:01
	KeepHistoryHours int    `json:"keepHistoryHours"`
}

func DefaultSettings() Settings {
	return Settings{GuestPerHour: 3, GuestMaxPages: 20, MaxUploadMB: 50, RequestTTLHours: 24, FlipReverse: true,
		NotifyTelegram: true, NotifyIncoming: true, KeepHistoryHours: 72}
}

type state struct {
	Users    []*User    `json:"users"`
	Sessions []*Session `json:"sessions"`
	Invites  []*Invite  `json:"invites"`
	Jobs     []*Job     `json:"jobs"`
	Settings Settings   `json:"settings"`
	Secret   string     `json:"secret"` // signs Telegram callbacks
}

// Store is safe for concurrent use. Read methods return copies.
type Store struct {
	mu   sync.Mutex
	path string
	dir  string
	s    state
	// OnJobChange is called (outside the lock) after a job changes.
	OnJobChange func(Job)
}

var ErrNotFound = errors.New("not found")

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o700); err != nil {
		return nil, err
	}
	st := &Store{path: filepath.Join(dataDir, "state.json"), dir: dataDir}
	st.s.Settings = DefaultSettings()
	b, err := os.ReadFile(st.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &st.s); err != nil {
			return nil, fmt.Errorf("read %s: %w", st.path, err)
		}
	}
	if st.s.Secret == "" {
		st.s.Secret = RandomToken(32)
	}
	return st, st.saveLocked()
}

func (st *Store) FilesDir() string { return filepath.Join(st.dir, "files") }

func (st *Store) FilePath(j Job) string { return filepath.Join(st.FilesDir(), j.StoredAs) }

// PreviewDir holds a job's page previews (1.png, 2.png...).
func (st *Store) PreviewDir(jobID string) string {
	return filepath.Join(st.FilesDir(), jobID+".pages")
}

func (st *Store) saveLocked() error {
	b, err := json.MarshalIndent(&st.s, "", " ")
	if err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// RandomToken returns n random bytes as hex.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func NewID() string { return RandomToken(6) }

func (st *Store) Secret() string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Secret
}

// ---- settings

func (st *Store) Settings() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Settings
}

func (st *Store) UpdateSettings(fn func(*Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s.Settings)
	return st.saveLocked()
}

// ---- users

func (st *Store) UserCount() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.s.Users)
}

func (st *Store) Users() []User {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]User, len(st.s.Users))
	for i, u := range st.s.Users {
		out[i] = *u
	}
	return out
}

func (st *Store) User(id string) (User, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, u := range st.s.Users {
		if u.ID == id {
			return *u, nil
		}
	}
	return User{}, ErrNotFound
}

func (st *Store) UserByName(username string) (User, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, u := range st.s.Users {
		if u.Username == username {
			return *u, nil
		}
	}
	return User{}, ErrNotFound
}

func (st *Store) AddUser(u User) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, x := range st.s.Users {
		if x.Username == u.Username {
			return fmt.Errorf("username %q is taken", u.Username)
		}
	}
	st.s.Users = append(st.s.Users, &u)
	return st.saveLocked()
}

func (st *Store) UpdateUser(id string, fn func(*User)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, u := range st.s.Users {
		if u.ID == id {
			fn(u)
			return st.saveLocked()
		}
	}
	return ErrNotFound
}

// ---- sessions

func (st *Store) AddSession(s Session) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Sessions = append(st.s.Sessions, &s)
	return st.saveLocked()
}

// SessionUser returns the user for a session token hash and refreshes LastSeen.
func (st *Store) SessionUser(tokenHash string, maxAge time.Duration) (User, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.s.Sessions {
		if s.TokenHash != tokenHash {
			continue
		}
		if time.Since(s.LastSeen) > maxAge {
			return User{}, ErrNotFound
		}
		for _, u := range st.s.Users {
			if u.ID == s.UserID && !u.Disabled {
				if time.Since(s.LastSeen) > time.Hour { // avoid a disk write per request
					s.LastSeen = time.Now()
					st.saveLocked()
				}
				return *u, nil
			}
		}
	}
	return User{}, ErrNotFound
}

func (st *Store) DeleteSession(tokenHash string) error {
	return st.deleteSessions(func(s *Session) bool { return s.TokenHash == tokenHash })
}

func (st *Store) DeleteUserSessions(userID string) error {
	return st.deleteSessions(func(s *Session) bool { return s.UserID == userID })
}

func (st *Store) deleteSessions(match func(*Session) bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	keep := st.s.Sessions[:0]
	for _, s := range st.s.Sessions {
		if !match(s) {
			keep = append(keep, s)
		}
	}
	st.s.Sessions = keep
	return st.saveLocked()
}

func (st *Store) SessionCount(userID string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, s := range st.s.Sessions {
		if s.UserID == userID {
			n++
		}
	}
	return n
}

// ---- invites

func (st *Store) AddInvite(inv Invite) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.Invites = append(st.s.Invites, &inv)
	return st.saveLocked()
}

// UseInvite marks an unused, unexpired invite as used by userID.
func (st *Store) ValidInvite(tokenHash string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, inv := range st.s.Invites {
		if inv.TokenHash == tokenHash && inv.UsedBy == "" && time.Now().Before(inv.Expires) {
			return true
		}
	}
	return false
}

func (st *Store) UseInvite(tokenHash, userID string) (Invite, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, inv := range st.s.Invites {
		if inv.TokenHash == tokenHash && inv.UsedBy == "" && time.Now().Before(inv.Expires) {
			inv.UsedBy = userID
			return *inv, st.saveLocked()
		}
	}
	return Invite{}, ErrNotFound
}

func (st *Store) PendingInvites() []Invite {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Invite
	for _, inv := range st.s.Invites {
		if inv.UsedBy == "" && time.Now().Before(inv.Expires) {
			out = append(out, *inv)
		}
	}
	return out
}

// ---- jobs

func (st *Store) AddJob(j Job) error {
	st.mu.Lock()
	st.s.Jobs = append(st.s.Jobs, &j)
	err := st.saveLocked()
	st.mu.Unlock()
	st.changed(j)
	return err
}

func (st *Store) Job(id string) (Job, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, j := range st.s.Jobs {
		if j.ID == id {
			return *j, nil
		}
	}
	return Job{}, ErrNotFound
}

// UpdateJob applies fn to the job and saves. fn returning false skips the save.
func (st *Store) UpdateJob(id string, fn func(*Job) bool) (Job, error) {
	st.mu.Lock()
	var out Job
	found := false
	for _, j := range st.s.Jobs {
		if j.ID == id {
			found = true
			if !fn(j) {
				st.mu.Unlock()
				return *j, nil
			}
			j.UpdatedAt = time.Now()
			out = *j
			break
		}
	}
	if !found {
		st.mu.Unlock()
		return Job{}, ErrNotFound
	}
	err := st.saveLocked()
	st.mu.Unlock()
	st.changed(out)
	return out, err
}

func (st *Store) changed(j Job) {
	if st.OnJobChange != nil {
		st.OnJobChange(j)
	}
}

// Jobs returns jobs matching filter, newest first.
func (st *Store) Jobs(filter func(Job) bool) []Job {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Job
	for _, j := range st.s.Jobs {
		if filter == nil || filter(*j) {
			out = append(out, *j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.After(out[b].CreatedAt) })
	return out
}

// Prune removes finished jobs older than keep and returns their stored files.
func (st *Store) Prune(keep time.Duration) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	var files []string
	kept := st.s.Jobs[:0]
	for _, j := range st.s.Jobs {
		if (j.State.Finished() || j.State == JobDraft) && time.Since(j.UpdatedAt) > keep {
			files = append(files, j.StoredAs)
			continue
		}
		kept = append(kept, j)
	}
	st.s.Jobs = kept
	// Expired invites and stale sessions go too.
	inv := st.s.Invites[:0]
	for _, i := range st.s.Invites {
		if i.UsedBy == "" && time.Now().After(i.Expires.Add(24*time.Hour)) {
			continue
		}
		inv = append(inv, i)
	}
	st.s.Invites = inv
	if len(files) > 0 {
		st.saveLocked()
	}
	return files
}
