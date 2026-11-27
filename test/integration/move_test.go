package integration

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
)

type moveView struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Hostname      string `json:"hostname"`
	Port          int    `json:"port"`
	TLS           bool   `json:"tls"`
	RecordName    string `json:"record_name"`
	RecordValue   string `json:"record_value"`
	Token         string `json:"token"`
	Expired       bool   `json:"expired"`
	ExpiresAt     string `json:"expires_at"`
	LastCheckedAt string `json:"last_checked_at"`
	From          struct {
		Hostname string `json:"hostname"`
		Port     int    `json:"port"`
	} `json:"from"`
	To struct {
		Hostname string `json:"hostname"`
	} `json:"to"`
}

type moveListing struct {
	Network struct {
		ID                string   `json:"id"`
		Hostname          string   `json:"hostname"`
		Port              int      `json:"port"`
		TLS               bool     `json:"tls"`
		Name              string   `json:"name"`
		Status            string   `json:"status"`
		Tags              []string `json:"tags"`
		Channels          []string `json:"channels"`
		PreviousHostnames []string `json:"previous_hostnames"`
		CreatedAt         string   `json:"created_at"`
		VerificationToken string   `json:"verification_token"`
	} `json:"network"`
	Verification *struct {
		RecordName string `json:"record_name"`
		Token      string `json:"token"`
	} `json:"verification"`
	Move      *moveView  `json:"move"`
	MovedFrom string     `json:"moved_from"`
	History   []moveView `json:"history"`
	// verify responses
	Moved   bool `json:"moved"`
	Success bool `json:"success"`
	Report  *struct {
		Success bool `json:"success"`
	} `json:"report"`
}

func (e *env) moveListing(r resp) moveListing {
	e.t.Helper()
	var l moveListing
	r.json(e.t, &l)
	return l
}

func (e *env) auditActions(t *testing.T) []string {
	t.Helper()
	op := e.token("usr_op", "directory.manage")
	var au struct {
		Entries []struct {
			Action    string `json:"action"`
			ActorKind string `json:"actor_kind"`
		} `json:"entries"`
	}
	e.expect(e.do("GET", "/v1/admin/audit?limit=200", bearer(op), nil), 200).json(t, &au)
	var out []string
	for _, a := range au.Entries {
		out = append(out, a.Action+"/"+a.ActorKind)
	}
	return out
}

