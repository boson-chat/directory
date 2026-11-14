package federation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/services/federation"
)

type settingsResp struct {
	Settings struct {
		Name        string `json:"name"`
		Operator    string `json:"operator"`
		Contact     string `json:"contact"`
		IncludeMain bool   `json:"include_main"`
		MainPeerURL string `json:"main_peer_url"`
		SyncEvery   string `json:"sync_every"`
	} `json:"settings"`
	Defaults struct {
		Name        string `json:"name"`
		IncludeMain bool   `json:"include_main"`
	} `json:"defaults"`
	Overridden []string `json:"overridden"`
	IsMain     bool     `json:"is_main"`
	MainActive bool     `json:"main_active"`
	MainPeer   *struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
		Builtin bool   `json:"builtin"`
	} `json:"main_peer"`
	UpdatedBy *string `json:"updated_by"`
	Error     string  `json:"error"`
}

func (in *instance) settings() settingsResp {
	in.t.Helper()
	var s settingsResp
	if code := in.do("GET", "/v1/admin/settings", "op", nil, &s); code != 200 {
		in.t.Fatalf("get settings: %d", code)
	}
	return s
}

func (in *instance) patchSettings(body map[string]any) settingsResp {
	in.t.Helper()
	var s settingsResp
	if code := in.do("PATCH", "/v1/admin/settings", "op", body, &s); code != 200 {
		in.t.Fatalf("patch settings %v: %d %s", body, code, s.Error)
	}
	return s
}

func TestSettingsAPIAuth(t *testing.T) {
	a := newInstance(t)
	if code := a.do("GET", "/v1/admin/settings", "", nil, nil); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code := a.do("PATCH", "/v1/admin/settings", "user", map[string]any{"name": "x"}, nil); code != 403 {
		t.Fatalf("non-operator: %d", code)
	}
}

func TestSettingsNameOperatorContact(t *testing.T) {
	a := newInstance(t, func(c *configT) { c.DirectoryConfig.Name, c.DirectoryConfig.Operator = "Env Dir", "env op" })
	s := a.settings()
	if s.Settings.Name != "Env Dir" || s.Settings.Operator != "env op" || len(s.Overridden) != 0 || s.Settings.SyncEvery != "10m" {
		t.Fatalf("defaults: %+v", s)
	}

	s = a.patchSettings(map[string]any{"name": "  My Directory ", "operator": "", "contact": "ops@example.test"})
	if s.Settings.Name != "My Directory" || s.Settings.Operator != "" || s.Settings.Contact != "ops@example.test" ||
		s.Defaults.Name != "Env Dir" || len(s.Overridden) != 3 || s.UpdatedBy == nil {
		t.Fatalf("patched: %+v", s)
	}
	var wk struct{ Name, Operator, Contact string }
	a.do("GET", "/.well-known/boson-directory.json", "", nil, &wk)
	if wk.Name != "My Directory" || wk.Operator != "" || wk.Contact != "ops@example.test" {
		t.Fatalf(".well-known doesn't follow the settings: %+v", wk)
	}

	// Overrides survive a restart and win over a changed env default.
	a.restart(func(c *configT) { c.DirectoryConfig.Name = "Other Env" })
	if s := a.settings(); s.Settings.Name != "My Directory" || s.Defaults.Name != "Other Env" {
		t.Fatalf("after restart: %+v", s)
	}
	// null resets to the env default.
	s = a.patchSettings(map[string]any{"name": nil, "operator": nil})
	if s.Settings.Name != "Other Env" || s.Settings.Operator != "env op" || len(s.Overridden) != 1 {
		t.Fatalf("reset: %+v", s)
	}

	for _, bad := range []map[string]any{
		{"name": "   "}, {"sync_every": "5s"}, {"sync_every": "soon"}, {"main_peer_url": "ftp://x"}, {},
		{"contact": strings.Repeat("x", 201)},
	} {
		if code := a.do("PATCH", "/v1/admin/settings", "op", bad, nil); code != 400 {
			t.Fatalf("PATCH %v: %d, want 400", bad, code)
		}
	}
	if s := a.patchSettings(map[string]any{"sync_every": "1h30m"}); s.Settings.SyncEvery != "1h30m" {
		t.Fatalf("sync_every: %+v", s.Settings)
	}

	var audit struct {
		Entries []struct{ Action string } `json:"entries"`
	}
	a.do("GET", "/v1/admin/audit?action=settings.update", "op", nil, &audit)
	n := 0
	for _, e := range audit.Entries {
		if e.Action == "settings.update" {
			n++
		}
	}
	if n < 3 {
		t.Fatalf("settings changes not audited: %+v", audit)
	}
}

