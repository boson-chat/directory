package federation_test

import (
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/services/federation"
)

// writeConfig replaces the file like the admin panel (tmp + rename).
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	tmp := filepath.Join(filepath.Dir(path), ".boson.yaml.tmp")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func withConfigFile(path string) instOpt {
	return func(c *configT) { c.AppConfig.ConfigFile = path }
}

type filePeer struct {
	ID                  string `json:"id"`
	URL                 string `json:"url"`
	Enabled             bool   `json:"enabled"`
	Builtin             bool   `json:"builtin"`
	ManagedByConfigFile bool   `json:"managed_by_config_file"`
	ListingCount        int    `json:"listing_count"`
}

func (in *instance) filePeers() map[string]filePeer {
	in.t.Helper()
	var list struct{ Peers []filePeer }
	if code := in.do("GET", "/v1/admin/peers", "op", nil, &list); code != 200 {
		in.t.Fatalf("list peers: %d", code)
	}
	out := map[string]filePeer{}
	for _, p := range list.Peers {
		out[p.URL] = p
	}
	return out
}

func (in *instance) configStatus() map[string]any {
	in.t.Helper()
	rec := httptest.NewRecorder()
	in.App.Internal.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/config/status", nil))
	if rec.Code != 200 {
		in.t.Fatalf("status: %d", rec.Code)
	}
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	return st
}

func TestConfigFileSettingsAndPeers(t *testing.T) {
	b := newInstance(t)
	c := newInstance(t)
	b.local("irc.b.test", network.StatusVerified)
	path := filepath.Join(t.TempDir(), "boson.yaml")
	writeConfig(t, path, `version: 1
directory:
  name: File Dir
  operator: File Ops
  sync_every: 30m
  peers: [`+b.URL+`]
`)
	a := newInstance(t, withConfigFile(path), func(cfg *configT) { cfg.DirectoryConfig.Contact = "env@example.test" })
	ctx := context.Background()

	// Settings: file > env, read-only.
	s := a.settings()
	if s.Settings.Name != "File Dir" || s.Settings.Operator != "File Ops" || s.Settings.Contact != "env@example.test" || s.Settings.SyncEvery != "30m" {
		t.Fatalf("settings: %+v", s)
	}
	var raw map[string]any
	a.do("GET", "/v1/admin/settings", "op", nil, &raw)
	if raw["source"] != "file" {
		t.Fatalf("source: %v", raw["source"])
	}
	var e struct{ Error string }
	if code := a.do("PATCH", "/v1/admin/settings", "op", map[string]any{"name": "x"}, &e); code != 409 || e.Error != "managed_by_config_file" {
		t.Fatalf("PATCH settings: %d %s", code, e.Error)
	}
	var wk struct{ Name, Operator, Contact string }
	a.do("GET", "/.well-known/boson-directory.json", "", nil, &wk)
	if wk.Name != "File Dir" || wk.Operator != "File Ops" {
		t.Fatalf(".well-known: %+v", wk)
	}
	if st := a.configStatus(); st["ok"] != true || st["present"] != true || st["enabled"] != true {
		t.Fatalf("status: %v", st)
	}

	// The file's peer exists, is enabled and is owned by the file.
	pb := a.filePeers()[b.URL]
	if pb.ID == "" || !pb.Enabled || !pb.ManagedByConfigFile {
		t.Fatalf("file peer: %+v", a.filePeers())
	}
	if code := a.do("PATCH", "/v1/admin/peers/"+pb.ID, "op", map[string]any{"enabled": false}, &e); code != 409 || e.Error != "managed_by_config_file" {
		t.Fatalf("PATCH file peer: %d %s", code, e.Error)
	}
	if code := a.do("DELETE", "/v1/admin/peers/"+pb.ID, "op", nil, &e); code != 409 {
		t.Fatalf("DELETE file peer: %d", code)
	}
	a.sync(pb.ID) // syncing is fine
	if a.byHost("irc.b.test") == nil {
		t.Fatal("file peer must sync")
	}
	// Peers not in the file stay under the API.
	cid := a.addPeer(c.URL)
	if code := a.do("PATCH", "/v1/admin/peers/"+cid, "op", map[string]any{"name": "C"}, nil); code != 200 {
		t.Fatalf("PATCH API peer: %d", code)
	}

	// Reload: b dropped, c listed, sync_every changed.
	writeConfig(t, path, `version: 1
directory:
  name: File Dir
  operator: File Ops
  sync_every: 1h
  peers: [`+c.URL+`]
`)
	a.App.StackConfig.Check(ctx)
	peers := a.filePeers()
	if p := peers[b.URL]; p.ID != pb.ID || p.Enabled || p.ManagedByConfigFile || p.ListingCount != 0 {
		t.Fatalf("dropped peer must be disabled, kept and released: %+v", p)
	}
	if a.byHost("irc.b.test") != nil {
		t.Fatal("disabled peer's listings must go")
	}
	if p := peers[c.URL]; p.ID != cid || !p.Enabled || !p.ManagedByConfigFile {
		t.Fatalf("listed API peer becomes file-managed: %+v", p)
	}
	if s := a.settings(); s.Settings.SyncEvery != "1h" {
		t.Fatalf("sync_every live: %+v", s.Settings)
	}
	// Released: the API may re-enable b.
	if code := a.do("PATCH", "/v1/admin/peers/"+pb.ID, "op", map[string]any{"enabled": true}, nil); code != 200 {
		t.Fatalf("re-enable released peer: %d", code)
	}

	var audit struct {
		Entries []struct {
			Action    string         `json:"action"`
			ActorName string         `json:"actor_name"`
			Meta      map[string]any `json:"meta"`
		} `json:"entries"`
	}
	a.do("GET", "/v1/admin/audit?action=config.reloaded", "op", nil, &audit)
	found := false
	for _, en := range audit.Entries {
		if en.Action == "config.reloaded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("config.reloaded not audited: %+v", audit)
	}

	// A broken edit keeps the last good config.
	writeConfig(t, path, "directory:\n  name: File Dir\n  peers: nope: [\n")
	a.App.StackConfig.Check(ctx)
	if st := a.configStatus(); st["ok"] != false || st["error"] == "" {
		t.Fatalf("status: %v", st)
	}
	if a.settings().Settings.SyncEvery != "1h" || !a.filePeers()[c.URL].ManagedByConfigFile {
		t.Fatal("last good config must stay")
	}

	// Edited while down: c dropped from the file, applied at start-up.
	writeConfig(t, path, "directory:\n  name: Restarted\n  peers: []\n")
	a.restart()
	peers = a.filePeers()
	if p := peers[c.URL]; p.Enabled || p.ManagedByConfigFile {
		t.Fatalf("start-up must disable peers the file dropped: %+v", p)
	}
	if a.settings().Settings.Name != "Restarted" {
		t.Fatal("name after restart")
	}

	// File removed: database mode, the API owns everything again.
	_ = os.Remove(path)
	a.App.StackConfig.Check(ctx)
	if code := a.do("PATCH", "/v1/admin/settings", "op", map[string]any{"name": "DB name"}, nil); code != 200 {
		t.Fatalf("PATCH after file removal: %d", code)
	}
	if st := a.configStatus(); st["present"] != false || st["ok"] != true {
		t.Fatalf("status: %v", st)
	}
}

