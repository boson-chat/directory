package federation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
)

func ptr[T any](v T) *T { return &v }

func TestBuildFeedExportsOnlyVerifiedLocalListings(t *testing.T) {
	now := time.Now()
	icon := "icons/net_1-abc.png"
	changes := []network.Change{
		{Seq: 3, Network: &network.Network{ID: "net_1", Hostname: "irc.a.test", Port: 6697, TLS: true, Name: "A",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-aaaaaaaa",
			IconKey: &icon, OwnerAuthUserID: ptr("usr_secret"), CreatedAt: now, UpdatedAt: now}},
		{Seq: 4, Network: &network.Network{ID: "net_2", Hostname: "irc.pending.test", Port: 6697,
			Origin: network.OriginLocal, Status: network.StatusPending, VerificationToken: "tok-bbbbbbbb"}},
		{Seq: 5, Network: &network.Network{ID: "net_3", Hostname: "irc.imported.test", Port: 6697,
			Origin: "peer:per_x", OriginID: ptr("net_9"), Status: network.StatusVerified, VerificationToken: "tok-cccccccc"}},
		{Seq: 6, Deleted: &network.Tombstone{Seq: 6, NetworkID: "net_4", Hostname: "irc.gone.test", Port: 6697, Origin: network.OriginLocal}},
		{Seq: 7, Deleted: &network.Tombstone{Seq: 7, NetworkID: "net_5", Hostname: "irc.gone2.test", Port: 6697, Origin: "peer:per_x"}},
	}
	page := buildFeed(changes, 2, 5, func(k string) string { return "https://dir.test/media/" + k })
	if page.NextSince != 7 || !page.HasMore {
		t.Fatalf("next_since=%d has_more=%v", page.NextSince, page.HasMore)
	}
	if len(page.Changes) != 3 {
		t.Fatalf("got %d changes, want 3 (imported rows are never re-exported): %+v", len(page.Changes), page.Changes)
	}
	a := page.Changes[0]
	if a.Deleted || a.VerificationToken != "tok-aaaaaaaa" || a.Network.Hostname != "irc.a.test" ||
		a.Network.IconURL == nil || *a.Network.IconURL != "https://dir.test/media/"+icon {
		t.Fatalf("verified local listing: %+v", a)
	}
	if p := page.Changes[1]; !p.Deleted || p.Network.ID != "net_2" || p.Network.Hostname != "" || p.VerificationToken != "" {
		t.Fatalf("pending listing must be exported as a bare deletion: %+v", p)
	}
	if d := page.Changes[2]; !d.Deleted || d.Network.ID != "net_4" || d.Network.Hostname != "" {
		t.Fatalf("tombstone: %+v", d)
	}
	empty := buildFeed(nil, 9, 5, nil)
	if empty.NextSince != 9 || empty.HasMore || empty.Changes == nil {
		t.Fatalf("empty page: %+v", empty)
	}
}

func validChange() FeedChange {
	return FeedChange{
		Seq: 1, VerificationToken: "AbCdEfGhIjKlMnOp_-123",
		Network: FeedNetwork{ID: "net_01ABC", Hostname: "irc.example.org", Port: 6697, TLS: true, Name: " Example ",
			Tags: []string{"Gaming", "gaming"}, Languages: []string{"en"}, WebsiteURL: ptr("https://example.org"),
			IconURL: ptr("https://dir.test/media/i.png")},
	}
}

func TestImportedNetworkValidation(t *testing.T) {
	n, err := importedNetwork("peer:per_1", validChange(), true)
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "Example" || len(n.Tags) != 1 || n.Tags[0] != "gaming" || n.Origin != "peer:per_1" ||
		*n.OriginID != "net_01ABC" || n.Status != network.StatusPending || n.OwnerAuthUserID != nil || *n.IconKey != "https://dir.test/media/i.png" {
		t.Fatalf("imported: %+v", n)
	}

	bad := map[string]func(c *FeedChange){
		"id":          func(c *FeedChange) { c.Network.ID = "../etc" },
		"ip host":     func(c *FeedChange) { c.Network.Hostname = "10.0.0.1" },
		"non-canon":   func(c *FeedChange) { c.Network.Hostname = "IRC.Example.org" },
		"port":        func(c *FeedChange) { c.Network.Port = 0 },
		"token":       func(c *FeedChange) { c.VerificationToken = "x" },
		"token chars": func(c *FeedChange) { c.VerificationToken = "abcdefgh\"; DROP" },
		"name":        func(c *FeedChange) { c.Network.Name = "" },
		"long name":   func(c *FeedChange) { c.Network.Name = strings.Repeat("n", 65) },
		"desc":        func(c *FeedChange) { c.Network.Description = strings.Repeat("d", 501) },
		"tags":        func(c *FeedChange) { c.Network.Tags = []string{"<script>"} },
		"many tags":   func(c *FeedChange) { c.Network.Tags = strings.Split("a,b,c,d,e,f,g,h,i", ",") },
		"lang":        func(c *FeedChange) { c.Network.Languages = []string{"english"} },
		"website":     func(c *FeedChange) { c.Network.WebsiteURL = ptr("javascript:alert(1)") },
		"auth url":    func(c *FeedChange) { c.Network.AuthURL = ptr("http://example.org") },
		"icon":        func(c *FeedChange) { c.Network.IconURL = ptr("http://dir.test/i.png") },
		"icon scheme": func(c *FeedChange) { c.Network.IconURL = ptr("data:image/png;base64,AAAA") },
	}
	for name, mut := range bad {
		c := validChange()
		mut(&c)
		if _, err := importedNetwork("peer:per_1", c, true); !errors.Is(err, errRejected) {
			t.Errorf("%s: accepted (err=%v)", name, err)
		}
	}
	// Outside production plain-http images are fine (docker tests).
	c := validChange()
	c.Network.IconURL = ptr("http://dir-a:8080/media/i.png")
	if _, err := importedNetwork("peer:per_1", c, false); err != nil {
		t.Fatalf("lax icon: %v", err)
	}
}
