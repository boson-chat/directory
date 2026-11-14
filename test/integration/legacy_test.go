package integration

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/boson-chat/directory/internal/services/importer"
)

// Key sets of the backend's server JSON (services/server/model.go):
// always-present keys, and the omitempty ones.
var (
	legacyPublicKeys = []string{"id", "hostname", "port", "tls", "name", "tags", "languages", "is_nsfw",
		"is_featured", "verification_status", "health_status", "registered_at"}
	legacyOptional = []string{"description", "verification_last_checked_at", "health_last_checked_at",
		"user_count", "user_count_updated_at", "icon_url", "banner_url"}
	legacyOwnerPendingKeys = append(slices.Clone(legacyPublicKeys), "verification_token", "verification_token_issued_at")
)

func keys(m map[string]any) []string {
	k := slices.Collect(maps.Keys(m))
	slices.Sort(k)
	return k
}

// sameKeys: every required key present, nothing outside required+optional.
func sameKeys(t *testing.T, what string, got map[string]any, required []string, optional ...string) {
	t.Helper()
	for _, k := range required {
		if _, ok := got[k]; !ok {
			t.Fatalf("%s lacks %q: %v", what, k, keys(got))
		}
	}
	for k := range got {
		if !slices.Contains(required, k) && !slices.Contains(optional, k) {
			t.Fatalf("%s has unexpected key %q", what, k)
		}
	}
}

func TestLegacyServersShapes(t *testing.T) {
	e := newEnv(t)
	alice := bearer(e.token("usr_alice"))
	// POST /servers: 201 owner view with the token (pending).
	r := e.expect(e.do("POST", "/servers", alice, map[string]any{
		"hostname": "irc.legacy.example", "port": 6697, "name": "Legacy", "description": "old client", "tags": []string{"foss"},
	}), 201).m(t)
	sameKeys(t, "POST /servers", r, legacyOwnerPendingKeys, legacyOptional...)
	id := r["id"].(string)
	if r["tls"] != true || r["verification_status"] != "pending" {
		t.Fatalf("created %v", r)
	}
	// Old error shapes.
	if e.expect(e.do("POST", "/servers", alice, map[string]any{"hostname": "x", "port": 0, "name": "n"}), 400).m(t)["error"] != "hostname, name, and 1<=port<=65535 are required" {
		t.Fatal("bad 400 message")
	}
	if e.expect(e.do("GET", "/servers/"+id, "", nil), 404).m(t)["error"] != "not found" {
		t.Fatal("pending row leaked on GET /servers/{id}")
	}
	e.expect(e.do("GET", "/servers/not-a-uuid", "", nil), 400)
	// Verify: 409 with the matrix, then 200; shape {server, report, success}.
	e.dns.Set("irc.legacy.example", 1)
	v := e.expect(e.do("POST", "/servers/"+id+"/verify", alice, nil), 409).m(t)
	sameKeys(t, "verify", v, []string{"server", "report", "success"})
	rep := v["report"].(map[string]any)
	sameKeys(t, "report", rep, []string{"success", "results"})
	if len(rep["results"].(map[string]any)) != 3 {
		t.Fatal("report results")
	}
	e.clock.Advance(time.Minute)
	e.dns.Set("irc.legacy.example", 3)
	v = e.expect(e.do("POST", "/servers/"+id+"/verify", alice, nil), 200).m(t)
	sameKeys(t, "verified server", v["server"].(map[string]any), append(slices.Clone(legacyPublicKeys), "description", "verification_last_checked_at"), legacyOptional...)
	// Other users: 403 "not the owner".
	if e.expect(e.do("PATCH", "/servers/"+id, bearer(e.token("usr_bob")), map[string]any{"name": "x"}), 403).m(t)["error"] != "not the owner" {
		t.Fatal("want not the owner")
	}
	p := e.expect(e.do("PATCH", "/servers/"+id, alice, map[string]any{"name": "Legacy 2", "is_nsfw": false}), 200).m(t)
	if p["name"] != "Legacy 2" {
		t.Fatalf("patch %v", p)
	}
	e.expect(e.do("PATCH", "/servers/"+id, alice, map[string]any{"name": " "}), 400)
	// GET /servers: {servers, count}, offset paging.
	var list struct {
		Servers []map[string]any `json:"servers"`
		Count   int              `json:"count"`
	}
	e.expect(e.do("GET", "/servers?limit=10&offset=0&sort=newest", "", nil), 200).json(t, &list)
	if list.Count != 1 || len(list.Servers) != 1 {
		t.Fatalf("list %+v", list)
	}
	sameKeys(t, "GET /servers item", list.Servers[0], legacyPublicKeys, legacyOptional...)
	e.expect(e.do("GET", "/servers?offset=1", "", nil), 200).json(t, &list)
	if list.Count != 0 {
		t.Fatal("offset ignored")
	}
	// GET /servers/me.
	e.expect(e.do("GET", "/servers/me", alice, nil), 200).json(t, &list)
	if list.Count != 1 {
		t.Fatalf("me %+v", list)
	}
	// Regenerate → pending + token; icon upload via POST.
	rg := e.expect(e.do("POST", "/servers/"+id+"/regenerate-token", alice, nil), 200).m(t)
	sameKeys(t, "regenerate", rg, legacyOwnerPendingKeys, legacyOptional...)
	ic := e.expect(e.do("POST", "/servers/"+id+"/icon", alice, pngBytes(t, 8, 8)), 200).m(t)
	if !strings.HasPrefix(ic["icon_url"].(string), "https://media.test/") {
		t.Fatalf("icon %v", ic)
	}
}

