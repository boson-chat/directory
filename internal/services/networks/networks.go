// Package networks is the directory's listing service: the public browse
// API, owner registration and editing (boson account or Network key), and
// the DNS verification state machine.
//
// Verification state machine (status column):
//
//	pending  --strict 3/3 within TokenTTL of token issue-->  verified
//	pending  --token older than TokenTTL-->                  pending (never checked again until regenerate)
//	verified --lenient 2/3-->                                verified (last_verified_at = now)
//	verified --miss, last_verified_at older than LapseAfter--> lapsed
//	verified --miss, within LapseAfter-->                    verified
//	lapsed   --lenient 2/3 (re-checked every LapsedEvery)--> verified
//	any      --regenerate-token-->                           pending (new token, last_verified_at cleared)
//	any      --block (operator or block pattern)-->          blocked
//	blocked  --unblock-->                                    lapsed (was verified before) or pending
package networks

import (
	"context"
	"net/http"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/dnsverify"
)

// Client-facing errors.
var (
	ErrNotFound       = apperr.New(http.StatusNotFound, "not_found", "not found")
	ErrHostnameTaken  = apperr.New(http.StatusConflict, "hostname_taken", "that hostname is already listed by someone else")
	ErrNotOwner       = apperr.New(http.StatusForbidden, "not_owner", "you don't own this listing")
	ErrBlocked        = apperr.New(http.StatusForbidden, "blocked", "this hostname is blocked on this directory")
	ErrTokenExpired   = apperr.New(http.StatusGone, "token_expired", "verification token expired; regenerate to try again")
	ErrRateLimited    = apperr.New(http.StatusTooManyRequests, "rate_limited", "wait a little before checking DNS again")
	ErrBadCursor      = apperr.New(http.StatusBadRequest, "invalid_cursor", "the cursor is invalid or for a different sort")
	ErrNotLocal       = apperr.New(http.StatusConflict, "not_local", "this listing was imported from another directory and can't be edited here")
	ErrUnauthenticate = apperr.New(http.StatusUnauthorized, "unauthenticated", "sign in or send a Network key")
)

// ListQuery is GET /v1/networks.
type ListQuery struct {
	Query       string
	Language    string
	Tag         string
	IncludeNSFW bool
	// Source: all | local | <peer id> | peer:<peer id>.
	Source string
	// Owner: list this account's listings, any status (needs users.view).
	Owner  string
	Sort   network.Sort
	Cursor string
	Limit  int
}

// Page is a page of networks.
type Page struct {
	Networks      []*network.Network
	NextCursor    string
	TotalEstimate int64
}

// Submission creates (all fields) or edits (name onwards) a listing.
// Hostname, port and tls are fixed once listed.
type Submission struct {
	Hostname    string   `json:"hostname,omitempty"`
	Port        int      `json:"port,omitempty"`
	TLS         *bool    `json:"tls,omitempty"`
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	IsNSFW      *bool    `json:"is_nsfw,omitempty"`
	WebsiteURL  *string  `json:"website_url,omitempty"`
	AuthURL     *string  `json:"auth_url,omitempty"`
}

// Edit is PATCH: nil fields are left alone; Tags/Languages replace when set.
type Edit struct {
	Name        *string
	Description *string
	Tags        *[]string
	Languages   *[]string
	IsNSFW      *bool
	WebsiteURL  *string
	AuthURL     *string
}

// CreateResult: Created is false when an identical registration already
// existed (same key or account and hostname), which is returned as is.
type CreateResult struct {
	Network *network.Network
	Created bool
}

// VerifyOutcome is the result of an owner-triggered DNS check.
type VerifyOutcome struct {
	Network *network.Network
	Report  dnsverify.Report
}

type NetworkServiceImpl interface {
	// Public.
	List(ctx context.Context, p *principal.Principal, q ListQuery) (*Page, error)
	// Get returns a verified listing, or any listing p may manage.
	Get(ctx context.Context, p *principal.Principal, id string) (*network.Network, error)
	// Lookup is Get by hostname.
	Lookup(ctx context.Context, p *principal.Principal, hostname string) (*network.Network, error)
	Facets(ctx context.Context, includeNSFW bool) (tags, languages []network.Facet, err error)

	// Owner (p is an account or a Network key; operators may manage any
	// local listing).
	Mine(ctx context.Context, p *principal.Principal) ([]*network.Network, error)
	Create(ctx context.Context, p *principal.Principal, s Submission) (*CreateResult, error)
	Update(ctx context.Context, p *principal.Principal, id string, e Edit) (*network.Network, error)
	Delete(ctx context.Context, p *principal.Principal, id string) error
	Verify(ctx context.Context, p *principal.Principal, id string) (*VerifyOutcome, error)
	RegenerateToken(ctx context.Context, p *principal.Principal, id string) (*network.Network, error)
	// SetImage stores (raw != nil) or clears an icon/banner.
	SetImage(ctx context.Context, p *principal.Principal, id string, kind ImageKind, raw []byte) (*network.Network, error)
	// CanManage reports whether p may manage n.
	CanManage(p *principal.Principal, n *network.Network) bool

	// Background / federation.
	// CheckOne runs the DNS check the worker would run for n and records it.
	CheckOne(ctx context.Context, n *network.Network) (*network.Network, dnsverify.Report, error)
	// VerifyDue checks every listing that is due; returns how many.
	VerifyDue(ctx context.Context) (int, error)
	// Options exposes the verification timings (TXT expiry for the UI).
	Options() Options
}

// ImageKind is icon or banner.
type ImageKind string

const (
	ImageIcon   ImageKind = "icon"
	ImageBanner ImageKind = "banner"
)

// Options configure the service.
type Options struct {
	TokenTTL       time.Duration
	LapseAfter     time.Duration
	VerifiedEvery  time.Duration
	PendingEvery   time.Duration
	LapsedEvery    time.Duration
	OwnerRateLimit time.Duration
	Now            func() time.Time
}
