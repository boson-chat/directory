package hostname

import "testing"

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"irc.libera.chat":    "irc.libera.chat",
		" IRC.Libera.Chat. ": "irc.libera.chat",
		"irc.bücher.de":      "irc.xn--bcher-kva.de",
		"a-b.example.org":    "a-b.example.org",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "localhost", "127.0.0.1", "::1", "[2001:db8::1]", "1.2.3.4.5", "under_score.example", "-x.example", "a..b", "x.123"} {
		if got, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) = %q, want error", bad, got)
		}
	}
}