func TestConfigFileIncludeMain(t *testing.T) {
	main := newInstance(t)
	main.local("irc.main.test", network.StatusVerified)
	path := filepath.Join(t.TempDir(), "boson.yaml")
	writeConfig(t, path, "directory:\n  include_main: false\n  main_peer_url: "+main.URL+"\n")
	a := newInstance(t, withConfigFile(path), withMain("", true))
	if s := a.settings(); s.Settings.IncludeMain || s.MainActive || s.MainPeer != nil {
		t.Fatalf("include_main false from the file: %+v", s)
	}

	writeConfig(t, path, "directory:\n  include_main: true\n  main_peer_url: "+main.URL+"\n")
	a.App.StackConfig.Check(context.Background())
	s := a.settings()
	if !s.Settings.IncludeMain || !s.MainActive || s.MainPeer == nil || !s.MainPeer.Builtin || !s.MainPeer.Enabled {
		t.Fatalf("include_main on live: %+v", s)
	}
	var e struct{ Error string }
	if code := a.do("PATCH", "/v1/admin/peers/"+s.MainPeer.ID, "op", map[string]any{"enabled": false}, &e); code != 409 || e.Error != "managed_by_config_file" {
		t.Fatalf("builtin under include_main from the file: %d %s", code, e.Error)
	}
	if err := a.App.RunWorkerOnce(context.Background(), federation.WorkerName); err != nil {
		t.Fatal(err)
	}
	if a.byHost("irc.main.test") == nil {
		t.Fatal("main directory must sync")
	}
	// Facets reflect the file live: local source named by the file, the
	// main directory as a source.
	writeConfig(t, path, "directory:\n  name: Facet Dir\n  include_main: true\n  main_peer_url: "+main.URL+"\n")
	a.App.StackConfig.Check(context.Background())
	var facets struct {
		Sources []struct{ ID, Name, URL string } `json:"sources"`
	}
	if code := a.do("GET", "/v1/facets", "", nil, &facets); code != nethttp.StatusOK {
		t.Fatalf("facets: %d", code)
	}
	if len(facets.Sources) != 2 || facets.Sources[0].Name != "Facet Dir" || facets.Sources[1].URL != main.URL {
		t.Fatalf("facets: %+v", facets.Sources)
	}
}

func TestConfigStatusWithoutFile(t *testing.T) {
	a := newInstance(t)
	if st := a.configStatus(); st["enabled"] != false || st["ok"] != true {
		t.Fatalf("status: %v", st)
	}
	var raw map[string]any
	a.do("GET", "/v1/admin/settings", "op", nil, &raw)
	if raw["source"] != "db" {
		t.Fatal(raw["source"])
	}
}