// The whole move with the stack's Network key: request, pending (old
// hostname still served), failed check, check, switched.
func TestMoveListingHostname(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-move-aaaaaaaaaaaaaaaaaa")
	id := e.registerVerified(k, "irc.ircforever.example", map[string]any{
		"tags": []string{"chat"}, "channels": []string{"#lobby"}, "autojoin_channels": []string{"#lobby"},
	})
	before := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, k, nil), 200))
	if before.Move != nil || before.Network.PreviousHostnames == nil || len(before.Network.PreviousHostnames) != 0 {
		t.Fatalf("fresh listing: %+v", before)
	}
	var seqBefore int64
	_ = e.sqlDB.QueryRow("SELECT seq FROM networks WHERE id=$1", id).Scan(&seqBefore)

	// Request: 201 with the record for the new hostname.
	e.dns.Set("irc.boson.ircforever.example", 0)
	l := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", k,
		map[string]any{"hostname": "IRC.Boson.IRCForever.example.", "port": 6698}), 201))
	m := l.Move
	if m == nil || m.Status != "pending" || m.Hostname != "irc.boson.ircforever.example" || m.Port != 6698 || !m.TLS ||
		m.RecordName != "_boson.irc.boson.ircforever.example" || m.RecordValue != "boson-verify="+m.Token || m.Token == "" ||
		m.Expired || m.ExpiresAt == "" || m.From.Hostname != "irc.ircforever.example" || m.From.Port != 6697 {
		t.Fatalf("move: %+v", m)
	}
	if l.Network.Hostname != "irc.ircforever.example" || l.Network.Status != "verified" {
		t.Fatalf("the listing must keep its hostname while pending: %+v", l.Network)
	}
	// Nothing public changed: seq, public view, lookups.
	var seq int64
	_ = e.sqlDB.QueryRow("SELECT seq FROM networks WHERE id=$1", id).Scan(&seq)
	if seq != seqBefore {
		t.Fatal("a pending move bumped seq (it would reach the feed)")
	}
	pub := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, "", nil), 200))
	if pub.Move != nil || pub.Verification != nil {
		t.Fatalf("strangers see the move: %+v", pub)
	}
	e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.boson.ircforever.example", "", nil), 404)
	// The owner's listing views carry it.
	if got := e.moveListing(e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.ircforever.example", k, nil), 200)); got.Move == nil || got.Move.Token != m.Token {
		t.Fatalf("lookup by the owner: %+v", got.Move)
	}
	// The same request again keeps the token.
	again := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", k,
		map[string]any{"hostname": "irc.boson.ircforever.example", "port": 6698, "tls": true}), 200))
	if again.Move.Token != m.Token || again.Move.ID != m.ID {
		t.Fatalf("repeat request changed the token: %+v", again.Move)
	}

	// Check while the record is missing: 409 with the report, still pending.
	miss := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 409))
	if miss.Moved || miss.Success || miss.Report == nil || miss.Move == nil || miss.Move.LastCheckedAt == "" ||
		miss.Network.Hostname != "irc.ircforever.example" {
		t.Fatalf("failed check: %+v", miss)
	}
	// Rate limited like the listing check.
	r := e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 429)
	if r.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// 2 of 3 isn't enough: a move is a first verification of the hostname.
	e.dns.Set("irc.boson.ircforever.example", 2)
	e.clock.Advance(31 * time.Second)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 409)

	e.dns.Set("irc.boson.ircforever.example", 3)
	e.dns.SetToken("irc.boson.ircforever.example", m.Token)
	e.clock.Advance(31 * time.Second)
	ok := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 200))
	n := ok.Network
	if !ok.Moved || !ok.Success || ok.Move != nil || n.ID != id || n.Hostname != "irc.boson.ircforever.example" || n.Port != 6698 ||
		!n.TLS || n.Status != "verified" || !slices.Equal(n.PreviousHostnames, []string{"irc.ircforever.example"}) ||
		!slices.Equal(n.Tags, []string{"chat"}) || !slices.Equal(n.Channels, []string{"#lobby"}) || n.CreatedAt != before.Network.CreatedAt ||
		n.VerificationToken != m.Token || ok.Verification == nil || ok.Verification.RecordName != "_boson.irc.boson.ircforever.example" {
		t.Fatalf("after the move: %+v", ok)
	}
	var owner *string
	var healthAt *time.Time
	_ = e.sqlDB.QueryRow("SELECT management_key_hash, health_checked_at FROM networks WHERE id=$1", id).Scan(&owner, &healthAt)
	if owner == nil || healthAt != nil {
		t.Fatalf("owner kept and due for a health probe: %v %v", owner, healthAt)
	}

	// Lookups: the new hostname, and the old one with the hint.
	if got := e.moveListing(e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.boson.ircforever.example", "", nil), 200)); got.MovedFrom != "" || got.Network.ID != id {
		t.Fatalf("lookup new: %+v", got)
	}
	r = e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.ircforever.example", "", nil), 200)
	if got := e.moveListing(r); got.MovedFrom != "irc.ircforever.example" || got.Network.ID != id || got.Network.Hostname != "irc.boson.ircforever.example" {
		t.Fatalf("lookup old: %+v", got)
	}
	if r.Header.Get("Content-Location") != "/v1/networks/"+id {
		t.Fatalf("Content-Location %q", r.Header.Get("Content-Location"))
	}
	// Search finds it under the new hostname.
	if got := e.list("q=boson"); len(got.Networks) != 1 {
		t.Fatalf("search by new hostname: %+v", got)
	}

	// History.
	h := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id+"/move", k, nil), 200))
	if h.Move != nil || len(h.History) != 1 || h.History[0].Status != "completed" || h.History[0].Token != "" ||
		h.History[0].To.Hostname != "irc.boson.ircforever.example" {
		t.Fatalf("history: %+v", h)
	}
	// Strangers can't read it.
	e.expect(e.do("GET", "/v1/networks/"+id+"/move", netkey("stack-admin-key-move-other-bbbbbbbbbbbb"), nil), 403)

	acts := e.auditActions(t)
	for _, want := range []string{"network.move_request/network_key", "network.move_check/network_key", "network.move/network_key"} {
		if !slices.Contains(acts, want) {
			t.Errorf("audit lacks %s: %v", want, acts)
		}
	}

	// The daily re-check now uses the new hostname and the move's token.
	calls := len(e.dns.Calls())
	e.clock.Advance(day + time.Minute)
	e.runWorker("verify")
	if got := e.dns.Calls()[calls:]; !slices.Contains(got, "irc.boson.ircforever.example") || slices.Contains(got, "irc.ircforever.example") {
		t.Fatalf("re-check hostnames: %v", got)
	}
	if e.status(id) != "verified" {
		t.Fatalf("status after re-check: %s", e.status(id))
	}

	// Moving back drops it from previous_hostnames; the list is most recent first.
	e.dns.Set("irc.ircforever.example", 3)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.ircforever.example"}), 201)
	back := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 200))
	if back.Network.Hostname != "irc.ircforever.example" || back.Network.Port != 6698 ||
		!slices.Equal(back.Network.PreviousHostnames, []string{"irc.boson.ircforever.example"}) {
		t.Fatalf("move back: %+v", back.Network)
	}
}

