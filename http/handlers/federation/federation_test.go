package federation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/services/federation"
)

func TestFeed(t *testing.T) {
	a := newInstance(t)
	v1 := a.local("irc.one.test", network.StatusVerified)
	a.local("irc.pending.test", network.StatusPending)
	v2 := a.local("irc.two.test", network.StatusVerified)

	var page federation.FeedPage
	if code := a.do("GET", "/v1/feed", "", nil, &page); code != 200 {
		t.Fatalf("feed: %d", code)
	}
	if page.HasMore || len(page.Changes) != 3 {
		t.Fatalf("feed: %+v", page)
	}
	got := page.Changes[0]
	if got.Network.ID != v1.ID || got.VerificationToken != v1.VerificationToken || got.Deleted || got.Network.Name != v1.Name {
		t.Fatalf("first change: %+v", got)
	}
	if !page.Changes[1].Deleted || page.Changes[1].Network.Hostname != "" {
		t.Fatalf("pending listing must be a bare deletion: %+v", page.Changes[1])
	}

	// Paging.
	var p1, p2 federation.FeedPage
	a.do("GET", "/v1/feed?limit=2", "", nil, &p1)
	if len(p1.Changes) != 2 || !p1.HasMore {
		t.Fatalf("page 1: %+v", p1)
	}
	a.do("GET", "/v1/feed?limit=2&since="+itoa(p1.NextSince), "", nil, &p2)
	if len(p2.Changes) != 1 || p2.HasMore || p2.Changes[0].Network.ID != v2.ID {
		t.Fatalf("page 2: %+v", p2)
	}

	// A deletion shows up after the cursor.
	if err := a.App.Deps.NetworkRepo.Delete(context.Background(), v1.ID); err != nil {
		t.Fatal(err)
	}
	var p3 federation.FeedPage
	a.do("GET", "/v1/feed?since="+itoa(page.NextSince), "", nil, &p3)
	if len(p3.Changes) != 1 || !p3.Changes[0].Deleted || p3.Changes[0].Network.ID != v1.ID {
		t.Fatalf("tombstone: %+v", p3)
	}

	for _, q := range []string{"since=-1", "since=x", "limit=0", "limit=1001"} {
		if code := a.do("GET", "/v1/feed?"+q, "", nil, nil); code != 400 {
			t.Errorf("%s: %d", q, code)
		}
	}
}

func TestPeerAPIAuth(t *testing.T) {
	a := newInstance(t)
	if code := a.do("GET", "/v1/admin/peers", "", nil, nil); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code := a.do("GET", "/v1/admin/peers", "user", nil, nil); code != 403 {
		t.Fatalf("no perm: %d", code)
	}
	if code := a.do("GET", "/v1/admin/peers", "op", nil, nil); code != 200 {
		t.Fatalf("operator: %d", code)
	}
}

func TestAddPeerValidation(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	var r peerResp
	if code := a.do("POST", "/v1/admin/peers", "op", map[string]string{"url": a.URL}, &r); code != 400 || r.Error != "self_peer" {
		t.Fatalf("self by URL: %d %s", code, r.Error)
	}
	if code := a.do("POST", "/v1/admin/peers", "op", map[string]string{"url": "ftp://x.test"}, &r); code != 400 {
		t.Fatalf("bad URL: %d", code)
	}
	if code := a.do("POST", "/v1/admin/peers", "op", map[string]string{"url": "http://127.0.0.1:1"}, &r); code != 502 || r.Error != "peer_unreachable" {
		t.Fatalf("unreachable: %d %s", code, r.Error)
	}
	id := a.addPeer(b.URL + "/")
	if code := a.do("POST", "/v1/admin/peers", "op", map[string]string{"url": b.URL}, &r); code != 409 || r.Error != "peer_exists" {
		t.Fatalf("duplicate: %d %s", code, r.Error)
	}
	var got peerResp
	a.do("GET", "/v1/admin/peers/"+id, "op", nil, &got)
	if got.Peer.URL != b.URL || got.Peer.Name != "boson directory" || !got.Peer.Enabled || got.Peer.Builtin {
		t.Fatalf("peer: %+v", got.Peer)
	}
	name := "B dir"
	a.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"name": name}, &got)
	if got.Peer.Name != name {
		t.Fatalf("rename: %+v", got.Peer)
	}
	if code := a.do("DELETE", "/v1/admin/peers/"+id, "op", nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if code := a.do("GET", "/v1/admin/peers/"+id, "op", nil, nil); code != 404 {
		t.Fatalf("deleted peer: %d", code)
	}
}

