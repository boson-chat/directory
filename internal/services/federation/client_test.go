package federation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in     string
		strict bool
		want   string
		bad    bool
	}{
		{in: "https://Directory.Boson.Chat/", strict: true, want: "https://directory.boson.chat"},
		{in: "https://directory.boson.chat:443", strict: true, want: "https://directory.boson.chat"},
		{in: "https://example.org:8443/dir/", strict: true, want: "https://example.org:8443/dir"},
		{in: "http://example.org", strict: true, bad: true},
		{in: "http://dir-a:8080", strict: false, want: "http://dir-a:8080"},
		{in: "http://example.org:80/", strict: false, want: "http://example.org"},
		{in: "https://user:pw@example.org", strict: true, bad: true},
		{in: "https://example.org/?x=1", strict: true, bad: true},
		{in: "https://example.org/#f", strict: true, bad: true},
		{in: "ftp://example.org", strict: false, bad: true},
		{in: "example.org", strict: false, bad: true},
		{in: "https://127.0.0.1", strict: true, bad: true},
		{in: "https://10.1.2.3", strict: true, bad: true},
		{in: "https://[::1]", strict: true, bad: true},
		{in: "https://localhost", strict: true, bad: true},
		{in: "https://127.0.0.1:8443", strict: false, want: "https://127.0.0.1:8443"},
		{in: "https://[2606:4700::1]/", strict: true, want: "https://[2606:4700::1]"},
		{in: "", bad: true},
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.in, c.strict)
		if c.bad {
			if err == nil {
				t.Errorf("NormalizeURL(%q, %v) = %q, want error", c.in, c.strict, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("NormalizeURL(%q, %v) = %q, %v; want %q", c.in, c.strict, got, err, c.want)
		}
	}
}

func TestPublicAddr(t *testing.T) {
	for s, want := range map[string]bool{
		"1.1.1.1": true, "8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.0.0.1": false, "172.16.5.4": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false,
		"::1": false, "fd00::1": false, "fe80::1": false, "::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false,
		"64:ff9b::a00:1": false,
	} {
		if got := PublicAddr(netip.MustParseAddr(s)); got != want {
			t.Errorf("PublicAddr(%s) = %v, want %v", s, got, want)
		}
	}
}

func TestStrictClientRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"api":"x","federation":true}`))
	}))
	defer srv.Close()

	strict := NewClient(ClientOptions{Strict: true})
	_, err := strict.FetchWellKnown(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "not a public IP") {
		t.Fatalf("strict client reached a loopback server: %v", err)
	}
	lax := NewClient(ClientOptions{})
	if _, err := lax.FetchWellKnown(context.Background(), srv.URL); err != nil {
		t.Fatalf("non-production client: %v", err)
	}
}

func TestClientLimits(t *testing.T) {
	big := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/boson-directory.json":
			_, _ = w.Write([]byte(`{"name":"` + big + `","api":"x","federation":true}`))
		case "/v1/feed":
			if r.URL.Query().Get("since") == "10" {
				_, _ = w.Write([]byte(`{"changes":[],"next_since":3}`))
				return
			}
			_, _ = w.Write([]byte(`{"changes":[{},{}],"next_since":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{MaxBytes: 1024})
	if _, err := c.FetchWellKnown(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("oversized body: %v", err)
	}
	c = NewClient(ClientOptions{})
	if _, err := c.FetchFeed(context.Background(), srv.URL, 0, 1); err == nil || !strings.Contains(err.Error(), "for limit 1") {
		t.Fatalf("over-long page: %v", err)
	}
	if _, err := c.FetchFeed(context.Background(), srv.URL, 10, 10); !errors.Is(err, errCursorRegressed) {
		t.Fatalf("regressed cursor: %v", err)
	}
	var pe *PeerError
	_, err := c.FetchFeed(context.Background(), srv.URL+"/nope", 0, 10)
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("404: %v", err)
	}
}

func TestWellKnownMustOfferFederation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"x","api":"https://x/v1","federation":false}`))
	}))
	defer srv.Close()
	if _, err := NewClient(ClientOptions{}).FetchWellKnown(context.Background(), srv.URL); err == nil {
		t.Fatal("accepted a directory without federation")
	}
}