func TestMoveCancelReplaceAndExpire(t *testing.T) {
	e := newEnv(t)
	alice := e.token("usr_alice")
	id := e.registerVerified(bearer(alice), "irc.cancel.example", nil)
	e.dns.Set("irc.a.example", 0)
	e.dns.Set("irc.b.example", 0)

	first := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", bearer(alice), map[string]any{"hostname": "irc.a.example"}), 201)).Move
	// Another target replaces it with a new token.
	second := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", bearer(alice), map[string]any{"hostname": "irc.b.example", "tls": false}), 201)).Move
	if second.Token == first.Token || second.Hostname != "irc.b.example" || second.TLS || second.Port != 6697 {
		t.Fatalf("replace: %+v %+v", first, second)
	}
	// Owner session cookie works too (with X-Requested-With for writes).
	cookie := "Cookie: boson_at=" + alice
	e.expect(e.do("DELETE", "/v1/networks/"+id+"/move", "", nil, cookie), 403)
	l := e.moveListing(e.expect(e.do("DELETE", "/v1/networks/"+id+"/move", "", nil, cookie, "X-Requested-With: fetch"), 200))
	if l.Move != nil {
		t.Fatalf("cancel: %+v", l.Move)
	}
	// Idempotent; checking without a move is 404.
	e.expect(e.do("DELETE", "/v1/networks/"+id+"/move", bearer(alice), nil), 200)
	if e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", bearer(alice), nil), 404).m(t)["error"] != "no_pending_move" {
		t.Fatal("want no_pending_move")
	}
	h := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id+"/move", bearer(alice), nil), 200))
	if len(h.History) != 2 || h.History[0].Status != "cancelled" || h.History[1].Status != "cancelled" {
		t.Fatalf("history: %+v", h.History)
	}

	// Expiry: after the token TTL the move is no longer checked, Check is 410.
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", bearer(alice), map[string]any{"hostname": "irc.a.example"}), 201)
	e.runWorker("verify") // checks it once (record missing)
	e.clock.Advance(73 * time.Hour)
	e.dns.Set("irc.a.example", 3)
	calls := len(e.dns.Calls())
	e.runWorker("verify")
	if slices.Contains(e.dns.Calls()[calls:], "irc.a.example") {
		t.Fatal("worker checked an expired move")
	}
	if e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", bearer(alice), nil), 410).m(t)["error"] != "token_expired" {
		t.Fatal("want token_expired")
	}
	got := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, bearer(alice), nil), 200))
	if got.Move == nil || !got.Move.Expired || got.Network.Hostname != "irc.cancel.example" {
		t.Fatalf("expired move: %+v", got)
	}
	// Requesting again issues a fresh token; the old request is closed as expired.
	fresh := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", bearer(alice), map[string]any{"hostname": "irc.a.example"}), 201)).Move
	if fresh.Expired || fresh.Token == got.Move.Token {
		t.Fatalf("fresh: %+v", fresh)
	}
	h = e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id+"/move", bearer(alice), nil), 200))
	if h.History[1].Status != "expired" {
		t.Fatalf("history after re-request: %+v", h.History)
	}
	// The worker closes requests that expired long ago.
	e.clock.Advance(73*time.Hour + 31*day)
	e.runWorker("verify")
	if got := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, bearer(alice), nil), 200)); got.Move != nil {
		t.Fatalf("old expired move still pending: %+v", got.Move)
	}
}