// A peer that advertises this directory's own API is refused even under
// another URL (e.g. an alias hostname).
func TestNeverPeersWithItselfByWellKnown(t *testing.T) {
	a := newInstance(t)
	alias := strings.Replace(a.URL, "127.0.0.1", "localhost", 1)
	var r peerResp
	if code := a.do("POST", "/v1/admin/peers", "op", map[string]string{"url": alias}, &r); code != 400 || r.Error != "self_peer" {
		t.Fatalf("self by .well-known: %d %s", code, r.Error)
	}
}

func TestImportReverifyDeleteAndUpdate(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	one := a.local("irc.one.test", network.StatusVerified)
	a.local("irc.two.test", network.StatusVerified)
	a.local("irc.pending.test", network.StatusPending)
	// B's DNS view: irc.two.test publishes some other token.
	b.Fake.SetToken("irc.two.test", "not-the-token")

	id := b.addPeer(a.URL)
	res := b.sync(id)
	if res.Created != 2 || res.Verified != 1 || !res.Full {
		t.Fatalf("first sync: %+v", res)
	}
	got := b.byHost("irc.one.test")
	if got == nil || got.Origin != "peer:"+id || *got.OriginID != one.ID || got.Status != network.StatusVerified ||
		got.VerificationToken != one.VerificationToken || got.OwnerAuthUserID != nil || got.Name != one.Name {
		t.Fatalf("imported one: %+v", got)
	}
	if two := b.byHost("irc.two.test"); two == nil || two.Status != network.StatusPending {
		t.Fatalf("irc.two.test must stay pending until B's own DNS check passes: %+v", two)
	}
	if b.byHost("irc.pending.test") != nil {
		t.Fatal("unverified listing was exported")
	}
	// B never re-exports what it imported.
	var feed federation.FeedPage
	b.do("GET", "/v1/feed", "", nil, &feed)
	if len(feed.Changes) != 0 {
		t.Fatalf("B re-exported imports: %+v", feed.Changes)
	}

	// Edit at the origin propagates; a new token sends it back to pending
	// and B checks it again.
	one.Name = "Renamed"
	if err := a.App.Deps.NetworkRepo.Update(context.Background(), one, "name"); err != nil {
		t.Fatal(err)
	}
	if res := b.sync(id); res.Updated != 1 || res.Full {
		t.Fatalf("incremental sync: %+v", res)
	}
	if got := b.byHost("irc.one.test"); got.Name != "Renamed" || got.Status != network.StatusVerified {
		t.Fatalf("after edit: %+v", got)
	}
	one.VerificationToken = "tok-rotated-1234"
	if err := a.App.Deps.NetworkRepo.Update(context.Background(), one, "verification_token"); err != nil {
		t.Fatal(err)
	}
	b.Fake.SetToken("irc.one.test", "tok-old-record")
	b.sync(id)
	if got := b.byHost("irc.one.test"); got.Status != network.StatusPending || got.VerificationToken != "tok-rotated-1234" {
		t.Fatalf("rotated token must re-verify: %+v", got)
	}
	b.Fake.SetToken("irc.one.test", "tok-rotated-1234")
	b.App.Deps.Networks.CheckOne(context.Background(), b.byHost("irc.one.test"))
	if got := b.byHost("irc.one.test"); got.Status != network.StatusVerified {
		t.Fatalf("verify worker should confirm the imported listing: %+v", got)
	}

	// Lapsing or deleting at the origin removes the import.
	a.setStatus(one, network.StatusLapsed)
	b.sync(id)
	if b.byHost("irc.one.test") != nil {
		t.Fatal("lapsed listing still imported")
	}
	two := a.byHost("irc.two.test")
	if err := a.App.Deps.NetworkRepo.Delete(context.Background(), two.ID); err != nil {
		t.Fatal(err)
	}
	if res := b.sync(id); res.Deleted != 1 {
		t.Fatalf("delete: %+v", res)
	}
	if b.byHost("irc.two.test") != nil {
		t.Fatal("deleted listing still imported")
	}
	var pr peerResp
	b.do("GET", "/v1/admin/peers/"+id, "op", nil, &pr)
	if pr.Peer.ListingCount != 0 || pr.Peer.LastSyncAt == nil || pr.Peer.LastError != nil {
		t.Fatalf("peer status: %+v", pr.Peer)
	}
}

