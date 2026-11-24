package netinfo

import "testing"

func TestURLs(t *testing.T) {
	if got := URLs("127.0.0.1:8631", false); len(got) != 1 || got[0].Kind != "local" {
		t.Errorf("loopback: %+v", got)
	}
	if got := URLs("192.168.1.9:8631", true); len(got) != 1 || got[0].URL != "https://192.168.1.9:8631" {
		t.Errorf("bound: %+v", got)
	}
	for _, a := range URLs(":8631", false) {
		t.Logf("%-32s %s", a.URL, a.Label)
	}
}
