// Package peer is the peers repository: the other directories this one
// imports listings from. Only internal/services/federation uses it.
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
	// LastFullSyncAt is the last complete walk of the peer's feed (000002).
	LastFullSyncAt *time.Time
	// API is the peer's API base from its .well-known document (000002).
	API string `gorm:"column:api"`
	// ConfigManaged: listed in the stack config file's directory.peers (000004).
	ConfigManaged bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (Peer) TableName() string { return "peers" }
