package network

import "testing"

func TestPrefixTSQuery(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "",
		"libera":              "libera:*",
		"Libera.Chat":         "libera:* & chat:*",
		"open   source":       "open:* & source:*",
		`a&b|c!d:*(e)'`:       "abcde:*",
		"--x--":               "x:*",
		"日本語":                 "日本語:*",
		"a b c d e f g h i j": "a:* & b:* & c:* & d:* & e:* & f:* & g:* & h:*",
	} {
		if got := PrefixTSQuery(in); got != want {
			t.Errorf("PrefixTSQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
