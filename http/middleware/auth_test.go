package middleware

import (
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/boson-chat/directory/internal/principal"
)

type stubVerifier struct{}

func (stubVerifier) Verify(tok string) (*AccessClaims, error) {
	if tok != "good" {
		return nil, errors.New("bad")
	}
	c := &AccessClaims{Perms: []string{"directory.manage"}}
	c.Subject = "usr_x"
	return c, nil
}

func run(t *testing.T, method string, hdr map[string]string) (int, *principal.Principal) {
	t.Helper()
	var got *principal.Principal
	h := Authenticate(stubVerifier{}, "boson_at", "secret-internal", func(w nethttp.ResponseWriter, _ *nethttp.Request, err error) {
		w.WriteHeader(599)
	})(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) { got = principal.From(r.Context()) }))
	req := httptest.NewRequest(method, "/", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got
}

func TestAuthenticate(t *testing.T) {
	if c, p := run(t, "GET", nil); c != 200 || p != nil {
		t.Fatal("anonymous")
	}
	if _, p := run(t, "GET", map[string]string{"Authorization": "Bearer good"}); p == nil || !p.IsOperator() || p.AuthUserID != "usr_x" {
		t.Fatalf("bearer %+v", p)
	}
	if c, _ := run(t, "GET", map[string]string{"Authorization": "Bearer bad"}); c != 599 {
		t.Fatal("bad bearer accepted")
	}
	_, p := run(t, "POST", map[string]string{"Authorization": "Network 0123456789abcdefghijKLMN"})
	if p == nil || p.Kind != principal.KindNetworkKey || p.KeyHash != HashNetworkKey("0123456789abcdefghijKLMN") || len(p.KeyHash) != 64 {
		t.Fatalf("network key %+v", p)
	}
	// Cookie: a stale cookie on GET is anonymous; on POST it needs X-Requested-With.
	if c, p := run(t, "GET", map[string]string{"Cookie": "boson_at=bad"}); c != 200 || p != nil {
		t.Fatal("stale cookie GET")
	}
	if c, _ := run(t, "POST", map[string]string{"Cookie": "boson_at=good"}); c != 599 {
		t.Fatal("cookie POST without CSRF header accepted")
	}
	if _, p := run(t, "POST", map[string]string{"Cookie": "boson_at=good", "X-Requested-With": "x"}); p == nil || !p.ViaCookie {
		t.Fatal("cookie POST with header")
	}
	if c, _ := run(t, "GET", map[string]string{"X-Internal-Token": "wrong"}); c != 599 {
		t.Fatal("wrong internal token accepted")
	}
	if _, p := run(t, "GET", map[string]string{"X-Internal-Token": "secret-internal", "X-Acting-User": "bob"}); p == nil || p.Kind != principal.KindInternal || !p.Has("users.view") || p.ActorID() != "internal:bob" {
		t.Fatalf("internal %+v", p)
	}
}
