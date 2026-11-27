//go:build federation_e2e

// Two-directory federation test. Run with `make test-federation`, which
// starts compose.yml (two directories, Postgres, fake DNS + auth) and runs
// this against it. Listings and blocks are seeded straight into each
// directory's database; everything federation does goes through HTTP and
// real DNS queries against the fake resolver.
package federation_e2e

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	urlA    = "http://127.0.0.1:" + env("E2E_A_PORT", "47392")
	urlB    = "http://127.0.0.1:" + env("E2E_B_PORT", "47393")
	urlFake = "http://127.0.0.1:" + env("E2E_FAKES_PORT", "47391")
	pgBase  = "postgres://directory:e2e@127.0.0.1:" + env("E2E_PG_PORT", "47390") + "/"
	compose = []string{"compose", "-f", "compose.yml"}
)

type world struct {
	t     *testing.T
	dbA   *sql.DB
	dbB   *sql.DB
	token string
}

func call(t *testing.T, method, u, token, body string, out any) int {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, u, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(raw)
		} else if len(raw) > 0 {
			_ = json.Unmarshal(raw, out)
		}
	}
	return resp.StatusCode
}

// send is an owner write with a Network key; body is sent as contentType.
func send(t *testing.T, method, u, key, contentType, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Network "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		t.Logf("%s %s: %d %s", method, u, resp.StatusCode, raw)
	}
	return resp.StatusCode
}

// keyJSON is a JSON request with a Network key; out gets the body.
func keyJSON(t *testing.T, method, u, key, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Network "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	return resp.StatusCode
}

