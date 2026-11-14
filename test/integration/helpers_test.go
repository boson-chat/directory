// Package integration runs the HTTP API against a real Postgres.
//
// Set TEST_DATABASE_URL to a database the test user can CREATE DATABASE
// from (each test gets its own throwaway database). Without it these tests
// are skipped. Locally: `make test-pg-up test-integration`.
package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/db"
	dirhttp "github.com/boson-chat/directory/http"
	"github.com/boson-chat/directory/http/middleware"
	"github.com/boson-chat/directory/internal/leader"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	issuer        = "https://auth.integration.test"
	internalToken = "internal-test-token-0123456789"
)

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	t      *testing.T
	app    *dirhttp.App
	srv    *httptest.Server
	dns    *dnsverify.Fake
	clock  *clock
	key    ed25519.PrivateKey
	media  *media.MemStorage
	dsn    string
	sqlDB  *sql.DB
	client *nethttp.Client
}

// newDatabase creates a throwaway database and returns its DSN.
func newDatabase(t *testing.T, prefix string) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := prefix + "_" + hex.EncodeToString(b)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a, err := sql.Open("pgx", base)
		if err == nil {
			_, _ = a.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
			_ = a.Close()
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	dsn := newDatabase(t, "dirtest")
	if err := db.MigrateUp(dsn); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.AppConfig = config.AppConfig{APPName: "directory", Env: "test"}
	cfg.DBConfig = config.DBConfig{URL: dsn, MaxConns: 5}
	cfg.HTTPConfig = config.HTTPConfig{PublicURL: "https://directory.test"}
	cfg.AuthConfig = config.AuthConfig{Issuer: issuer, Audience: "boson", AccessCookie: "boson_at"}
	cfg.DirectoryConfig = config.DirectoryConfig{Name: "test directory", WritesPerMinute: 6000, WriteBurst: 1000, InternalToken: internalToken}
	cfg.VerifyConfig = config.VerifyConfig{
		Verifier: "fake", TokenTTL: 72 * time.Hour, LapseAfter: 14 * 24 * time.Hour, VerifiedEvery: 24 * time.Hour,
		PendingEvery: time.Hour, LapsedEvery: 7 * 24 * time.Hour, WorkerTick: time.Minute, OwnerRateLimit: 30 * time.Second,
		WorkerEnabled: true,
	}
	cfg.HealthConfig = config.HealthConfig{Every: 15 * time.Minute, DialTimeout: time.Second}
	cfg.MediaConfig = config.MediaConfig{PublicURL: "https://media.test"}
	for _, m := range mutate {
		m(&cfg)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	clk := &clock{t: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	fake := dnsverify.NewFake()
	store := media.NewMemStorage()
	app, err := dirhttp.Build(context.Background(), cfg, dirhttp.Options{
		Now: clk.Now, Verifier: fake, Storage: store, Locker: leader.Local{},
		TokenVerifier: middleware.NewJWTVerifierWithKeyfunc(func(*jwt.Token) (any, error) { return pub, nil }, issuer, "boson"),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Public)
	sqlDB, _ := app.DB.SQL()
	t.Cleanup(func() {
		srv.Close()
		_ = app.Close()
	})
	return &env{t: t, app: app, srv: srv, dns: fake, clock: clk, key: priv, media: store, dsn: dsn, sqlDB: sqlDB, client: srv.Client()}
}

// token mints an access token for sub with perms.
func (e *env) token(sub string, perms ...string) string {
	e.t.Helper()
	if perms == nil {
		perms = []string{}
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"iss": issuer, "sub": sub, "aud": "boson", "name": strings.TrimPrefix(sub, "usr_"), "role": "user",
		"perms": perms, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	tok.Header["typ"] = "at+jwt"
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(e.key)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func bearer(tok string) string { return "Bearer " + tok }
func netkey(k string) string   { return "Network " + k }

type resp struct {
	Status int
	Header nethttp.Header
	Body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

func (r resp) m(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	r.json(t, &out)
	return out
}

// do sends a request; body is JSON-encoded unless it is []byte. headers
// are "Name: value" strings or an Authorization value.
func (e *env) do(method, path, auth string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd, ct = bytes.NewReader(b), "image/png"
	default:
		buf, _ := json.Marshal(b)
		rd, ct = bytes.NewReader(buf), "application/json"
	}
	req, err := nethttp.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for _, h := range headers {
		k, v, _ := strings.Cut(h, ": ")
		req.Header.Set(k, v)
	}
	res, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Header: res.Header, Body: data}
}

func (e *env) expect(r resp, status int) resp {
	e.t.Helper()
	if r.Status != status {
		e.t.Fatalf("status %d, want %d: %s", r.Status, status, r.Body)
	}
	return r
}

// register creates a listing and returns its id and token.
func (e *env) register(auth, host string, extra map[string]any) (string, string) {
	e.t.Helper()
	body := map[string]any{"hostname": host, "port": 6697, "tls": true, "name": "Net " + host}
	for k, v := range extra {
		body[k] = v
	}
	r := e.expect(e.do("POST", "/v1/networks", auth, body), 201)
	var l struct {
		Network      struct{ ID string }
		Verification struct{ Token string }
	}
	r.json(e.t, &l)
	return l.Network.ID, l.Verification.Token
}

// registerVerified registers and verifies a listing.
func (e *env) registerVerified(auth, host string, extra map[string]any) string {
	e.t.Helper()
	id, _ := e.register(auth, host, extra)
	e.expect(e.do("POST", "/v1/networks/"+id+"/verify", auth, nil), 200)
	return id
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.sqlDB.Exec(q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) status(id string) string {
	e.t.Helper()
	var s string
	if err := e.sqlDB.QueryRow("SELECT status FROM networks WHERE id = $1", id).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) runWorker(name string) {
	e.t.Helper()
	if err := e.app.RunWorkerOnce(context.Background(), name); err != nil {
		e.t.Fatal(err)
	}
}