func TestConflictRules(t *testing.T) {
	a := newInstance(t)
	c := newInstance(t)
	b := newInstance(t)
	// Local beats any peer.
	a.local("irc.local.test", network.StatusVerified)
	mine := b.local("irc.local.test", network.StatusPending)
	// Both A and C list irc.shared.test; B can verify A's token only.
	a.local("irc.shared.test", network.StatusVerified)
	c.local("irc.shared.test", network.StatusVerified)
	b.Fake.SetToken("irc.shared.test", "tok-irc.shared.test")
	// Both list irc.both.test with the same token (it's the same DNS).
	a.local("irc.both.test", network.StatusVerified)
	c.local("irc.both.test", network.StatusVerified)

	pa := b.addPeer(a.URL)
	pc := b.addPeer(c.URL)
	b.sync(pa)
	res := b.sync(pc)
	if res.Conflicts != 2 {
		t.Fatalf("C sync conflicts: %+v", res)
	}
	if got := b.byHost("irc.local.test"); got.ID != mine.ID || !got.IsLocal() {
		t.Fatalf("local listing lost: %+v", got)
	}
	if got := b.byHost("irc.shared.test"); got.Origin != "peer:"+pa {
		t.Fatalf("first verified peer must win: %+v", got)
	}
	if got := b.byHost("irc.both.test"); got.Origin != "peer:"+pa {
		t.Fatalf("first verified peer must win: %+v", got)
	}
}

func TestUnverifiedPeerListingIsReplaced(t *testing.T) {
	a := newInstance(t)
	c := newInstance(t)
	b := newInstance(t)
	a.local("irc.x.test", network.StatusVerified)
	c.local("irc.x.test", network.StatusVerified)
	// Only C's token is in DNS as B sees it.
	b.Fake.SetToken("irc.x.test", "tok-irc.x.test-c")
	cn := c.byHost("irc.x.test")
	cn.VerificationToken = "tok-irc.x.test-c"
	if err := c.App.Deps.NetworkRepo.Update(context.Background(), cn, "verification_token"); err != nil {
		t.Fatal(err)
	}
	pa := b.addPeer(a.URL)
	pc := b.addPeer(c.URL)
	b.sync(pa)
	if got := b.byHost("irc.x.test"); got.Status != network.StatusPending {
		t.Fatalf("A's copy: %+v", got)
	}
	b.sync(pc)
	if got := b.byHost("irc.x.test"); got.Origin != "peer:"+pc || got.Status != network.StatusVerified {
		t.Fatalf("C's verified copy should replace A's unverified one: %+v", got)
	}
}

