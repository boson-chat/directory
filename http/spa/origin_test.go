package spa

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestOrigin(t *testing.T) {
	cases := map[string]string{
		"https://auth.boson.chat":           "https://auth.boson.chat",
		"https://auth.example.org:8443/x/y": "https://auth.example.org:8443",
		"http://localhost:8080":             "http://localhost:8080",
		"":                                  "",
		"javascript:alert(1)":               "",
		"auth.boson.chat":                   "",
	}
	for in, want := range cases {
		if got := Origin(in); got != want {
			t.Errorf("Origin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSPAllowsAuthOriginAndHashesBootstrap(t *testing.T) {
	s, err := NewSPA(fstest.MapFS{"index.html": {Data: []byte(`<html><script>boot()</script><script src="/x.js"></script></html>`)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ScriptHashes) != 1 {
		t.Fatalf("hashes: %v", s.ScriptHashes)
	}
	csp := s.CSP(Origin("https://auth.boson.chat/"))
	if !strings.Contains(csp, "connect-src 'self' https://auth.boson.chat;") {
		t.Fatalf("csp: %s", csp)
	}
}
