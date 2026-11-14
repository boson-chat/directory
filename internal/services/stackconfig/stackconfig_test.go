package stackconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boson-chat/directory/config"
	auditrepo "github.com/boson-chat/directory/internal/db/repositories/audit"
	settingsrepo "github.com/boson-chat/directory/internal/db/repositories/settings"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/settings"
)

type entry struct {
	actor, action string
	meta          map[string]any
}

type auditSpy struct{ entries []entry }

func (a *auditSpy) Record(_ context.Context, _ *principal.Principal, action string, _ audit.Target, meta map[string]any) {
	a.entries = append(a.entries, entry{"", action, meta})
}
func (a *auditSpy) System(_ context.Context, worker, action string, _ audit.Target, meta map[string]any) {
	a.entries = append(a.entries, entry{worker, action, meta})
}
func (a *auditSpy) List(context.Context, auditrepo.Filter) ([]*auditrepo.Entry, error) {
	return nil, nil
}

type fixture struct {
	svc      *StackConfigService
	settings *settings.SettingsService
	audit    *auditSpy
	path     string
	reloads  int
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path+".tmp", []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T, content string) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boson.yaml")
	if content != "" {
		write(t, path, content)
	}
	f := &fixture{audit: &auditSpy{}, path: path}
	f.settings = settings.NewSettingsService(settings.Deps{Repo: &settingsrepo.Memory{}, Defaults: settings.Values{
		Name: "env", IncludeMain: true, MainPeerURL: "https://directory.boson.chat", SyncEvery: 10 * time.Minute,
	}})
	f.svc = NewStackConfigService(Deps{
		Config: config.AppConfig{ConfigFile: path, ConfigPoll: time.Hour}, Settings: f.settings, Audit: f.audit,
	})
	f.svc.SetReloaded(func(context.Context) error { f.reloads++; return nil })
	return f
}

func TestWithoutConfigFile(t *testing.T) {
	f := newFixture(t, "")
	f.svc = NewStackConfigService(Deps{Settings: f.settings, Audit: f.audit})
	if err := f.svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.settings.File() != nil || f.svc.Status().Enabled {
		t.Fatal("no BOSON_CONFIG → database mode, status disabled")
	}
}

func TestLoadReloadAndAudit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "version: 1\ndirectory:\n  name: File Dir\n  peers: [https://a.example.com]\n")
	if err := f.svc.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if f.settings.Current().Name != "File Dir" || f.reloads != 0 || len(f.audit.entries) != 0 {
		t.Fatal("start-up: settings in file mode, no hooks (modules' Start handles it), no audit")
	}
	write(t, f.path, "version: 1\ndirectory:\n  name: File Dir\n  sync_every: 1h\n")
	f.svc.Check(ctx)
	if f.settings.Current().SyncEvery != time.Hour || f.reloads != 1 {
		t.Fatal(f.settings.Current(), f.reloads)
	}
	if len(f.audit.entries) != 1 {
		t.Fatal(f.audit.entries)
	}
	e := f.audit.entries[0]
	if e.actor != Actor || e.action != "config.reloaded" ||
		strings.Join(e.meta["changed"].([]string), ",") != "directory.peers,directory.sync_every" {
		t.Fatalf("%+v", e)
	}
	// Comments and other services' sections: no hooks, no audit.
	write(t, f.path, "# note\nversion: 1\ndirectory:\n  name: File Dir\n  sync_every: 1h\nvoice: { rooms_strict: true }\n")
	f.svc.Check(ctx)
	if f.reloads != 1 || len(f.audit.entries) != 1 {
		t.Fatal(f.reloads, f.audit.entries)
	}
	if st := f.svc.Status(); !st.OK || len(st.RestartRequired) != 0 {
		t.Fatalf("everything in directory: applies live: %+v", st)
	}
}

func TestInvalidKeepsLastGood(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "directory:\n  name: Good\n")
	_ = f.svc.Load(ctx)
	write(t, f.path, "directory:\n  name: Good\n  sync_every: 1s\n")
	f.svc.Check(ctx)
	if f.settings.Current().Name != "Good" || f.settings.Current().SyncEvery != 10*time.Minute || f.reloads != 0 {
		t.Fatal(f.settings.Current())
	}
	st := f.svc.Status()
	if st.OK || st.ErrorLine != 3 || !strings.Contains(st.Error, "sync_every") {
		t.Fatalf("%+v", st)
	}
}

func TestRemovedFileSwitchesToDatabase(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "directory:\n  name: File Dir\n")
	_ = f.svc.Load(ctx)
	_ = os.Remove(f.path)
	f.svc.Check(ctx)
	if f.settings.File() != nil || f.settings.Current().Name != "env" || f.reloads != 1 {
		t.Fatal(f.settings.Current(), f.reloads)
	}
	if e := f.audit.entries[0]; e.meta["mode"] != "db" {
		t.Fatalf("%+v", e)
	}
}

func TestInvalidAtStartRunsOnEnv(t *testing.T) {
	f := newFixture(t, "directory:\n  peers: [\n")
	if err := f.svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.settings.File() != nil || f.settings.Current().Name != "env" {
		t.Fatal("invalid at start-up → env/database values")
	}
	if st := f.svc.Status(); st.OK || st.LoadedAt != nil {
		t.Fatalf("%+v", st)
	}
}
