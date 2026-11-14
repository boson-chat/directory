// Package audit is the audit repository (operator and owner actions).
package audit

import (
	"context"
	"time"

	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories"
	"github.com/boson-chat/directory/internal/ids"
)

// Actor kinds.
const (
	ActorAccount    = "account"
	ActorNetworkKey = "network_key"
	ActorOperator   = "operator"
	ActorSystem     = "system"
)

type Entry struct {
	ID        string `gorm:"primaryKey"`
	At        time.Time
	ActorKind string
	ActorID   string
	ActorName string
	Action    string
	NetworkID *string
	Hostname  *string
	IP        string
	Meta      repositories.JSONB `gorm:"type:jsonb"`
}

func (Entry) TableName() string { return "audit" }

// Cursor is the keyset position (at, id) of the last entry of a page.
type Cursor struct {
	At time.Time
	ID string
}

// Filter selects entries, newest first.
type Filter struct {
	NetworkID string
	Action    string
	After     *Cursor
	Limit     int
}

type AuditRepositoryImpl interface {
	Create(ctx context.Context, e *Entry) error
	List(ctx context.Context, f Filter) ([]*Entry, error)
}

type AuditRepository struct{ db *db.DB }

func NewAuditRepository(d *db.DB) AuditRepositoryImpl { return &AuditRepository{db: d} }

func (r *AuditRepository) Create(ctx context.Context, e *Entry) error {
	if e.ID == "" {
		e.ID = ids.New(ids.Audit)
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	return r.db.Conn(ctx).Create(e).Error
}

func (r *AuditRepository) List(ctx context.Context, f Filter) ([]*Entry, error) {
	q := r.db.Conn(ctx).Model(&Entry{})
	if f.NetworkID != "" {
		q = q.Where("network_id = ?", f.NetworkID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.After != nil {
		q = q.Where("(at, id) < (?, ?)", f.After.At, f.After.ID)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var out []*Entry
	err := q.Order("at DESC, id DESC").Limit(f.Limit).Find(&out).Error
	return out, err
}
