// Package peer holds the peers table model. The federation service owns
// its repository (internal/services/federation); this is the shared row type.
package peer

import "time"

// Peer is another directory this one imports listings from.
type Peer struct {
	ID           string `gorm:"primaryKey"`
	URL          string
	Name         string
	Enabled      bool
	Builtin      bool
	Cursor       int64
	LastSyncAt   *time.Time
	LastError    *string
	ListingCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (Peer) TableName() string { return "peers" }
