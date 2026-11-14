package networks

import (
	"testing"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/dnsverify"
)

func TestTransition(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	lapse := 14 * 24 * time.Hour
	cases := []struct {
		name     string
		n        network.Network
		success  bool
		want     string
		verified bool
	}{
		{"pending match", network.Network{Status: "pending"}, true, "verified", true},
		{"pending miss", network.Network{Status: "pending"}, false, "pending", false},
		{"verified miss recent", network.Network{Status: "verified", LastVerifiedAt: ago(13 * 24 * time.Hour)}, false, "verified", false},
		{"verified miss old", network.Network{Status: "verified", LastVerifiedAt: ago(15 * 24 * time.Hour)}, false, "lapsed", false},
		{"verified miss never", network.Network{Status: "verified"}, false, "lapsed", false},
		{"verified match", network.Network{Status: "verified", LastVerifiedAt: ago(time.Hour)}, true, "verified", true},
		{"lapsed match recovers", network.Network{Status: "lapsed"}, true, "verified", true},
		{"lapsed miss", network.Network{Status: "lapsed"}, false, "lapsed", false},
		{"blocked stays", network.Network{Status: "blocked"}, true, "blocked", false},
	}
	for _, c := range cases {
		got, at := Transition(&c.n, c.success, now, lapse)
		if got != c.want || (at != nil) != c.verified {
			t.Errorf("%s: %s %v, want %s %v", c.name, got, at, c.want, c.verified)
		}
	}
	if ModeFor(&network.Network{Status: "pending"}) != dnsverify.ModeStrict || ModeFor(&network.Network{Status: "lapsed"}) != dnsverify.ModeLenient {
		t.Fatal("ModeFor")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := network.Cursor{Sort: network.SortUsers, Keys: []string{"5", "2026-01-01T00:00:00Z"}, ID: "net_x"}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil || got.ID != c.ID || got.Sort != c.Sort || len(got.Keys) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"!!", "e30", "bm90IGpzb24"} {
		if _, err := decodeCursor(bad); err == nil {
			t.Errorf("decodeCursor(%q) accepted", bad)
		}
	}
}

func TestCanManage(t *testing.T) {
	s := &NetworkService{}
	owner, hash := "usr_a", "abc"
	acc := &network.Network{Origin: "local", OwnerAuthUserID: &owner}
	key := &network.Network{Origin: "local", ManagementKeyHash: &hash}
	peer := &network.Network{Origin: "peer:p"}
	a := &principal.Principal{Kind: principal.KindAccount, AuthUserID: "usr_a"}
	b := &principal.Principal{Kind: principal.KindAccount, AuthUserID: "usr_b"}
	k := &principal.Principal{Kind: principal.KindNetworkKey, KeyHash: "abc"}
	op := &principal.Principal{Kind: principal.KindAccount, AuthUserID: "usr_op", Perms: []string{"directory.manage"}}
	internal := &principal.Principal{Kind: principal.KindInternal}
	checks := []struct {
		p    *principal.Principal
		n    *network.Network
		want bool
	}{
		{a, acc, true}, {b, acc, false}, {k, acc, false}, {k, key, true}, {a, key, false},
		{a, peer, false}, {op, peer, true}, {op, acc, true}, {internal, key, true}, {nil, acc, false},
	}
	for i, c := range checks {
		if got := s.CanManage(c.p, c.n); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func TestSourceOrigin(t *testing.T) {
	for in, want := range map[string]string{"": "", "all": "", "local": "local", "per_1": "peer:per_1", "peer:per_1": "peer:per_1"} {
		if got := SourceOrigin(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
