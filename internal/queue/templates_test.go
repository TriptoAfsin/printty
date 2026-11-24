package queue

import "testing"

func TestRender(t *testing.T) {
	tests := []struct {
		name, tmpl string
		vals       map[string]string
		want       string
	}{
		{"fills placeholders", "Hi {who}, {file}", map[string]string{"who": "Mina", "file": "a.pdf"}, "Hi Mina, a.pdf"},
		{"drops a line whose placeholders are empty", "📄 {file}\n💬 {note}\nend", map[string]string{"file": "a.pdf", "note": ""}, "📄 a.pdf\nend"},
		{"keeps a line with one filled placeholder", "{who} {note}", map[string]string{"who": "Mina", "note": ""}, "Mina"},
		{"keeps plain lines and unknown placeholders", "Hello\n{nope}", map[string]string{"who": "x"}, "Hello\n{nope}"},
		{"emoji survive", "✅ {who} 🎉", map[string]string{"who": "Mina"}, "✅ Mina 🎉"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(tc.tmpl, tc.vals); got != tc.want {
				t.Errorf("render() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultsRender(t *testing.T) {
	for _, tm := range Templates {
		vals := map[string]string{}
		for _, f := range tm.Fields {
			vals[f] = "x"
		}
		if got := render(tm.Default, vals); got == "" {
			t.Errorf("%s renders empty", tm.Key)
		}
	}
}
