package printer

import "testing"

func TestCapsOption(t *testing.T) {
	caps := func(keys ...string) Caps {
		var c Caps
		for _, k := range keys {
			c.Options = append(c.Options, DriverOption{Key: k})
		}
		return c
	}
	for _, tc := range []struct {
		name  string
		caps  Caps
		which string
		want  string // key found, "" for none
	}{
		{"windows quality", caps("media", "source", "quality"), OptQuality, "quality"},
		{"windows media", caps("media", "source", "quality"), OptMedia, "media"},
		{"cups quality", caps("MediaType", "cupsPrintQuality"), OptQuality, "cupsPrintQuality"},
		{"cups media", caps("InputSlot", "MediaType"), OptMedia, "MediaType"},
		{"no quality", caps("media", "source"), OptQuality, ""},
		{"source is not media", caps("source"), OptMedia, ""},
		{"no options", Caps{}, OptQuality, ""},
	} {
		got := tc.caps.Option(tc.which)
		switch {
		case tc.want == "" && got != nil:
			t.Errorf("%s: got %q, want none", tc.name, got.Key)
		case tc.want != "" && (got == nil || got.Key != tc.want):
			t.Errorf("%s: got %v, want %q", tc.name, got, tc.want)
		}
	}
}
