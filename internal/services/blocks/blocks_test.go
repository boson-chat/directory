package blocks

import "testing"

func TestPatterns(t *testing.T) {
	for in, want := range map[string]string{"IRC.Bad.Example": "irc.bad.example", "*.Bad.example.": "*.bad.example"} {
		if got, err := NormalizePattern(in); err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"*", "*.com", "1.2.3.4", "**.x.y", "bad host"} {
		if _, err := NormalizePattern(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if !Matches("*.bad.example", "bad.example") || !Matches("*.bad.example", "a.b.bad.example") || Matches("*.bad.example", "notbad.example") ||
		!Matches("irc.x.org", "irc.x.org") || Matches("irc.x.org", "a.irc.x.org") {
		t.Fatal("Matches")
	}
}
