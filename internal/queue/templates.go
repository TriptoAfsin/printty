package queue

import (
	"strings"
)

// Telegram message templates. The owner can change each one in Settings;
// an empty template means the default below. Placeholders in {braces} are
// filled in, and a line whose placeholders all come out empty is dropped
// (so "💬 {note}" disappears when there's no note).

// Template is one editable message.
type Template struct {
	Key, Label, Default string
	Fields              []string // placeholders it can use
}

// Templates lists the messages in Settings order.
var Templates = []Template{
	{Key: "request", Label: "New request (needs your approval)", Fields: []string{"who", "file", "details", "note", "pages", "link"},
		Default: "🖨️ New print request from {who}\n📄 {file}\n⚙️ {details}\n💬 {note}\n\nApprove when the printer is on and has paper."},
	{Key: "printing", Label: "Printing now (someone who prints directly)", Fields: []string{"who", "file", "details", "pages", "link"},
		Default: "✅ Printing now for {who}\n📄 {file}\n⚙️ {details}\n\nMake sure the printer is on and has paper."},
	{Key: "offline", Label: "Printer is off", Fields: []string{"reason", "waiting", "link"},
		Default: "⚠️ The printer seems to be off.\n{reason}\n⏳ {waiting} waiting. Please switch it on."},
	{Key: "flip", Label: "Flip the paper (manual two-sided)", Fields: []string{"file", "link"},
		Default: "🔄 Flip needed: {file}\nThe front sides are printed. Put the stack back in the tray as calibrated, then continue."},
}

// DefaultTemplate returns the built-in text for key.
func DefaultTemplate(key string) string {
	for _, t := range Templates {
		if t.Key == key {
			return t.Default
		}
	}
	return ""
}

// render fills a template. Unknown placeholders are left as they are.
func render(tmpl string, vals map[string]string) string {
	var out []string
	for _, line := range strings.Split(tmpl, "\n") {
		used, filled := 0, 0
		for k, v := range vals {
			ph := "{" + k + "}"
			if n := strings.Count(line, ph); n > 0 {
				used++
				if strings.TrimSpace(v) != "" {
					filled++
				}
				line = strings.ReplaceAll(line, ph, v)
			}
		}
		if used > 0 && filled == 0 {
			continue // every placeholder on this line was empty
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// message renders the owner's template for key, or the default.
func (q *Queue) message(key string, vals map[string]string) string {
	tmpl := q.st.Settings().Templates[key]
	if strings.TrimSpace(tmpl) == "" {
		tmpl = DefaultTemplate(key)
	}
	if _, ok := vals["link"]; !ok {
		vals["link"] = q.link("/admin")
	}
	return render(tmpl, vals)
}

// SampleMessage renders a template with made-up values, for the Settings
// preview and the test message.
func (q *Queue) SampleMessage(key string) string {
	return q.message(key, map[string]string{
		"who": "Mina", "file": "Lab report.pdf", "details": "4 pages, black and white, A4",
		"note": "For class tomorrow", "pages": "4", "reason": "Windows reports the printer offline.",
		"waiting": "2 jobs",
	})
}