func eventually(t *testing.T, what string, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func waitUp(t *testing.T, u string) {
	eventually(t, u+" up", 3*time.Minute, func() bool { return call(t, "GET", u+"/readyz", "", "", nil) == 200 })
}

func openDB(t *testing.T, name string) *sql.DB {
	db, err := sql.Open("pgx", pgBase+name+"?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func (w *world) exec(db *sql.DB, q string, args ...any) {
	w.t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

// seed lists a verified local network on db.
func (w *world) seed(db *sql.DB, id, host string) {
	w.exec(db, `INSERT INTO networks (id, hostname, port, name, description, tags, languages, verification_token, status, last_verified_at)
		VALUES ($1, $2, 6697, $3, 'e2e', '{chat}', '{en}', $4, 'verified', now())`, id, host, "Net "+host, "tok-"+id+"-e2e")
}

func (w *world) publishTXT(host, token string) {
	if code := call(w.t, "PUT", urlFake+"/txt/"+host, "", token, nil); code != 204 {
		w.t.Fatalf("fake dns: %d", code)
	}
}

// status of host on B ("" = not listed) and its origin.
func (w *world) onB(host string) (status, origin string) {
	err := w.dbB.QueryRow("SELECT status, origin FROM networks WHERE hostname = $1", host).Scan(&status, &origin)
	if err == sql.ErrNoRows {
		return "", ""
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return status, origin
}

type peer struct {
	ID           string  `json:"id"`
	URL          string  `json:"url"`
	Enabled      bool    `json:"enabled"`
	Builtin      bool    `json:"builtin"`
	LastError    *string `json:"last_error"`
	ListingCount int     `json:"listing_count"`
}

func (w *world) peers(u string) []peer {
	w.t.Helper()
	var list []peer
	// Right after a (re)start the directory may not have loaded the
	// issuer's JWKS yet and answers 401 for a moment.
	eventually(w.t, "list peers on "+u, 30*time.Second, func() bool {
		var r struct {
			Peers []peer `json:"peers"`
		}
		code := call(w.t, "GET", u+"/v1/admin/peers", w.token, "", &r)
		list = r.Peers
		if code == 401 {
			w.refreshToken()
		}
		return code == 200
	})
	return list
}

func (w *world) refreshToken() {
	var tok string
	if code := call(w.t, "GET", urlFake+"/token?perms=directory.manage", "", "", &tok); code == 200 && tok != "" {
		w.token = tok
	}
}

// syncB nudges B's sync of peer id (409 = the worker is already on it).
func (w *world) syncB(id string) {
	code := call(w.t, "POST", urlB+"/v1/admin/peers/"+id+"/sync", w.token, "", nil)
	if code != 200 && code != 409 {
		w.t.Fatalf("sync: %d", code)
	}
}

func dockerCompose(t *testing.T, extraEnv []string, args ...string) {
	t.Helper()
	cmd := exec.Command("docker", append(compose, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose %v: %v\n%s", args, err, out)
	}
}

func TestTwoDirectories(t *testing.T) {
	waitUp(t, urlA)
	waitUp(t, urlB)
	w := &world{t: t, dbA: openDB(t, "dir_a"), dbB: openDB(t, "dir_b")}
	if code := call(t, "GET", urlFake+"/token?perms=directory.manage", "", "", &w.token); code != 200 || w.token == "" {
		t.Fatalf("token: %d", code)
	}
	t.Log("A is the main directory: it never peers with itself")
	if ps := w.peers(urlA); len(ps) != 0 {
		t.Fatalf("A peers: %+v", ps)
	}

	t.Log("B ships with A as its built-in, enabled main peer")
	ps := w.peers(urlB)
	if len(ps) != 1 || !ps[0].Builtin || !ps[0].Enabled || ps[0].URL != "http://dir-a:8080" {
		t.Fatalf("B peers: %+v", ps)
	}
	mainID := ps[0].ID

	t.Log("seed: a block on B, listings on A, a local listing on B")
	w.exec(w.dbB, `INSERT INTO blocks (id, pattern, reason) VALUES ('blk_01E2E000000000000000000001', '*.blocked.test', 'e2e')`)
	w.seed(w.dbA, "net_01E2E00000000000000000000A", "irc.alpha.test")
	w.seed(w.dbA, "net_01E2E00000000000000000000B", "irc.beta.test")
	w.seed(w.dbA, "net_01E2E00000000000000000000C", "irc.blocked.test")
	w.seed(w.dbA, "net_01E2E00000000000000000000D", "irc.conflict.test")
	w.exec(w.dbB, `INSERT INTO networks (id, hostname, port, name, verification_token, status)
		VALUES ('net_01E2E0000000000000000000B1', 'irc.conflict.test', 6697, 'B local', 'tok-b-local-xx', 'pending')`)
	// DNS publishes alpha's token only (beta comes later).
	w.publishTXT("irc.alpha.test", "tok-net_01E2E00000000000000000000A-e2e")
	w.publishTXT("irc.conflict.test", "tok-net_01E2E00000000000000000000D-e2e")

	t.Log("import + local re-verification over real DNS queries")
	eventually(t, "alpha verified on B", time.Minute, func() bool {
		w.syncB(mainID)
		s, o := w.onB("irc.alpha.test")
		return s == "verified" && o == "peer:"+mainID
	})
	if s, _ := w.onB("irc.beta.test"); s != "pending" {
		t.Fatalf("beta must stay pending without its TXT record on B's DNS: %q", s)
	}
	if s, _ := w.onB("irc.blocked.test"); s != "" {
		t.Fatalf("blocked hostname imported: %q", s)
	}
	if s, o := w.onB("irc.conflict.test"); o != "local" || s != "pending" {
		t.Fatalf("local listing must beat the peer's: %q %q", s, o)
	}

	t.Log("beta's TXT appears: B's verify worker confirms it")
	w.publishTXT("irc.beta.test", "tok-net_01E2E00000000000000000000B-e2e")
	eventually(t, "beta verified on B", time.Minute, func() bool {
		s, _ := w.onB("irc.beta.test")
		return s == "verified"
	})

	t.Log("theme: alpha's owner (a Network key, like a stack) sets a theme and a logo on A; B imports both")
	const key = "e2e-stack-network-key-0123456789abcdef"
	w.exec(w.dbA, "UPDATE networks SET management_key_hash = encode(sha256(convert_to($1, 'UTF8')), 'hex') WHERE hostname = 'irc.alpha.test'", key)
	alphaA := "/v1/networks/net_01E2E00000000000000000000A"
	if code := send(t, "PUT", urlA+alphaA+"/theme", key, "application/json",
		`{"version":1,"base":"dark","colors":{"accent":"#7C3AED","bg":"#0b0b10"},"panes":{"messages":{"dim":0.6,"blur":4}},"login_background":"https://cdn.example/bg.webp"}`); code != 200 {
		t.Fatalf("PUT theme on A: %d", code)
	}
	if code := send(t, "PUT", urlA+alphaA+"/theme-images/logo", key, "image/svg+xml",
		`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8" fill="#7c3aed"/></svg>`); code != 200 {
		t.Fatalf("PUT logo on A: %d", code)
	}
	var onB struct {
		Network struct {
			Theme struct {
				Colors          map[string]string `json:"colors"`
				Logo            string            `json:"logo"`
				LoginBackground string            `json:"login_background"`
				Panes           struct {
					Messages struct {
						Dim float64 `json:"dim"`
					} `json:"messages"`
				} `json:"panes"`
			} `json:"theme"`
		} `json:"network"`
	}
	eventually(t, "alpha's theme on B", time.Minute, func() bool {
		w.syncB(mainID)
		call(t, "GET", urlB+"/v1/networks/lookup?hostname=irc.alpha.test", "", "", &onB)
		return strings.HasPrefix(onB.Network.Theme.Logo, "http://dir-a:8080/media/network-themes/net_01E2E00000000000000000000A-logo-")
	})
	if th := onB.Network.Theme; th.Colors["accent"] != "#7c3aed" || th.LoginBackground != "https://cdn.example/bg.webp" || th.Panes.Messages.Dim != 0.6 {
		t.Fatalf("theme on B: %+v", th)
	}
	// B hot-links A's copy: A serves it as an SVG image, sandboxed.
	logo, err := http.Get(strings.Replace(onB.Network.Theme.Logo, "http://dir-a:8080", urlA, 1))
	if err != nil {
		t.Fatal(err)
	}
	logo.Body.Close()
	if logo.StatusCode != 200 || logo.Header.Get("Content-Type") != "image/svg+xml" || logo.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(logo.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("logo on A: %d %v", logo.StatusCode, logo.Header)
	}
	t.Log("theme removed on A: gone on B too")
	if code := send(t, "DELETE", urlA+alphaA+"/theme", key, "", ""); code != 200 {
		t.Fatalf("DELETE theme on A: %d", code)
	}
	eventually(t, "alpha's theme gone from B", time.Minute, func() bool {
		w.syncB(mainID)
		var th sql.NullString
		if err := w.dbB.QueryRow("SELECT theme::text FROM networks WHERE hostname = 'irc.alpha.test'").Scan(&th); err != nil {
			t.Fatal(err)
		}
		return !th.Valid
	})

	t.Log("move: gamma's owner moves it to a new hostname on A; B follows it as an update of the same listing")
	w.seed(w.dbA, "net_01E2E00000000000000000000E", "irc.gamma.test")
	w.publishTXT("irc.gamma.test", "tok-net_01E2E00000000000000000000E-e2e")
	eventually(t, "gamma verified on B", time.Minute, func() bool {
		w.syncB(mainID)
		s, _ := w.onB("irc.gamma.test")
		return s == "verified"
	})
	var gammaB string
	if err := w.dbB.QueryRow("SELECT id FROM networks WHERE hostname = 'irc.gamma.test'").Scan(&gammaB); err != nil {
		t.Fatal(err)
	}
	const gammaKey = "e2e-stack-network-key-gamma-0123456789"
	w.exec(w.dbA, "UPDATE networks SET management_key_hash = encode(sha256(convert_to($1, 'UTF8')), 'hex') WHERE hostname = 'irc.gamma.test'", gammaKey)
	var mv struct {
		Move struct {
			Token       string `json:"token"`
			RecordName  string `json:"record_name"`
			RecordValue string `json:"record_value"`
		} `json:"move"`
	}
	gammaA := urlA + "/v1/networks/net_01E2E00000000000000000000E"
	if code := keyJSON(t, "POST", gammaA+"/move", gammaKey, `{"hostname":"irc.boson.gamma.test"}`, &mv); code != 201 ||
		mv.Move.RecordName != "_boson.irc.boson.gamma.test" || mv.Move.RecordValue != "boson-verify="+mv.Move.Token {
		t.Fatalf("request move: %d %+v", code, mv)
	}
	// No record yet: the check fails and the listing stays where it is.
	if code := keyJSON(t, "POST", gammaA+"/move/verify", gammaKey, "", nil); code != 409 {
		t.Fatalf("check without the record: %d", code)
	}
	if s, _ := w.onB("irc.gamma.test"); s != "verified" {
		t.Fatalf("gamma on B during the pending move: %q", s)
	}
	// The record appears on real DNS: A's verify worker completes the move.
	w.publishTXT("irc.boson.gamma.test", mv.Move.Token)
	eventually(t, "gamma moved on A", time.Minute, func() bool {
		var h string
		_ = w.dbA.QueryRow("SELECT hostname FROM networks WHERE id = 'net_01E2E00000000000000000000E'").Scan(&h)
		return h == "irc.boson.gamma.test"
	})
	eventually(t, "gamma moved on B (same row, re-verified)", time.Minute, func() bool {
		w.syncB(mainID)
		var id, status string
		_ = w.dbB.QueryRow("SELECT id, status FROM networks WHERE hostname = 'irc.boson.gamma.test'").Scan(&id, &status)
		return id == gammaB && status == "verified"
	})
	if s, _ := w.onB("irc.gamma.test"); s != "" {
		t.Fatalf("old hostname still on B: %q", s)
	}
	var lk struct {
		Network struct {
			ID string `json:"id"`
		} `json:"network"`
		MovedFrom string `json:"moved_from"`
	}
	if code := call(t, "GET", urlB+"/v1/networks/lookup?hostname=irc.gamma.test", "", "", &lk); code != 200 || lk.Network.ID != gammaB || lk.MovedFrom != "irc.gamma.test" {
		t.Fatalf("old hostname lookup on B: %d %+v", code, lk)
	}

	t.Log("B never re-exports imported listings")
	var feed struct {
		Changes []struct {
			Network struct {
				ID string `json:"id"`
			} `json:"network"`
			Deleted bool `json:"deleted"`
		} `json:"changes"`
	}
	call(t, "GET", urlB+"/v1/feed", "", "", &feed)
	// Only B's own (pending, so a bare deletion) listing may appear.
	for _, c := range feed.Changes {
		if c.Network.ID != "net_01E2E0000000000000000000B1" || !c.Deleted {
			t.Fatalf("B feed exports %+v", c)
		}
	}

	t.Log("deletes propagate")
	w.exec(w.dbA, "DELETE FROM networks WHERE hostname = 'irc.alpha.test'")
	eventually(t, "alpha gone from B", time.Minute, func() bool {
		w.syncB(mainID)
		s, _ := w.onB("irc.alpha.test")
		return s == ""
	})

	t.Log("operator disables the main peer: its listings go, and it stays off after a restart")
	var pr struct {
		Peer peer `json:"peer"`
	}
	if code := call(t, "PATCH", urlB+"/v1/admin/peers/"+mainID, w.token, `{"enabled":false}`, &pr); code != 200 || pr.Peer.Enabled {
		t.Fatalf("disable: %d %+v", code, pr.Peer)
	}
	if s, _ := w.onB("irc.beta.test"); s != "" {
		t.Fatalf("disabled main peer listings remain: %q", s)
	}
	dockerCompose(t, nil, "restart", "dir-b")
	waitUp(t, urlB)
	time.Sleep(12 * time.Second) // more than a sync interval
	ps = w.peers(urlB)
	if len(ps) != 1 || ps[0].ID != mainID || ps[0].Enabled {
		t.Fatalf("after restart: %+v", ps)
	}
	if s, _ := w.onB("irc.beta.test"); s != "" {
		t.Fatalf("disabled main peer synced after restart: %q", s)
	}

	t.Log("re-enable, then DIRECTORY_INCLUDE_MAIN=false removes it entirely")
	call(t, "PATCH", urlB+"/v1/admin/peers/"+mainID, w.token, `{"enabled":true}`, &pr)
	eventually(t, "beta back on B", time.Minute, func() bool {
		w.syncB(mainID)
		s, _ := w.onB("irc.beta.test")
		return s != ""
	})
	dockerCompose(t, []string{"B_INCLUDE_MAIN=false"}, "up", "-d", "dir-b")
	waitUp(t, urlB)
	eventually(t, "no builtin peer with DIRECTORY_INCLUDE_MAIN=false", time.Minute, func() bool {
		return len(w.peers(urlB)) == 0
	})
	if s, _ := w.onB("irc.beta.test"); s != "" {
		t.Fatalf("main listings remain with DIRECTORY_INCLUDE_MAIN=false: %q", s)
	}
	dockerCompose(t, nil, "up", "-d", "dir-b")
	waitUp(t, urlB)
	fmt.Println("federation e2e: all scenarios passed")
}

var (
	urlC         = "http://127.0.0.1:" + env("E2E_C_PORT", "47394")
	urlCInternal = "http://127.0.0.1:" + env("E2E_C_INTERNAL_PORT", "47395")
	configC      = "config-c/boson.yaml"
)

// writeConfigC replaces dir-c's config file like the admin panel does
// (tmp file in the same directory, then rename).
func writeConfigC(t *testing.T, content string) {
	t.Helper()
	tmp := configC + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, configC); err != nil {
		t.Fatal(err)
	}
}

type configStatus struct {
	OK              bool     `json:"ok"`
	Present         bool     `json:"present"`
	Error           string   `json:"error"`
	ErrorLine       int      `json:"error_line"`
	RestartRequired []string `json:"restart_required"`
	LoadedAt        string   `json:"loaded_at"`
}

func statusC(t *testing.T) configStatus {
	var st configStatus
	if code := call(t, "GET", urlCInternal+"/internal/config/status", "", "", &st); code != 200 {
		t.Fatalf("config status: %d", code)
	}
	return st
}

func wellKnownName(t *testing.T, u string) string {
	var wk struct{ Name string }
	call(t, "GET", u+"/.well-known/boson-directory.json", "", "", &wk)
	return wk.Name
}

// dir-c runs with BOSON_CONFIG: settings and peers come from the file and
// follow edits to it without a restart (the directory is a read-only bind
// mount, so this also covers the mtime poll when fsnotify events don't
// cross the mount).
func TestConfigFileDirectory(t *testing.T) {
	waitUp(t, urlA)
	waitUp(t, urlC)
	w := &world{t: t, dbA: openDB(t, "dir_a")}
	dbC := openDB(t, "dir_c")
	w.refreshToken()
	onC := func(host string) string {
		var status string
		if err := dbC.QueryRow("SELECT status FROM networks WHERE hostname = $1", host).Scan(&status); err == sql.ErrNoRows {
			return ""
		} else if err != nil {
			t.Fatal(err)
		}
		return status
	}
	type cpeer struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
		Managed bool   `json:"managed_by_config_file"`
	}
	peersC := func() []cpeer {
		var r struct {
			Peers []cpeer `json:"peers"`
		}
		eventually(t, "list peers on C", 30*time.Second, func() bool {
			code := call(t, "GET", urlC+"/v1/admin/peers", w.token, "", &r)
			if code == 401 {
				w.refreshToken()
			}
			return code == 200
		})
		return r.Peers
	}

	t.Log("settings come from the file and are read-only")
	if st := statusC(t); !st.OK || !st.Present || len(st.RestartRequired) != 0 {
		t.Fatalf("status: %+v", st)
	}
	if n := wellKnownName(t, urlC); n != "directory C (file)" {
		t.Fatalf(".well-known name: %q", n)
	}
	var settings struct {
		Source   string `json:"source"`
		Settings struct {
			Operator    string `json:"operator"`
			IncludeMain bool   `json:"include_main"`
		} `json:"settings"`
	}
	call(t, "GET", urlC+"/v1/admin/settings", w.token, "", &settings)
	if settings.Source != "file" || settings.Settings.Operator != "e2e operator" || settings.Settings.IncludeMain {
		t.Fatalf("settings: %+v", settings)
	}
	var e struct{ Error string }
	if code := call(t, "PATCH", urlC+"/v1/admin/settings", w.token, `{"name":"x"}`, &e); code != 409 || e.Error != "managed_by_config_file" {
		t.Fatalf("PATCH settings: %d %s", code, e.Error)
	}

	t.Log("the file's peer (A) exists, is enabled, is owned by the file and syncs")
	ps := peersC()
	if len(ps) != 1 || ps[0].URL != "http://dir-a:8080" || !ps[0].Enabled || !ps[0].Managed {
		t.Fatalf("C peers: %+v", ps)
	}
	aID := ps[0].ID
	if code := call(t, "PATCH", urlC+"/v1/admin/peers/"+aID, w.token, `{"enabled":false}`, &e); code != 409 || e.Error != "managed_by_config_file" {
		t.Fatalf("PATCH file peer: %d %s", code, e.Error)
	}
	if code := call(t, "DELETE", urlC+"/v1/admin/peers/"+aID, w.token, "", nil); code != 409 {
		t.Fatalf("DELETE file peer: %d", code)
	}
	w.seed(w.dbA, "net_01E2E00000000000000000000G", "irc.gamma.test")
	w.publishTXT("irc.gamma.test", "tok-net_01E2E00000000000000000000G-e2e")
	eventually(t, "gamma verified on C", time.Minute, func() bool {
		call(t, "POST", urlC+"/v1/admin/peers/"+aID+"/sync", w.token, "", nil)
		return onC("irc.gamma.test") == "verified"
	})

	t.Log("edit: rename, drop the peer → applied live; the peer is disabled, not deleted")
	writeConfigC(t, `version: 1
directory:
  name: directory C (edited)
  include_main: false
  main_peer_url: ""
  peers: []
`)
	eventually(t, "rename applied", 30*time.Second, func() bool { return wellKnownName(t, urlC) == "directory C (edited)" })
	ps = peersC()
	if len(ps) != 1 || ps[0].ID != aID || ps[0].Enabled || ps[0].Managed {
		t.Fatalf("dropped peer: %+v", ps)
	}
	if s := onC("irc.gamma.test"); s != "" {
		t.Fatalf("disabled peer's listings must go: %q", s)
	}

	t.Log("a broken edit keeps the last good config and is reported with its line")
	writeConfigC(t, "version: 1\ndirectory:\n  name: directory C (broken)\n  sync_every: 1s\n")
	eventually(t, "error status", 30*time.Second, func() bool { return !statusC(t).OK })
	if st := statusC(t); st.ErrorLine != 4 || !strings.Contains(st.Error, "sync_every") {
		t.Fatalf("status: %+v", st)
	}
	if n := wellKnownName(t, urlC); n != "directory C (edited)" {
		t.Fatalf("last good config must stay: %q", n)
	}

	t.Log("fixed, peer listed again: re-enabled and managed; survives a restart")
	writeConfigC(t, `version: 1
directory:
  name: directory C (file)
  include_main: false
  main_peer_url: ""
  peers: [http://dir-a:8080]
`)
	eventually(t, "status ok", 30*time.Second, func() bool { return statusC(t).OK })
	eventually(t, "peer re-enabled", 30*time.Second, func() bool {
		ps := peersC()
		return len(ps) == 1 && ps[0].ID == aID && ps[0].Enabled && ps[0].Managed
	})
	dockerCompose(t, nil, "restart", "dir-c")
	waitUp(t, urlC)
	if n := wellKnownName(t, urlC); n != "directory C (file)" {
		t.Fatalf("after restart: %q", n)
	}
	if ps := peersC(); len(ps) != 1 || !ps[0].Enabled || !ps[0].Managed {
		t.Fatalf("after restart: %+v", ps)
	}
}