// The verify worker completes a move whose record shows up.
func TestMoveCompletedByWorker(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-move-worker-cccccccccccc")
	id := e.registerVerified(k, "irc.w.example", nil)
	// The old record is gone: the listing lapses, and it can still move.
	e.exec("UPDATE networks SET status='lapsed' WHERE id=$1", id)
	e.dns.Set("irc.w2.example", 0)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.w2.example"}), 201)
	e.runWorker("verify")
	if e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, k, nil), 200)).Network.Hostname != "irc.w.example" {
		t.Fatal("moved without the record")
	}
	e.dns.Set("irc.w2.example", 3)
	e.runWorker("verify") // not due yet (PendingEvery)
	if e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, k, nil), 200)).Network.Hostname != "irc.w.example" {
		t.Fatal("checked before it was due")
	}
	e.clock.Advance(61 * time.Minute)
	e.runWorker("verify")
	got := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, k, nil), 200))
	if got.Network.Hostname != "irc.w2.example" || got.Network.Status != "verified" || got.Move != nil {
		t.Fatalf("worker move: %+v", got)
	}
	if acts := e.auditActions(t); !slices.Contains(acts, "network.move/system") {
		t.Fatalf("audit: %v", acts)
	}
}

func TestMoveRules(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-rules-dddddddddddddddddd")
	other := netkey("stack-admin-key-rules-eeeeeeeeeeeeeeeeee")
	id := e.registerVerified(k, "irc.rules.example", nil)
	mv := func(host string, status int) map[string]any {
		t.Helper()
		return e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": host}), status).m(t)
	}

	// Validation.
	if mv("not a host", 400)["error"] != "invalid_hostname" {
		t.Fatal("want invalid_hostname")
	}
	mv("10.0.0.1", 400)
	mv("irc.rules.example", 400) // the same address
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.x.example", "port": 70000}), 400)

	// Taken by another verified or a fresh pending listing.
	e.registerVerified(other, "irc.taken.example", nil)
	if mv("irc.taken.example", 409)["error"] != "hostname_taken" {
		t.Fatal("want hostname_taken")
	}
	e.register(other, "irc.claimed.example", nil)
	mv("irc.claimed.example", 409)

	// Blocked hostnames.
	op := e.token("usr_op", "directory.manage")
	e.expect(e.do("POST", "/v1/admin/blocks", bearer(op), map[string]any{"pattern": "*.bad.example"}), 201)
	if mv("irc.bad.example", 403)["error"] != "blocked" {
		t.Fatal("want blocked")
	}

	// Who may move: not another key, not a pending listing, not an import.
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", other, map[string]any{"hostname": "irc.y.example"}), 403)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", "", map[string]any{"hostname": "irc.y.example"}), 401)
	pid, _ := e.register(k, "irc.pending-rules.example", nil)
	if e.expect(e.do("POST", "/v1/networks/"+pid+"/move", k, map[string]any{"hostname": "irc.y.example"}), 409).m(t)["error"] != "not_verified" {
		t.Fatal("want not_verified")
	}
	imp := &network.Network{Hostname: "irc.imported-rules.example", Port: 6697, TLS: true, Name: "imp", Origin: "peer:per_x",
		OriginID: ptr("net_remote"), Status: network.StatusVerified, VerificationToken: "tok-remote-123", TokenIssuedAt: e.clock.Now()}
	if err := e.app.Deps.NetworkRepo.Create(context.Background(), imp); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("POST", "/v1/networks/"+imp.ID+"/move", bearer(op), map[string]any{"hostname": "irc.y.example"}), 409)
	// An operator may move any local listing.
	e.dns.Set("irc.op.example", 0)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", bearer(op), map[string]any{"hostname": "irc.op.example"}), 201)
	// An operator-blocked listing can't.
	e.expect(e.do("POST", "/v1/admin/networks/"+id+"/block", bearer(op), map[string]any{}), 200)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.op.example"}), 403)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 403)
}

func ptr[T any](v T) *T { return &v }

