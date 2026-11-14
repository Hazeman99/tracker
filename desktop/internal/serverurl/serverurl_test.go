package serverurl

import "testing"

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"https://tracker.example.com":       "https://tracker.example.com",
		"https://Tracker.Example.com/":      "https://tracker.example.com",
		"tracker.example.com":               "https://tracker.example.com",
		" https://tracker.example.com:8443": "https://tracker.example.com:8443",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "http://tracker.example.com", "ftp://x", "https://user:pw@tracker.example.com",
		"https://tracker.example.com/api", "https://tracker.example.com?x=1", "https://tracker.example.com#f",
		"javascript:alert(1)", "https://", "http://localhost.evil.com",
	}
	for _, in := range bad {
		if got, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) = %q, want error", in, got)
		}
	}
	_, err := Normalize("http://localhost:8080")
	if allowInsecureLocalhost != (err == nil) {
		t.Errorf("http://localhost accepted=%v in a build with allowInsecureLocalhost=%v", err == nil, allowInsecureLocalhost)
	}
}
