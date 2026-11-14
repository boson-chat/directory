package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/golang-jwt/jwt/v5"
)

// Internal service auth headers.
const (
	InternalTokenHeader = "X-Internal-Token"
	ActingUserHeader    = "X-Acting-User"
)

// CSRFHeader must be present on unsafe requests authenticated by cookie.
const CSRFHeader = "X-Requested-With"

var (
	ErrUnauthenticated = apperr.New(nethttp.StatusUnauthorized, "unauthenticated", "missing or invalid credentials")
	errCSRF            = apperr.New(nethttp.StatusForbidden, "csrf", "cookie-authenticated requests must send the X-Requested-With header")
	errNoAccounts      = apperr.New(nethttp.StatusUnauthorized, "accounts_disabled", "account sign-in is not configured on this directory (AUTH_ISSUER)")
)

// AccessClaims are the claims of an auth-service access token.
type AccessClaims struct {
	jwt.RegisteredClaims
	SessionID string   `json:"sid"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Roles     []string `json:"roles"`
	Perms     []string `json:"perms"`
}

// TokenVerifier checks auth-service access tokens.
type TokenVerifier interface {
	Verify(token string) (*AccessClaims, error)
}

// JWTVerifier checks access tokens: EdDSA only, a key from the JWKS
// (cached, refetched on an unknown kid), typ at+jwt, and exp/iss/aud.
type JWTVerifier struct {
	keys     jwt.Keyfunc
	issuer   string
	audience string
}

// NewJWTVerifier starts the JWKS cache. The JWKS being unreachable at
// startup isn't fatal (keyfunc keeps retrying); tokens fail until it loads.
func NewJWTVerifier(ctx context.Context, cfg config.AuthConfig) (*JWTVerifier, error) {
	if cfg.JWKSURL == "" || cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("AUTH_ISSUER, AUTH_JWKS_URL and AUTH_AUDIENCE are required for account sign-in")
	}
	k, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("JWKS %s: %w", cfg.JWKSURL, err)
	}
	return NewJWTVerifierWithKeyfunc(k.Keyfunc, cfg.Issuer, cfg.Audience), nil
}

// NewJWTVerifierWithKeyfunc is NewJWTVerifier with the keys supplied (tests).
func NewJWTVerifierWithKeyfunc(kf jwt.Keyfunc, issuer, audience string) *JWTVerifier {
	return &JWTVerifier{keys: kf, issuer: issuer, audience: audience}
}

func (v *JWTVerifier) Verify(token string) (*AccessClaims, error) {
	var c AccessClaims
	t, err := jwt.ParseWithClaims(token, &c, v.keys,
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return nil, err
	}
	if typ, _ := t.Header["typ"].(string); !strings.EqualFold(typ, "at+jwt") {
		return nil, errors.New("not an access token")
	}
	if !strings.HasPrefix(c.Subject, "usr_") {
		return nil, errors.New("token sub is not an auth user id")
	}
	return &c, nil
}

// HashNetworkKey is what the directory stores for a Network key.
func HashNetworkKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves the caller from (in order) "Authorization: Bearer
// <access token>", "Authorization: Network <key>", or the auth service's
// access-token cookie, and stores it with principal.With. No credentials =
// anonymous (handlers decide); bad credentials = 401. verifier may be nil
// (account sign-in off).
//
// internalToken (DIRECTORY_INTERNAL_TOKEN, empty = off) admits a trusted
// service via "X-Internal-Token: <token>" (compared in constant time) as an
// operator with every permission; "X-Acting-User: <name>" names the person
// for the audit log.
func Authenticate(verifier TokenVerifier, cookieName, internalToken string, onError func(nethttp.ResponseWriter, *nethttp.Request, error)) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			var p *principal.Principal
			var err error
			if tok := r.Header.Get(InternalTokenHeader); tok != "" {
				if internalToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(internalToken)) != 1 {
					onError(w, r, ErrUnauthenticated)
					return
				}
				acting := strings.TrimSpace(r.Header.Get(ActingUserHeader))
				if len(acting) > 64 {
					acting = acting[:64]
				}
				p = &principal.Principal{Kind: principal.KindInternal, Username: acting}
			} else {
				p, err = resolve(r, verifier, cookieName)
			}
			if err != nil {
				onError(w, r, err)
				return
			}
			if p != nil {
				r = r.WithContext(principal.With(r.Context(), p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func unsafeMethod(m string) bool {
	return m != nethttp.MethodGet && m != nethttp.MethodHead && m != nethttp.MethodOptions
}

func resolve(r *nethttp.Request, verifier TokenVerifier, cookieName string) (*principal.Principal, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	viaCookie := false
	var token string
	switch {
	case authz != "":
		scheme, cred, _ := strings.Cut(authz, " ")
		cred = strings.TrimSpace(cred)
		switch strings.ToLower(scheme) {
		case "bearer":
			token = cred
		case "network":
			if len(cred) < 20 || len(cred) > 512 {
				return nil, ErrUnauthenticated
			}
			return &principal.Principal{Kind: principal.KindNetworkKey, KeyHash: HashNetworkKey(cred)}, nil
		default:
			return nil, ErrUnauthenticated
		}
	case cookieName != "":
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			return nil, nil
		}
		token, viaCookie = c.Value, true
	default:
		return nil, nil
	}
	if verifier == nil {
		if viaCookie {
			return nil, nil
		}
		return nil, errNoAccounts
	}
	c, err := verifier.Verify(token)
	if err != nil {
		if viaCookie && !unsafeMethod(r.Method) {
			// A stale cookie shouldn't break public browsing.
			return nil, nil
		}
		return nil, ErrUnauthenticated
	}
	if viaCookie && unsafeMethod(r.Method) && r.Header.Get(CSRFHeader) == "" {
		return nil, errCSRF
	}
	return &principal.Principal{
		Kind: principal.KindAccount, AuthUserID: c.Subject, Username: c.Name, Role: c.Role,
		Perms: c.Perms, ViaCookie: viaCookie,
	}, nil
}