// The new hostname may be held by an import or an abandoned claim: the
// move takes it over at completion, like a registration would. A holder
// that verifies in the meantime wins.
func TestMoveConflictsAtCompletion(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-conf-ffffffffffffffffffff")
	other := netkey("stack-admin-key-conf-gggggggggggggggggggg")
	id := e.registerVerified(k, "irc.conf.example", nil)

	// An imported listing holds the target.
	imp := &network.Network{Hostname: "irc.imp.example", Port: 6697, TLS: true, Name: "imp", Origin: "peer:per_x",
		OriginID: ptr("net_remote2"), Status: network.StatusVerified, VerificationToken: "tok-remote-456", TokenIssuedAt: e.clock.Now()}
	if err := e.app.Deps.NetworkRepo.Create(context.Background(), imp); err != nil {
		t.Fatal(err)
	}
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.imp.example"}), 201)
	got := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 200))
	if got.Network.Hostname != "irc.imp.example" {
		t.Fatalf("over an import: %+v", got.Network)
	}
	if _, err := e.app.Deps.NetworkRepo.Get(context.Background(), imp.ID); err == nil {
		t.Fatal("the import was kept")
	}

	// An abandoned (expired) claim holds the next target.
	e.clock.Advance(time.Minute)
	stale, _ := e.register(other, "irc.stale.example", nil)
	e.clock.Advance(73 * time.Hour)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.stale.example"}), 201)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 200)
	if _, err := e.app.Deps.NetworkRepo.Get(context.Background(), stale); err == nil {
		t.Fatal("the expired claim was kept")
	}
	if acts := e.auditActions(t); !slices.Contains(acts, "network.replace_imported/network_key") || !slices.Contains(acts, "network.replace_expired/network_key") {
		t.Fatalf("audit: %v", acts)
	}

	// Someone else registers and verifies the target while the move waits.
	e.clock.Advance(time.Minute)
	e.dns.Set("irc.race.example", 0)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.race.example"}), 201)
	e.dns.Set("irc.race.example", 3)
	e.registerVerified(other, "irc.race.example", nil)
	if e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 409).m(t)["error"] != "hostname_taken" {
		t.Fatal("want hostname_taken at completion")
	}
	if got := e.moveListing(e.expect(e.do("GET", "/v1/networks/"+id, k, nil), 200)); got.Network.Hostname != "irc.stale.example" || got.Move == nil {
		t.Fatalf("after the lost race: %+v", got)
	}

	// A new listing at a previous hostname is found by its own hostname.
	e.clock.Advance(time.Minute)
	newID := e.registerVerified(other, "irc.conf.example", nil)
	if l := e.moveListing(e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.conf.example", "", nil), 200)); l.Network.ID != newID || l.MovedFrom != "" {
		t.Fatalf("lookup of a reused hostname: %+v", l)
	}
	// While that new claim is pending and unseen, the old hostname still
	// resolves to the moved listing.
	e.exec("UPDATE networks SET status='pending' WHERE id=$1", newID)
	if l := e.moveListing(e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.conf.example", "", nil), 200)); l.Network.ID != id || l.MovedFrom != "irc.conf.example" {
		t.Fatalf("lookup falls back to the moved listing: %+v", l)
	}
}

// Same hostname, new port or tls: no DNS proof needed, applied at once.
func TestMovePortOnly(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-port-hhhhhhhhhhhhhhhhhhhh")
	id := e.registerVerified(k, "irc.port.example", nil)
	l := e.moveListing(e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.port.example", "port": 7000, "tls": false}), 200))
	if l.Move != nil || l.Network.Port != 7000 || l.Network.TLS || l.Network.Hostname != "irc.port.example" || len(l.Network.PreviousHostnames) != 0 {
		t.Fatalf("port change: %+v", l)
	}
	if acts := e.auditActions(t); !slices.Contains(acts, "network.move/network_key") {
		t.Fatalf("audit: %v", acts)
	}
}

// A health probe that started before a move doesn't write over the new address.
func TestHealthResultIgnoredAfterMove(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-health-iiiiiiiiiiiiiiiiii")
	id := e.registerVerified(k, "irc.h.example", nil)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move", k, map[string]any{"hostname": "irc.h2.example"}), 201)
	e.expect(e.do("POST", "/v1/networks/"+id+"/move/verify", k, nil), 200)
	repo := e.app.Deps.NetworkRepo
	if err := repo.RecordHealth(context.Background(), network.HealthResult{ID: id, Hostname: "irc.h.example", Status: "up", CheckedAt: e.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	var hs string
	_ = e.sqlDB.QueryRow("SELECT health_status FROM networks WHERE id=$1", id).Scan(&hs)
	if hs != "unknown" {
		t.Fatalf("stale probe recorded: %s", hs)
	}
	due, err := repo.DueForHealth(context.Background(), e.clock.Now(), 10)
	if err != nil || len(due) != 1 || due[0].Hostname != "irc.h2.example" {
		t.Fatalf("health due after the move: %v %+v", err, due)
	}
	if err := repo.RecordHealth(context.Background(), network.HealthResult{ID: id, Hostname: "irc.h2.example", Status: "up", CheckedAt: e.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = e.sqlDB.QueryRow("SELECT health_status FROM networks WHERE id=$1", id).Scan(&hs)
	if hs != "up" {
		t.Fatalf("probe of the new address: %s", hs)
	}
}
