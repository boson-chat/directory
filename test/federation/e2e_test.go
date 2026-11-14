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