const oldSchema = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE users (id uuid PRIMARY KEY, handle text UNIQUE NOT NULL);
CREATE TABLE servers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hostname text NOT NULL, port integer NOT NULL, tls boolean NOT NULL DEFAULT true,
    name text NOT NULL, description text,
    tags text[] NOT NULL DEFAULT '{}', languages text[] NOT NULL DEFAULT '{}',
    is_nsfw boolean NOT NULL DEFAULT false, is_featured boolean NOT NULL DEFAULT false,
    verification_status text NOT NULL DEFAULT 'pending', verification_token text,
    verification_last_checked_at timestamptz,
    health_status text NOT NULL DEFAULT 'unknown', health_last_checked_at timestamptz,
    user_count integer, user_count_updated_at timestamptz,
    registered_by uuid REFERENCES users(id) ON DELETE SET NULL,
    registered_at timestamptz NOT NULL DEFAULT now(),
    verification_token_issued_at timestamptz,
    icon_storage_key text, banner_storage_key text
);
CREATE TABLE auth_identities (auth_user_id text PRIMARY KEY, user_id uuid NOT NULL UNIQUE,
    source text NOT NULL DEFAULT 'auto', created_at timestamptz NOT NULL DEFAULT now());
INSERT INTO users VALUES ('11111111-1111-1111-1111-111111111111', 'alice'), ('22222222-2222-2222-2222-222222222222', 'ghost');
INSERT INTO auth_identities (auth_user_id, user_id) VALUES ('usr_alice', '11111111-1111-1111-1111-111111111111');
INSERT INTO servers (id, hostname, port, name, description, tags, verification_status, verification_token, verification_last_checked_at, registered_by, registered_at, user_count, icon_storage_key) VALUES
 ('aaaaaaaa-0000-0000-0000-000000000001', 'irc.libera.chat', 6697, 'Libera', 'FOSS', '{foss}', 'verified', 'tok1', now() - interval '1 day', '11111111-1111-1111-1111-111111111111', now() - interval '30 days', 42, 'server-icons/a-1.png'),
 -- duplicate hostname (case + trailing dot), pending and older: loses to the verified row
 ('aaaaaaaa-0000-0000-0000-000000000002', 'IRC.Libera.Chat.', 6697, 'Libera dup', NULL, '{}', 'pending', 'tok2', NULL, '22222222-2222-2222-2222-222222222222', now() - interval '60 days', NULL, NULL),
 ('aaaaaaaa-0000-0000-0000-000000000003', '192.0.2.1', 6667, 'By IP', NULL, '{}', 'verified', 'tok3', NULL, NULL, now(), NULL, NULL),
 ('aaaaaaaa-0000-0000-0000-000000000004', 'irc.ghost.example', 6697, 'Orphan', NULL, '{}', 'lapsed', 'tok4', now() - interval '20 days', '22222222-2222-2222-2222-222222222222', now() - interval '90 days', NULL, NULL),
 ('aaaaaaaa-0000-0000-0000-000000000005', 'irc.pending.example', 6697, 'Pending', NULL, '{}', 'pending', 'tok5', NULL, '11111111-1111-1111-1111-111111111111', now(), NULL, NULL);