func TestBlocksApplyToImports(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	a.local("irc.bad.test", network.StatusVerified)
	a.local("chat.evil.test", network.StatusVerified)
	a.local("irc.good.test", network.StatusVerified)
	ctx := context.Background()
	if _, _, err := b.App.Deps.Blocks.Add(ctx, nil, "*.evil.test", "spam"); err != nil {
		t.Fatal(err)
	}
	id := b.addPeer(a.URL)
	res := b.sync(id)
	if res.Blocked != 1 || b.byHost("chat.evil.test") != nil || b.byHost("irc.bad.test") == nil {
		t.Fatalf("block before import: %+v", res)
	}
	// Blocking later blocks the import; the next sync removes it.
	if _, _, err := b.App.Deps.Blocks.Add(ctx, nil, "irc.bad.test", ""); err != nil {
		t.Fatal(err)
	}
	if got := b.byHost("irc.bad.test"); got.Status != network.StatusBlocked {
		t.Fatalf("after block: %+v", got)
	}
	// Force the origin to re-send it (full resync).
	setFullResyncDue(t, b, id)
	b.sync(id)
	if b.byHost("irc.bad.test") != nil {
		t.Fatal("blocked import kept")
	}
	if b.byHost("irc.good.test") == nil {
		t.Fatal("unblocked listing lost")
	}
}

