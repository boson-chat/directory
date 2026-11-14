// Command fakes is the test double for the two-directory docker test
// (make test-federation): an authoritative-ish DNS server answering
// _boson.<host> TXT queries from an in-memory table, and a minimal auth
// issuer (JWKS + operator access tokens).
//
//	PUT    /txt/{host}   body = token  → _boson.<host> TXT "boson-verify=<token>"
//	DELETE /txt/{host}
//	GET    /.well-known/jwks.json
//	GET    /token?perms=directory.manage
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/miekg/dns"
)

type txtTable struct {
	mu  sync.Mutex
	txt map[string]string // fqdn → token
}

func (t *txtTable) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	for _, q := range r.Question {
		if q.Qtype != dns.TypeTXT {
			continue
		}
		t.mu.Lock()
		tok, ok := t.txt[strings.ToLower(q.Name)]
		t.mu.Unlock()
		if !ok {
			continue
		}
		m.Answer = append(m.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 0},
			Txt: []string{"v=other", "boson-verify=" + tok},
		})
	}
	if len(m.Answer) == 0 {
		m.Rcode = dns.RcodeNameError
	}
	_ = w.WriteMsg(m)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	issuer := env("ISSUER", "http://fakes:8080")
	table := &txtTable{txt: map[string]string{}}
	for _, netw := range []string{"udp", "tcp"} {
		srv := &dns.Server{Addr: env("DNS_ADDR", ":53"), Net: netw, Handler: table}
		go func() { log.Fatal(srv.ListenAndServe()) }()
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("PUT /txt/{host}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
		table.mu.Lock()
		table.txt[dns.Fqdn("_boson."+strings.ToLower(r.PathValue("host")))] = strings.TrimSpace(string(b))
		table.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /txt/{host}", func(w http.ResponseWriter, r *http.Request) {
		table.mu.Lock()
		delete(table.txt, dns.Fqdn("_boson."+strings.ToLower(r.PathValue("host"))))
		table.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "OKP", "crv": "Ed25519", "kid": "e2e", "alg": "EdDSA", "use": "sig",
			"x": base64.RawURLEncoding.EncodeToString(pub),
		}}})
	})
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		var perms []string
		if p := r.URL.Query().Get("perms"); p != "" {
			perms = strings.Split(p, ",")
		}
		now := time.Now()
		tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
			"iss": issuer, "aud": "boson", "sub": "usr_01E2E0000000000000000000OP", "name": "e2e-operator",
			"role": "admin", "perms": perms, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		})
		tok.Header["typ"] = "at+jwt"
		tok.Header["kid"] = "e2e"
		s, err := tok.SignedString(priv)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = w.Write([]byte(s))
	})
	log.Fatal(http.ListenAndServe(env("HTTP_ADDR", ":8080"), mux))
}
