// Package network is the networks repository: listings, their keyset
// pagination, search, facets, the verify/health bookkeeping and the
// federation helpers (upsert by origin, the seq-ordered change feed).
package network

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Statuses.
const (
	StatusPending  = "pending"
	StatusVerified = "verified"
	StatusLapsed   = "lapsed"
	StatusBlocked  = "blocked"
)

// Health statuses.
const (
	HealthUnknown = "unknown"
	HealthUp      = "up"
	HealthDown    = "down"
)

// OriginLocal is a listing registered on this directory. Imported ones are
// "peer:<peer id>" (see PeerOrigin).
const OriginLocal = "local"

// PeerOrigin is the origin value for listings imported from peer id.
func PeerOrigin(peerID string) string { return "peer:" + peerID }

var (
	ErrNotFound      = errors.New("network not found")
	ErrHostnameTaken = errors.New("hostname already listed")
)

// Network is a row of networks.
type Network struct {
	ID          string `gorm:"primaryKey"`
	Hostname    string
	Port        int
	TLS         bool `gorm:"column:tls"`
	Name        string
	Description string
	Tags        pq.StringArray `gorm:"type:text[]"`
	Languages   pq.StringArray `gorm:"type:text[]"`
	IsNSFW      bool           `gorm:"column:is_nsfw"`
	WebsiteURL  *string
	AuthURL     *string
	IconKey     *string
	BannerKey   *string
	// Theme is the boson-theme/1 document (internal/services/theme), NULL
	// when the listing has none. Image fields hold media keys or URLs.
	Theme             JSONB `gorm:"type:jsonb"`
	OwnerAuthUserID   *string
	ManagementKeyHash *string
	Origin            string
	OriginID          *string
	Status            string
	VerificationToken string
	TokenIssuedAt     time.Time
	LastCheckedAt     *time.Time
	LastVerifiedAt    *time.Time
	HealthStatus      string
	HealthCheckedAt   *time.Time
	UserCount         *int
	UserCountAt       *time.Time
	LegacyID          *uuid.UUID `gorm:"type:uuid"`
	// Seq, CreatedAt and UpdatedAt are maintained by the database.
	Seq       int64     `gorm:"->"`
	CreatedAt time.Time `gorm:"<-:create"`
	UpdatedAt time.Time `gorm:"->"`
}

func (Network) TableName() string { return "networks" }

// JSONB is a jsonb column; empty is NULL.
type JSONB []byte

// Value implements driver.Valuer.
func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSONB) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append(JSONB(nil), v...)
	case string:
		*j = JSONB(v)
	default:
		return fmt.Errorf("network: can't scan %T into JSONB", src)
	}
	return nil
}

// IsLocal reports origin=local.
func (n *Network) IsLocal() bool { return n.Origin == OriginLocal }

// Sort orders for List.
type Sort string

const (
	SortUsers  Sort = "users"  // user_count desc (unknown last), newest first on ties
	SortActive Sort = "active" // most recently counted first
	SortNewest Sort = "newest" // created_at desc
	SortName   Sort = "name"   // lower(name) asc
)

// ValidSort reports whether s is a known sort.
func ValidSort(s Sort) bool {
	return s == SortUsers || s == SortActive || s == SortNewest || s == SortName
}

// Cursor is a keyset position: the sort keys and id of the last row of the
// previous page (see KeysOf).
type Cursor struct {
	Sort Sort     `json:"s"`
	Keys []string `json:"k"`
	ID   string   `json:"i"`
}

// ListFilter selects networks.
type ListFilter struct {
	Query       string
	Language    string
	Tag         string
	IncludeNSFW bool
	// Source: "" or "all" (any origin), "local", or "peer:<id>".
	Source string
	// Statuses defaults to verified only.
	Statuses []string
	// AnyStatus lists every status (operator and owner views).
	AnyStatus       bool
	OwnerAuthUserID string
	KeyHash         string
	Sort            Sort
	After           *Cursor
	Limit           int
	// Offset is only for the legacy /servers endpoint.
	Offset int
}

// Facet is a tag or language with its listing count.
type Facet struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// Change is one entry of the seq-ordered change feed. Exactly one of
// Network / Deleted is set.
type Change struct {
	Seq     int64
	Network *Network
	Deleted *Tombstone
}

// Tombstone is a deleted network.
type Tombstone struct {
	Seq       int64 `gorm:"primaryKey"`
	NetworkID string
	Hostname  string
	Port      int
	Origin    string
	OriginID  *string
	DeletedAt time.Time
}

func (Tombstone) TableName() string { return "network_tombstones" }

