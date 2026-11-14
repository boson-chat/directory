package federation_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/boson-chat/directory/config"
	migrations "github.com/boson-chat/directory/db"
	dirhttp "github.com/boson-chat/directory/http"
	"github.com/boson-chat/directory/http/middleware"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/leader"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/federation"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// testDB creates a throwaway database from TEST_DATABASE_URL and migrates it.
func testDB(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fed_%d_%d", time.Now().UnixNano(), rand.IntN(1e6))
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		_ = admin.Close()
	})
	u, _ := url.Parse(base)
	u.Path = "/" + name
	dsn := u.String()
	if err := migrations.MigrateUp(dsn); err != nil {
		t.Fatal(err)
	}
	return dsn
}

// stubTokens accepts "op" (directory.manage) and "user" (no perms).
type stubTokens struct{}

func (stubTokens) Verify(tok string) (*middleware.AccessClaims, error) {
	c := &middleware.AccessClaims{Name: tok}
	switch tok {
	case "op":
		c.Subject, c.Perms = "usr_op", []string{principal.PermDirectoryManage}
	case "user":
		c.Subject = "usr_user"
	default:
		return nil, errors.New("bad token")
	}
	return c, nil
}

// instance is one running directory.
type instance struct {
	t      *testing.T
	URL    string
	DSN    string
	App    *dirhttp.App
	Fake   *dnsverify.Fake
	srv    *httptest.Server
	cfgFn  func(*config.Config)
	ln     net.Listener
	closed bool
}

type instOpt func(*config.Config)

func withMain(u string, include bool) instOpt {
	return func(c *config.Config) {
		c.FederationConfig.MainPeer, c.FederationConfig.IncludeMain = u, include
	}
}

func newInstance(t *testing.T, opts ...instOpt) *instance {
	t.Helper()
	in := &instance{t: t, DSN: testDB(t), Fake: dnsverify.NewFake()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	in.ln = ln
	in.URL = "http://" + ln.Addr().String()
	in.cfgFn = func(c *config.Config) {
		for _, o := range opts {
			o(c)
		}
	}
	in.start()
	t.Cleanup(in.stop)
	return in
}

func (in *instance) config() config.Config {
	cfg, err := config.LoadConfig()
	if err != nil {
		in.t.Fatal(err)
	}
	cfg.AppConfig.Env = "test"
	cfg.DBConfig.URL = in.DSN
	cfg.HTTPConfig.PublicURL = in.URL
	cfg.FederationConfig.MainPeer = ""
	cfg.FederationConfig.IncludeMain = true
	cfg.FederationConfig.Peers = ""
	cfg.MediaConfig.Dir = in.t.TempDir()
	cfg.MediaConfig.PublicURL = in.URL + "/media"
	in.cfgFn(&cfg)
	return cfg
}

// start (re)builds the app on the same database and listener address:
// a restart.
func (in *instance) start() {
	in.t.Helper()
	cfg := in.config()
	app, err := dirhttp.Build(context.Background(), cfg, dirhttp.Options{
		Verifier: in.Fake, Locker: leader.Local{}, TokenVerifier: stubTokens{}, NoUI: true,
	})
	if err != nil {
		in.t.Fatal(err)
	}
	in.App = app
	if in.ln == nil {
		ln, err := net.Listen("tcp", in.URL[len("http://"):])
		if err != nil {
			in.t.Fatal(err)
		}
		in.ln = ln
	}
	in.srv = &httptest.Server{Listener: in.ln, Config: &nethttp.Server{Handler: app.Public}}
	in.srv.Start()
	in.ln = nil
}

func (in *instance) stop() {
	if in.srv != nil {
		in.srv.Close()
		in.srv = nil
	}
	if in.App != nil {
		_ = in.App.Close()
		in.App = nil
	}
}

// restart rebuilds the app with optional config changes.
func (in *instance) restart(opts ...instOpt) {
	in.stop()
	prev := in.cfgFn
	in.cfgFn = func(c *config.Config) {
		prev(c)
		for _, o := range opts {
			o(c)
		}
	}
	in.start()
}

func (in *instance) do(method, path, token string, body any, out any) int {
	in.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := nethttp.NewRequest(method, in.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		in.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			in.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

// local registers a listing directly (the owner API is core's).
func (in *instance) local(host, status string) *network.Network {
	in.t.Helper()
	n := &network.Network{
		Hostname: host, Port: 6697, TLS: true, Name: "Net " + host, Description: "about " + host,
		Tags: []string{"chat"}, Languages: []string{"en"}, Origin: network.OriginLocal, Status: status,
		VerificationToken: "tok-" + host, TokenIssuedAt: time.Now(),
	}
	if status == network.StatusVerified {
		now := time.Now()
		n.LastVerifiedAt = &now
	}
	if err := in.App.Deps.NetworkRepo.Create(context.Background(), n); err != nil {
		in.t.Fatal(err)
	}
	return n
}

func (in *instance) setStatus(n *network.Network, status string) {
	in.t.Helper()
	n.Status = status
	if err := in.App.Deps.NetworkRepo.Update(context.Background(), n, "status"); err != nil {
		in.t.Fatal(err)
	}
}

// byHost returns the listing for host, or nil.
func (in *instance) byHost(host string) *network.Network {
	in.t.Helper()
	n, err := in.App.Deps.NetworkRepo.GetByHostname(context.Background(), host)
	if errors.Is(err, network.ErrNotFound) {
		return nil
	}
	if err != nil {
		in.t.Fatal(err)
	}
	return n
}

type peerResp struct {
	Peer struct {
		ID           string  `json:"id"`
		URL          string  `json:"url"`
		Name         string  `json:"name"`
		Enabled      bool    `json:"enabled"`
		Builtin      bool    `json:"builtin"`
		Active       bool    `json:"active"`
		LastSyncAt   *string `json:"last_sync_at"`
		LastError    *string `json:"last_error"`
		ListingCount int     `json:"listing_count"`
	} `json:"peer"`
	Result *federation.SyncResult `json:"result"`
	Error  string                 `json:"error"`
}

type peersResp struct {
	Peers []struct {
		ID           string  `json:"id"`
		URL          string  `json:"url"`
		Enabled      bool    `json:"enabled"`
		Builtin      bool    `json:"builtin"`
		LastError    *string `json:"last_error"`
		ListingCount int     `json:"listing_count"`
	} `json:"peers"`
}

func (in *instance) addPeer(u string) string {
	in.t.Helper()
	var r peerResp
	if code := in.do("POST", "/v1/admin/peers", "op", map[string]string{"url": u}, &r); code != 201 {
		in.t.Fatalf("add peer %s: %d %s", u, code, r.Error)
	}
	return r.Peer.ID
}

func (in *instance) sync(id string) *federation.SyncResult {
	in.t.Helper()
	var r peerResp
	if code := in.do("POST", "/v1/admin/peers/"+id+"/sync", "op", nil, &r); code != 200 {
		in.t.Fatalf("sync %s: %d %s", id, code, r.Error)
	}
	if r.Result.Error != "" {
		in.t.Fatalf("sync %s: %s", id, r.Result.Error)
	}
	return r.Result
}

type configT = config.Config

func itoa(n int64) string { return fmt.Sprint(n) }
