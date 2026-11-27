package federation

import (
	"encoding/json"
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

func TestFeedCarriesTheme(t *testing.T) {
	now := time.Now()
	stored := `{"version":1,"base":"dark","colors":{"accent":"#7c3aed"},"panes":{"messages":{"image":"network-themes/net_1-pane-messages-ab.webp","dim":0.6}},"logo":"network-themes/net_1-logo-cd.svg","login_background":"https://cdn.example/bg.jpg","variants":{"light":{"colors":{"bg":"#f5f9f6","accent":"#0d7a45"},"nick":{"lightness":36},"panes":{"servers":{"bg":"#e7efea","dim":0.4}}}}}`
	changes := []network.Change{
		{Seq: 3, Network: &network.Network{ID: "net_1", Hostname: "irc.a.test", Port: 6697, Name: "A",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-aaaaaaaa",
			Theme: network.JSONB(stored), CreatedAt: now, UpdatedAt: now}},
		{Seq: 4, Network: &network.Network{ID: "net_2", Hostname: "irc.b.test", Port: 6697, Name: "B",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-bbbbbbbb", CreatedAt: now, UpdatedAt: now}},
	}
	page := buildFeed(changes, 0, 10, func(k string) string {
		if strings.HasPrefix(k, "https://") {
			return k
		}
		return "https://dir.test/media/" + k
	})
	got := string(page.Changes[0].Network.Theme)
	for _, want := range []string{`"logo":"https://dir.test/media/network-themes/net_1-logo-cd.svg"`,
		`"image":"https://dir.test/media/network-themes/net_1-pane-messages-ab.webp"`, `"login_background":"https://cdn.example/bg.jpg"`, `"accent":"#7c3aed"`,
		`"variants":{"light":{"colors":{"accent":"#0d7a45","bg":"#f5f9f6"},"nick":{"lightness":36},"panes":{"servers":{"bg":"#e7efea","dim":0.4}}}}`} {
		if !strings.Contains(got, want) {
			t.Errorf("feed theme lacks %s: %s", want, got)
		}
	}
	if page.Changes[1].Network.Theme != nil {
		t.Fatal("no theme must be omitted")
	}
	raw, _ := json.Marshal(page.Changes[1])
	if strings.Contains(string(raw), "theme") {
		t.Fatalf("theme key on a listing without one: %s", raw)
	}

	// The importer keeps it (URLs hot-linked) after validating it again.
	c := validChange()
	c.Network.Theme = page.Changes[0].Network.Theme
	th, err := importedTheme(c.Network.Theme, true)
	if err != nil || !strings.Contains(string(th), "https://dir.test/media/network-themes/net_1-logo-cd.svg") ||
		!strings.Contains(string(th), `"variants":{"light":{"colors":{"accent":"#0d7a45"`) {
		t.Fatalf("%s %v", th, err)
	}
	// Keys (not URLs), bad colours, http in production: rejected.
	for _, bad := range []string{`{"logo":"network-themes/x.png"}`, `{"colors":{"bg":"red"}}`, `{"logo":"http://dir-a:8080/media/x.png"}`,
		`{"variants":{"light":{"colors":{"bg":"red"}}}}`} {
		if _, err := importedTheme(json.RawMessage(bad), true); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if th, err := importedTheme(json.RawMessage(`{"logo":"http://dir-a:8080/media/x.png"}`), false); err != nil || th == nil {
		t.Fatalf("lax: %v", err)
	}
	if th, err := importedTheme(nil, true); err != nil || th != nil {
		t.Fatal("absent theme")
	}
}

func TestFeedPageStopsAtThemeBudget(t *testing.T) {
	big := `{"version":1,"base":"dark","logo":"https://cdn.example/` + strings.Repeat("a", 15000) + `"}`
	var changes []network.Change
	for i := range 300 {
		changes = append(changes, network.Change{Seq: int64(i + 1), Network: &network.Network{
			ID: "net_" + strings.Repeat("x", 3), Hostname: "irc.a.test", Port: 6697, Name: "A", Origin: network.OriginLocal,
			Status: network.StatusVerified, VerificationToken: "tok-aaaaaaaa", Theme: network.JSONB(big)}})
	}
	page := buildFeed(changes, 0, 500, func(k string) string { return k })
	if !page.HasMore || len(page.Changes) >= 300 || page.NextSince != int64(len(page.Changes)) {
		t.Fatalf("has_more=%v changes=%d next=%d", page.HasMore, len(page.Changes), page.NextSince)
	}
}

func TestFeedCarriesChannels(t *testing.T) {
	now := time.Now()
	changes := []network.Change{
		{Seq: 3, Network: &network.Network{ID: "net_1", Hostname: "irc.a.test", Port: 6697, Name: "A",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-aaaaaaaa",
			Channels: []string{"#Lobby", "#help"}, AutojoinChannels: []string{"#Lobby"}, CreatedAt: now, UpdatedAt: now}},
		{Seq: 4, Network: &network.Network{ID: "net_2", Hostname: "irc.b.test", Port: 6697, Name: "B",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-bbbbbbbb",
			Channels: []string{}, AutojoinChannels: []string{}, CreatedAt: now, UpdatedAt: now}},
	}
	page := buildFeed(changes, 0, 10, func(k string) string { return k })
	raw, _ := json.Marshal(page.Changes[0])
	if !strings.Contains(string(raw), `"channels":["#Lobby","#help"],"autojoin_channels":["#Lobby"]`) {
		t.Fatalf("feed lacks the channels: %s", raw)
	}
	raw, _ = json.Marshal(page.Changes[1])
	if strings.Contains(string(raw), "channels") {
		t.Fatalf("empty channel lists must be omitted: %s", raw)
	}

	// Round trip through JSON into an importer, as a peer sees it.
	raw, _ = json.Marshal(page.Changes[0])
	var c FeedChange
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	c.VerificationToken = "AbCdEfGhIjKlMnOp_-123"
	n, err := importedNetwork("peer:per_1", c, true)
	if err != nil {
		t.Fatal(err)
	}
	if p := importedChannels(n, c.Network); p != nil {
		t.Fatal(p)
	}
	if strings.Join(n.Channels, " ") != "#Lobby #help" || strings.Join(n.AutojoinChannels, " ") != "#Lobby" {
		t.Fatalf("imported channels %q autojoin %q", n.Channels, n.AutojoinChannels)
	}
}

func TestImportedChannelsOldPeersAndBadLists(t *testing.T) {
	// An old peer sends no channel keys: empty lists, never NULL.
	var c FeedChange
	if err := json.Unmarshal([]byte(`{"seq":1,"verification_token":"AbCdEfGhIjKlMnOp","network":{"id":"net_1","hostname":"irc.example.org","port":6697,"tls":true,"name":"X"}}`), &c); err != nil {
		t.Fatal(err)
	}
	n, err := importedNetwork("peer:per_1", c, true)
	if err != nil {
		t.Fatal(err)
	}
	if p := importedChannels(n, c.Network); p != nil || n.Channels == nil || n.AutojoinChannels == nil ||
		len(n.Channels)+len(n.AutojoinChannels) != 0 {
		t.Fatalf("old peer: %q %q %v", n.Channels, n.AutojoinChannels, p)
	}
	// Unknown keys from a newer peer are ignored, not rejected.
	if err := json.Unmarshal([]byte(`{"seq":1,"network":{"id":"net_1","future_field":{"x":1},"channels":["#a"]},"future":true}`), &c); err != nil {
		t.Fatalf("unknown fields rejected: %v", err)
	}
	// A bad list is dropped with a problem; the listing stays.
	c = validChange()
	c.Network.Channels = []string{"#ok", "bad name"}
	c.Network.AutojoinChannels = []string{"#ok"}
	n, err = importedNetwork("peer:per_1", c, true)
	if err != nil {
		t.Fatal(err)
	}
	p := importedChannels(n, c.Network)
	if len(p) != 1 || !strings.Contains(p[0], "channels dropped") || len(n.Channels) != 0 || len(n.AutojoinChannels) != 1 {
		t.Fatalf("bad list: %q %q %v", n.Channels, n.AutojoinChannels, p)
	}
}

// A moved listing is exported as an update of the same id with its new
// hostname and the hostnames it left; a listing that never moved omits
// the field (older readers never see it).
func TestFeedCarriesPreviousHostnames(t *testing.T) {
	now := time.Now()
	changes := []network.Change{
		{Seq: 1, Network: &network.Network{ID: "net_1", Hostname: "irc.boson.example.org", Port: 6697, TLS: true, Name: "A",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-aaaaaaaa",
			PreviousHostnames: []string{"irc.example.org"}, CreatedAt: now, UpdatedAt: now}},
		{Seq: 2, Network: &network.Network{ID: "net_2", Hostname: "irc.b.test", Port: 6697, Name: "B",
			Origin: network.OriginLocal, Status: network.StatusVerified, VerificationToken: "tok-bbbbbbbb", PreviousHostnames: []string{}}},
	}
	page := buildFeed(changes, 0, 10, func(k string) string { return k })
	raw, _ := json.Marshal(page)
	if !strings.Contains(string(raw), `"previous_hostnames":["irc.example.org"]`) || strings.Count(string(raw), "previous_hostnames") != 1 {
		t.Fatalf("feed JSON: %s", raw)
	}
	if c := page.Changes[0]; c.Deleted || c.Network.ID != "net_1" || c.Network.Hostname != "irc.boson.example.org" {
		t.Fatalf("moved listing: %+v", c)
	}
}

func TestImportedPreviousHostnames(t *testing.T) {
	c := validChange()
	c.Network.PreviousHostnames = []string{"irc.old.example.org", "IRC.Upper.example.org", "irc.example.org", "irc.old.example.org", "not a host", "10.0.0.1"}
	n, err := importedNetwork("peer:per_x", c, true)
	if err != nil {
		t.Fatal(err)
	}
	problems := importedPreviousHostnames(n, c.Network)
	if strings.Join(n.PreviousHostnames, ",") != "irc.old.example.org" || len(problems) != 3 {
		t.Fatalf("previous %q problems %q", n.PreviousHostnames, problems)
	}
	// Capped; an older origin that doesn't send them means none.
	c.Network.PreviousHostnames = nil
	for i := 0; i < 15; i++ {
		c.Network.PreviousHostnames = append(c.Network.PreviousHostnames, "irc"+string(rune('a'+i))+".example.org")
	}
	importedPreviousHostnames(n, c.Network)
	if len(n.PreviousHostnames) != network.MaxPreviousHostnames {
		t.Fatalf("cap: %d", len(n.PreviousHostnames))
	}
	c.Network.PreviousHostnames = nil
	if importedPreviousHostnames(n, c.Network); n.PreviousHostnames == nil || len(n.PreviousHostnames) != 0 {
		t.Fatalf("none: %q", n.PreviousHostnames)
	}
}