`

func TestImportLegacy(t *testing.T) {
	e := newEnv(t)
	srcDSN := newDatabase(t, "dirlegacy")
	src, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if _, err := src.Exec(oldSchema); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo, m := e.app.Deps.NetworkRepo, e.app.Deps.Media

	rep, err := importer.Import(ctx, src, repo, m, importer.Options{DryRun: true, CDNURL: "https://cdn.old.example"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Read != 5 || rep.Imported != 3 || rep.Duplicates != 1 || rep.Invalid != 1 || rep.NoOwner != 1 {
		t.Fatalf("dry run %s", rep)
	}
	var n int
	_ = e.sqlDB.QueryRow("SELECT count(*) FROM networks").Scan(&n)
	if n != 0 {
		t.Fatal("dry run wrote rows")
	}

	rep, err = importer.Import(ctx, src, repo, m, importer.Options{CDNURL: "https://cdn.old.example", Now: e.clock.Now})
	if err != nil || rep.Imported != 3 {
		t.Fatalf("import %s %v", rep, err)
	}
	libera, err := repo.GetByHostname(ctx, "irc.libera.chat")
	if err != nil {
		t.Fatal(err)
	}
	if libera.Status != "verified" || libera.OwnerAuthUserID == nil || *libera.OwnerAuthUserID != "usr_alice" ||
		libera.LegacyID == nil || libera.LegacyID.String() != "aaaaaaaa-0000-0000-0000-000000000001" ||
		libera.IconKey == nil || *libera.IconKey != "https://cdn.old.example/server-icons/a-1.png" ||
		libera.VerificationToken != "tok1" || libera.LastVerifiedAt == nil || *libera.UserCount != 42 {
		t.Fatalf("libera %+v", libera)
	}
	ghost, _ := repo.GetByHostname(ctx, "irc.ghost.example")
	if ghost.Status != "lapsed" || ghost.OwnerAuthUserID != nil {
		t.Fatalf("ghost %+v", ghost)
	}
	// Re-run is a no-op.
	rep, err = importer.Import(ctx, src, repo, m, importer.Options{CDNURL: "https://cdn.old.example"})
	if err != nil || rep.Imported != 0 || rep.AlreadyThere != 3 {
		t.Fatalf("re-run %s %v", rep, err)
	}
	// Old links and the owner keep working.
	s := e.expect(e.do("GET", "/servers/aaaaaaaa-0000-0000-0000-000000000001", "", nil), 200).m(t)
	if s["id"] != "aaaaaaaa-0000-0000-0000-000000000001" || s["icon_url"] != "https://cdn.old.example/server-icons/a-1.png" {
		t.Fatalf("legacy get %v", s)
	}
	e.expect(e.do("GET", "/servers/aaaaaaaa-0000-0000-0000-000000000005", "", nil), 404)
	// /v1 knows the legacy id too: the row carries it, and GET by it works
	// (clients saved servers under it before the move to /v1).
	var listing struct {
		Network map[string]any `json:"network"`
	}
	e.expect(e.do("GET", "/v1/networks/"+libera.ID, "", nil), 200).json(t, &listing)
	if listing.Network["legacy_id"] != "aaaaaaaa-0000-0000-0000-000000000001" {
		t.Fatalf("legacy_id %v", listing.Network["legacy_id"])
	}
	e.expect(e.do("GET", "/v1/networks/aaaaaaaa-0000-0000-0000-000000000001", "", nil), 200).json(t, &listing)
	if listing.Network["id"] != libera.ID || listing.Network["hostname"] != "irc.libera.chat" {
		t.Fatalf("v1 get by legacy id %v", listing.Network)
	}
	var page struct{ Networks []map[string]any }
	e.expect(e.do("GET", "/v1/networks?q=libera", "", nil), 200).json(t, &page)
	if len(page.Networks) != 1 || page.Networks[0]["legacy_id"] != "aaaaaaaa-0000-0000-0000-000000000001" {
		t.Fatalf("list legacy_id %v", page.Networks)
	}
	// An unknown uuid is a 404, and native rows have no legacy_id.
	e.expect(e.do("GET", "/v1/networks/aaaaaaaa-0000-0000-0000-0000000000ff", "", nil), 404)
	native := e.registerVerified(bearer(e.token("usr_carol")), "irc.native.example", nil)
	var nativeListing struct {
		Network map[string]any `json:"network"`
	}
	e.expect(e.do("GET", "/v1/networks/"+native, "", nil), 200).json(t, &nativeListing)
	if _, ok := nativeListing.Network["legacy_id"]; ok {
		t.Fatalf("native row has legacy_id %v", nativeListing.Network)
	}
	var mine struct{ Listings []any }
	e.expect(e.do("GET", "/v1/me/networks", bearer(e.token("usr_alice")), nil), 200).json(t, &mine)
	if len(mine.Listings) != 2 {
		t.Fatalf("alice owns %d", len(mine.Listings))
	}
	// The imported verified listing survives its first daily re-check miss.
	e.dns.Set("irc.libera.chat", 0)
	e.clock.Advance(25 * time.Hour)
	e.runWorker("verify")
	if got, _ := repo.GetByHostname(ctx, "irc.libera.chat"); got.Status != "verified" {
		t.Fatalf("imported listing lapsed on first miss: %s", got.Status)
	}
}
