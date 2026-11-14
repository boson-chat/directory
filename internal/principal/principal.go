// Package principal is the authenticated caller of a request: a boson
// account (bearer access token from the auth service) or a Network key
// (the stack admin panel's fast path). Middleware stores it in the context.
package principal

import (
	"context"
	"slices"
)

type Kind string

const (
	// KindAccount is a boson account (auth-service access token).
	KindAccount Kind = "account"
	// KindNetworkKey is "Authorization: Network <key>".
	KindNetworkKey Kind = "network_key"
	// KindInternal is a trusted service (the stack admin panel) presenting
	// X-Internal-Token: an operator with every permission. Username is the
	// X-Acting-User it names, for the audit log.
	KindInternal Kind = "internal"
)

// Well-known permissions from the token's perms claim.
const (
	PermDirectoryManage = "directory.manage"
	PermUsersView       = "users.view"
)

type Principal struct {
	Kind Kind
	// Account: the auth user id (token sub, usr_<ULID>), name and perms.
	AuthUserID string
	Username   string
	Role       string
	Perms      []string
	// Network key: sha256 hex of the presented key. The key itself is
	// never kept.
	KeyHash string
	// ViaCookie: the token came from the auth cookie, not the header.
	ViaCookie bool
}

// Has reports whether the account holds perm.
func (p *Principal) Has(perm string) bool {
	if p == nil {
		return false
	}
	if p.Kind == KindInternal {
		return true
	}
	return p.Kind == KindAccount && slices.Contains(p.Perms, perm)
}

// IsOperator reports directory.manage.
func (p *Principal) IsOperator() bool { return p.Has(PermDirectoryManage) }

// ActorID is what the audit log records for p.
func (p *Principal) ActorID() string {
	if p == nil {
		return ""
	}
	if p.Kind == KindInternal {
		return "internal:" + p.Username
	}
	if p.Kind == KindNetworkKey {
		if len(p.KeyHash) > 12 {
			return "key:" + p.KeyHash[:12]
		}
		return "key:" + p.KeyHash
	}
	return p.AuthUserID
}

type ctxKey struct{}

// With returns ctx carrying p.
func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal in ctx, or nil when anonymous.
func From(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}