// UpsertResult says what UpsertByOrigin did.
type UpsertResult string

const (
	UpsertCreated UpsertResult = "created"
	UpsertUpdated UpsertResult = "updated"
	// UpsertConflict: the hostname belongs to a local listing or another
	// peer's verified listing, so nothing was written.
	UpsertConflict UpsertResult = "conflict"
	// UpsertUnchanged: the row already had these values.
	UpsertUnchanged UpsertResult = "unchanged"
)

// VerifyDue selects listings the verify worker should check now.
type VerifyDue struct {
	Now time.Time
	// Re-check cadence per status.
	VerifiedEvery, PendingEvery, LapsedEvery time.Duration
	// Local pending listings whose token is older than this are skipped
	// (expired pending stays pending until the owner regenerates).
	TokenTTL time.Duration
	Limit    int
}

// VerifyResult is the outcome the worker records for one listing.
type VerifyResult struct {
	ID string
	// CheckedToken must still be the row's token, else nothing is written
	// (the owner regenerated in the meantime).
	CheckedToken string
	Status       string
	CheckedAt    time.Time
	// VerifiedAt is set when the check succeeded.
	VerifiedAt *time.Time
}

// HealthResult is the outcome of one health probe.
type HealthResult struct {
	ID        string
	Status    string
	CheckedAt time.Time
	// UserCount is set when LUSERS was read.
	UserCount *int
}

type NetworkRepositoryImpl interface {
	// Create inserts n. ErrHostnameTaken when the hostname is listed.
	Create(ctx context.Context, n *Network) error
	Get(ctx context.Context, id string) (*Network, error)
	GetByHostname(ctx context.Context, hostname string) (*Network, error)
	GetByLegacyID(ctx context.Context, id uuid.UUID) (*Network, error)
	// List returns up to f.Limit rows in f.Sort order after f.After.
	List(ctx context.Context, f ListFilter) ([]*Network, error)
	// Count counts the rows List would page through (ignores After/Limit).
	Count(ctx context.Context, f ListFilter) (int64, error)
	// Facets counts tags and languages over the verified listings
	// (NSFW only when includeNSFW), most used first.
	Facets(ctx context.Context, includeNSFW bool, limit int) (tags, languages []Facet, err error)
	// OriginCounts counts verified listings per origin (NSFW only when
	// includeNSFW).
	OriginCounts(ctx context.Context, includeNSFW bool) (map[string]int64, error)
	// Update writes the named columns of n.
	Update(ctx context.Context, n *Network, columns ...string) error
	Delete(ctx context.Context, id string) error

	// DueForVerify returns the listings the verify worker should check.
	DueForVerify(ctx context.Context, d VerifyDue) ([]*Network, error)
	// RecordVerify writes a check result if the token is unchanged.
	RecordVerify(ctx context.Context, r VerifyResult) (bool, error)
	// DueForHealth returns verified listings not probed since before.
	DueForHealth(ctx context.Context, before time.Time, limit int) ([]*Network, error)
	RecordHealth(ctx context.Context, r HealthResult) error
	// BlockMatching sets status=blocked on every listing whose hostname
	// matches pattern (exact or "*.suffix"), returning the ids changed.
	BlockMatching(ctx context.Context, pattern string) ([]string, error)
	// UnblockMatching moves blocked listings matching pattern (and no
	// other remaining block) back to lapsed (verified before) or pending,
	// due for an immediate re-check.
	UnblockMatching(ctx context.Context, pattern string, stillBlocked func(hostname string) bool) ([]string, error)

	// Federation.
	// UpsertByOrigin inserts or updates the listing identified by
	// (n.Origin, n.OriginID). Conflicts: a local listing always keeps its
	// hostname; another peer's verified listing keeps it too; another
	// peer's unverified listing is replaced. Status and the verification
	// bookkeeping of an existing row are kept unless the token changed
	// (then it goes back to pending).
	UpsertByOrigin(ctx context.Context, n *Network) (UpsertResult, error)
	// DeleteByOrigin removes the listing (origin, originID).
	DeleteByOrigin(ctx context.Context, origin, originID string) (bool, error)
	// DeleteAllByOrigin removes every listing of origin (peer removed/disabled).
	DeleteAllByOrigin(ctx context.Context, origin string) (int64, error)
	// CountByOrigin counts listings of origin.
	CountByOrigin(ctx context.Context, origin string) (int64, error)
	// Changes returns networks and tombstones with seq > since, in seq
	// order, at most limit.
	Changes(ctx context.Context, since int64, limit int) ([]Change, error)
}