func setFullResyncDue(t *testing.T, in *instance, peerID string) {
	t.Helper()
	if err := in.App.DB.DB.Exec("UPDATE peers SET last_full_sync_at = now() - interval '30 days' WHERE id = ?", peerID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestFullResyncSweepsWhatThePeerNoLongerHas(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	gone := a.local("irc.gone.test", network.StatusVerified)
	a.local("irc.stays.test", network.StatusVerified)
	id := b.addPeer(a.URL)
	b.sync(id)
	// Delete at A and prune the tombstone, so the incremental feed can't tell.
	ctx := context.Background()
	if err := a.App.Deps.NetworkRepo.Delete(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.App.DB.DB.Exec("DELETE FROM network_tombstones").Error; err != nil {
		t.Fatal(err)
	}
	if res := b.sync(id); res.Full || b.byHost("irc.gone.test") == nil {
		t.Fatalf("incremental sync can't see a pruned tombstone: %+v", res)
	}
	setFullResyncDue(t, b, id)
	if res := b.sync(id); !res.Full || res.Deleted != 1 {
		t.Fatalf("full resync: %+v", res)
	}
	if b.byHost("irc.gone.test") != nil || b.byHost("irc.stays.test") == nil {
		t.Fatal("full resync sweep")
	}
}

func TestMaxListingsPerPeer(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t, func(c *configT) { c.FederationConfig.MaxListingsPerPeer = 2 })
	for _, h := range []string{"irc.a.test", "irc.b.test", "irc.c.test"} {
		a.local(h, network.StatusVerified)
	}
	id := b.addPeer(a.URL)
	var r peerResp
	b.do("POST", "/v1/admin/peers/"+id+"/sync", "op", nil, &r)
	if r.Result.Created != 2 || r.Result.Skipped != 1 || r.Peer.LastError == nil || !strings.Contains(*r.Peer.LastError, "listing limit") {
		t.Fatalf("limit: %+v %+v", r.Result, r.Peer)
	}
}

func TestRejectsInvalidImports(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	bad := a.local("irc.bad.test", network.StatusVerified)
	a.local("irc.ok.test", network.StatusVerified)
	// Corrupt it behind the service's back (what a hostile peer could send).
	if err := a.App.DB.DB.Exec("UPDATE networks SET website_url = 'javascript:alert(1)' WHERE id = ?", bad.ID).Error; err != nil {
		t.Fatal(err)
	}
	id := b.addPeer(a.URL)
	res := b.sync(id)
	if res.Rejected != 1 || res.Created != 1 || len(res.Problems) != 1 || b.byHost("irc.bad.test") != nil {
		t.Fatalf("validation: %+v", res)
	}
}

func TestDisableAndDeletePeer(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	a.local("irc.one.test", network.StatusVerified)
	id := b.addPeer(a.URL)
	b.sync(id)
	var r peerResp
	b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": false}, &r)
	if r.Peer.Enabled || r.Peer.ListingCount != 0 || b.byHost("irc.one.test") != nil {
		t.Fatalf("disable must remove listings: %+v", r.Peer)
	}
	if code := b.do("POST", "/v1/admin/peers/"+id+"/sync", "op", nil, &r); code != 409 || r.Error != "peer_disabled" {
		t.Fatalf("sync disabled: %d %s", code, r.Error)
	}
	b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": true}, &r)
	b.sync(id)
	if b.byHost("irc.one.test") == nil {
		t.Fatal("re-enable must re-import from scratch")
	}
	if code := b.do("DELETE", "/v1/admin/peers/"+id, "op", nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if b.byHost("irc.one.test") != nil {
		t.Fatal("delete must remove listings")
	}
}

func TestBuiltinMainPeer(t *testing.T) {
	main := newInstance(t)
	main.local("irc.main.test", network.StatusVerified)
	b := newInstance(t, withMain(main.URL, true))

	var list peersResp
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 || !list.Peers[0].Builtin || !list.Peers[0].Enabled || list.Peers[0].URL != main.URL {
		t.Fatalf("builtin peer: %+v", list)
	}
	id := list.Peers[0].ID
	if code := b.do("DELETE", "/v1/admin/peers/"+id, "op", nil, nil); code != 409 {
		t.Fatalf("builtin delete: %d", code)
	}
	// The background worker syncs it.
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if got := b.byHost("irc.main.test"); got == nil || got.Status != network.StatusVerified {
		t.Fatalf("main directory listing: %+v", got)
	}

	// Runtime disable persists across a restart.
	var r peerResp
	b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": false}, &r)
	if b.byHost("irc.main.test") != nil {
		t.Fatal("disabled main peer listings remain")
	}
	b.restart()
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 || list.Peers[0].ID != id || list.Peers[0].Enabled {
		t.Fatalf("after restart: %+v", list)
	}
	_ = b.App.RunWorkerOnce(context.Background(), federation.WorkerName)
	if b.byHost("irc.main.test") != nil {
		t.Fatal("disabled main peer synced after restart")
	}

	// Re-enable, sync, then turn it off by configuration.
	b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": true}, &r)
	b.sync(id)
	if b.byHost("irc.main.test") == nil {
		t.Fatal("re-enabled main peer")
	}
	b.restart(withMain(main.URL, false))
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 0 {
		t.Fatalf("DIRECTORY_INCLUDE_MAIN=false must hide the builtin: %+v", list)
	}
	if b.byHost("irc.main.test") != nil {
		t.Fatal("DIRECTORY_INCLUDE_MAIN=false must remove main listings")
	}
	if code := b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": true}, nil); code != 404 {
		t.Fatalf("hidden builtin patch: %d", code)
	}
	// Back on: the same row, still enabled (env off isn't an operator disable).
	b.restart(withMain(main.URL, true))
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 || list.Peers[0].ID != id || !list.Peers[0].Enabled {
		t.Fatalf("include main again: %+v", list)
	}
}

func TestMainDirectoryDoesNotPeerWithItself(t *testing.T) {
	a := newInstance(t, func(c *configT) {
		c.FederationConfig.MainPeer, c.FederationConfig.IncludeMain = c.HTTPConfig.PublicURL, true
	})
	var list peersResp
	a.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 0 {
		t.Fatalf("main directory peered with itself: %+v", list)
	}
}

func TestSeedPeersFromEnv(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t, func(c *configT) { c.FederationConfig.Peers = a.URL + ", " + c.HTTPConfig.PublicURL + ",not a url" })
	var list peersResp
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 || list.Peers[0].URL != a.URL || list.Peers[0].Builtin {
		t.Fatalf("seeded: %+v", list)
	}
	b.restart()
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 {
		t.Fatalf("seeding twice: %+v", list)
	}
}

func TestSyncRecordsPeerErrors(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	id := b.addPeer(a.URL)
	a.stop()
	var r peerResp
	if code := b.do("POST", "/v1/admin/peers/"+id+"/sync", "op", nil, &r); code != 200 {
		t.Fatalf("sync: %d", code)
	}
	if r.Result.Error == "" || r.Peer.LastError == nil || r.Peer.LastSyncAt == nil {
		t.Fatalf("unreachable peer: %+v %+v", r.Result, r.Peer)
	}
}
