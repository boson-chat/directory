package settings

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	repo "github.com/boson-chat/directory/internal/db/repositories/settings"
	"github.com/boson-chat/directory/internal/principal"
)

func newSvc() (*SettingsService, *repo.Memory) {
	m := &repo.Memory{}
	s := NewSettingsService(Deps{Repo: m, Defaults: Values{
		Name: "env", Operator: "env op", IncludeMain: true, MainPeerURL: "https://directory.boson.chat", SyncEvery: 10 * time.Minute,
	}})
	return s, m
}

func patch(t *testing.T, raw string) Patch {
	t.Helper()
	var p Patch
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOptDistinguishesAbsentNullAndValue(t *testing.T) {
	p := patch(t, `{"name":"x","operator":null}`)
	if !p.Name.Set || p.Name.Null || p.Name.V != "x" || !p.Operator.Set || !p.Operator.Null || p.Contact.Set {
		t.Fatalf("%+v", p)
	}
}

func TestUpdateOverridesAndResets(t *testing.T) {
	s, m := newSvc()
	ctx := context.Background()
	pr := &principal.Principal{Kind: principal.KindInternal, Username: "alice"}
	before, after, err := s.Update(ctx, pr, patch(t, `{"name":" Mine ","include_main":false,"sync_every":"1h"}`))
	if err != nil {
		t.Fatal(err)
	}
	if before.Name != "env" || after.Name != "Mine" || after.IncludeMain || after.SyncEvery != time.Hour || after.Operator != "env op" {
		t.Fatalf("before %+v after %+v", before, after)
	}
	if c := s.Current(); c != after {
		t.Fatalf("cache: %+v", c)
	}
	if m.Row.UpdatedBy == nil || *m.Row.UpdatedBy != "alice" || *m.Row.SyncEverySeconds != 3600 {
		t.Fatalf("row: %+v", m.Row)
	}
	snap, _ := s.Get(ctx)
	if len(snap.Overridden) != 3 || snap.Defaults.Name != "env" {
		t.Fatalf("snapshot: %+v", snap)
	}
	_, after, err = s.Update(ctx, pr, patch(t, `{"include_main":null,"name":null}`))
	if err != nil || !after.IncludeMain || after.Name != "env" {
		t.Fatalf("reset: %+v %v", after, err)
	}
}

func TestUpdateValidation(t *testing.T) {
	s, _ := newSvc()
	for _, raw := range []string{`{}`, `{"name":""}`, `{"sync_every":"30s"}`, `{"sync_every":"200h"}`, `{"sync_every":"x"}`} {
		if _, _, err := s.Update(context.Background(), nil, patch(t, raw)); err == nil {
			t.Fatalf("%s: want an error", raw)
		}
	}
	// An empty main_peer_url is allowed: no main directory.
	if _, after, err := s.Update(context.Background(), nil, patch(t, `{"main_peer_url":""}`)); err != nil || after.MainPeerURL != "" {
		t.Fatalf("empty main: %+v %v", after, err)
	}
}

func TestMarkSeeded(t *testing.T) {
	s, _ := newSvc()
	ctx := context.Background()
	_ = s.MarkSeeded(ctx, []string{"https://a", "https://b"})
	_ = s.MarkSeeded(ctx, []string{"https://b", "https://c"})
	got, _ := s.SeededPeers(ctx)
	if len(got) != 3 {
		t.Fatalf("seeded: %v", got)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{10 * time.Minute: "10m", 90 * time.Minute: "1h30m", time.Hour: "1h", 90 * time.Second: "1m30s"} {
		if got := FormatDuration(d); got != want {
			t.Fatalf("%v: %q, want %q", d, got, want)
		}
	}
}
