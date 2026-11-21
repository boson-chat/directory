package networks

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestCleanChannels(t *testing.T) {
	got, err := cleanChannels([]string{" #Lobby ", "#lobby", "#help", "#LOBBY", "#a!b[c]~"}, "channels")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"#Lobby", "#help", "#a!b[c]~"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, err := cleanChannels(nil, "channels"); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("nil: %q %v", got, err)
	}
	if _, err := cleanChannels([]string{"#" + strings.Repeat("x", 63)}, "channels"); err != nil {
		t.Fatalf("63 chars after #: %v", err)
	}
	many := make([]string, 50)
	for i := range many {
		many[i] = fmt.Sprintf("#c%d", i)
	}
	if _, err := cleanChannels(many, "channels"); err != nil {
		t.Fatalf("50: %v", err)
	}
	if _, err := cleanChannels(append(many, "#c50"), "channels"); err == nil {
		t.Fatal("51 accepted")
	}
	for _, bad := range []string{"", "#", "lobby", "&local", "#a b", "#a,b", "#" + strings.Repeat("x", 64),
		"#tab\there", "#café", "#nul\x00", "#del\x7f", "##"[:1]} {
		if _, err := cleanChannels([]string{bad}, "autojoin_channels"); err == nil {
			t.Errorf("%q accepted", bad)
		} else if !strings.Contains(err.Error(), "autojoin_channels") {
			t.Errorf("%q: error doesn't name the field: %v", bad, err)
		}
	}
	if !ValidChannel("##") {
		t.Fatal("## is a valid channel")
	}
}