// include_main switches the builtin main peer on and off at runtime, and
// the choice beats DIRECTORY_INCLUDE_MAIN across restarts.
func TestSettingsIncludeMainAtRuntime(t *testing.T) {
	main := newInstance(t)
	main.local("irc.main.test", network.StatusVerified)
	b := newInstance(t, withMain(main.URL, false))

	var list peersResp
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if s := b.settings(); s.Settings.IncludeMain || s.IsMain || s.MainActive || s.MainPeer != nil || len(list.Peers) != 0 {
		t.Fatalf("env off: %+v %+v", s, list)
	}

	s := b.patchSettings(map[string]any{"include_main": true})
	if !s.Settings.IncludeMain || !s.MainActive || s.MainPeer == nil || !s.MainPeer.Builtin || !s.MainPeer.Enabled {
		t.Fatalf("switched on: %+v", s)
	}
	id := s.MainPeer.ID
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if b.byHost("irc.main.test") == nil {
		t.Fatal("main directory not synced after include_main=true")
	}

	// A restart keeps the runtime choice although the env still says false.
	b.restart()
	if s := b.settings(); !s.Settings.IncludeMain || !s.MainActive || s.MainPeer.ID != id {
		t.Fatalf("after restart: %+v", s)
	}

	s = b.patchSettings(map[string]any{"include_main": false})
	if s.Settings.IncludeMain || s.MainActive {
		t.Fatalf("switched off: %+v", s)
	}
	if b.byHost("irc.main.test") != nil {
		t.Fatal("include_main=false must remove the main directory's listings at once")
	}
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 0 {
		t.Fatalf("builtin still listed: %+v", list)
	}
	b.restart(withMain(main.URL, true))
	if s := b.settings(); s.Settings.IncludeMain || s.MainActive {
		t.Fatalf("env true must not beat the runtime false: %+v", s)
	}

	// An operator-disabled builtin comes back with include_main=true.
	b.patchSettings(map[string]any{"include_main": nil}) // env default (true)
	b.do("PATCH", "/v1/admin/peers/"+id, "op", map[string]any{"enabled": false}, nil)
	if s := b.settings(); s.MainActive || s.MainPeer == nil || s.MainPeer.Enabled || !s.Settings.IncludeMain {
		t.Fatalf("operator disabled: %+v", s)
	}
	if s := b.patchSettings(map[string]any{"include_main": true}); !s.MainActive || !s.MainPeer.Enabled || s.MainPeer.ID != id {
		t.Fatalf("include_main=true must re-enable the builtin: %+v", s)
	}
}

func TestSettingsMainPeerURL(t *testing.T) {
	m1 := newInstance(t)
	m2 := newInstance(t)
	m1.local("irc.m1.test", network.StatusVerified)
	m2.local("irc.m2.test", network.StatusVerified)
	b := newInstance(t, withMain(m1.URL, true))
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if b.byHost("irc.m1.test") == nil {
		t.Fatal("m1 not synced")
	}
	s := b.patchSettings(map[string]any{"main_peer_url": m2.URL + "/"})
	if s.Settings.MainPeerURL != m2.URL || !s.MainActive {
		t.Fatalf("re-pointed: %+v", s)
	}
	if b.byHost("irc.m1.test") != nil {
		t.Fatal("old main directory's listings remain")
	}
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if b.byHost("irc.m2.test") == nil {
		t.Fatal("new main directory not synced")
	}
}

// The main directory itself: is_main, no builtin, include_main refused.
func TestSettingsIsMain(t *testing.T) {
	a := newInstance(t, func(c *configT) {
		c.FederationConfig.MainPeer, c.FederationConfig.IncludeMain = c.HTTPConfig.PublicURL, true
	})
	s := a.settings()
	if !s.IsMain || s.MainActive || s.MainPeer != nil {
		t.Fatalf("main directory: %+v", s)
	}
	var e settingsResp
	if code := a.do("PATCH", "/v1/admin/settings", "op", map[string]any{"include_main": true}, &e); code != 409 || e.Error != "is_main_directory" {
		t.Fatalf("include_main on the main directory: %d %+v", code, e)
	}
	// Switching it off is harmless (and pointless) — allowed.
	a.patchSettings(map[string]any{"include_main": false})
	other := newInstance(t)
	if s := a.patchSettings(map[string]any{"main_peer_url": other.URL, "include_main": true}); s.IsMain || !s.MainActive {
		t.Fatalf("pointed elsewhere: %+v", s)
	}
}

// DIRECTORY_PEERS seeds each URL once; a removed peer stays removed.
func TestSeededPeerStaysDeleted(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t, func(c *configT) { c.FederationConfig.Peers = a.URL })
	var list peersResp
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 1 {
		t.Fatalf("seeded: %+v", list)
	}
	if code := b.do("DELETE", "/v1/admin/peers/"+list.Peers[0].ID, "op", nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	b.restart()
	b.do("GET", "/v1/admin/peers", "op", nil, &list)
	if len(list.Peers) != 0 {
		t.Fatalf("deleted seed peer came back: %+v", list)
	}
}

// The worker syncs each peer once per sync_every.
func TestSyncWorkerHonoursSyncEvery(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	id := b.addPeer(a.URL)
	b.sync(id)
	a.local("irc.later.test", network.StatusVerified)
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if b.byHost("irc.later.test") != nil {
		t.Fatal("peer synced again before sync_every elapsed")
	}
	// A brand-new peer is due at once.
	c := newInstance(t)
	c.local("irc.c.test", network.StatusVerified)
	b.addPeer(c.URL)
	if err := b.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if b.byHost("irc.c.test") == nil {
		t.Fatal("new peer not synced by the worker")
	}
}
