package peer

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories"
	"github.com/boson-chat/directory/internal/ids"
	"gorm.io/gorm"
)

var (
	ErrNotFound = errors.New("peer not found")
	ErrExists   = errors.New("peer already exists")
)

// generation moves whenever a peer is created, deleted, or its label or
// state (name, url, enabled, builtin) changes in this process, so caches
// of peer names (core's origin_name / facet sources) can refresh at once
// instead of waiting for their TTL.
var generation atomic.Uint64

// Generation is the current peer-change generation (see generation).
func Generation() uint64 { return generation.Load() }

// labelColumns are the columns whose change bumps the generation.
var labelColumns = map[string]bool{"name": true, "url": true, "enabled": true, "builtin": true}

type PeerRepositoryImpl interface {
	// List returns every peer, builtin first, then oldest first.
	List(ctx context.Context) ([]*Peer, error)
	Get(ctx context.Context, id string) (*Peer, error)
	GetByURL(ctx context.Context, url string) (*Peer, error)
	// GetBuiltin returns the built-in main peer (ErrNotFound when none).
	GetBuiltin(ctx context.Context) (*Peer, error)
	// Create inserts p. ErrExists when the URL (or a second builtin) exists.
	Create(ctx context.Context, p *Peer) error
	// Update writes the named columns of p (updated_at is always set).
	Update(ctx context.Context, p *Peer, columns ...string) error
	Delete(ctx context.Context, id string) error
	// OriginIDs returns the origin_id of every listing imported with origin.
	OriginIDs(ctx context.Context, origin string) ([]string, error)
}

type PeerRepository struct{ db *db.DB }

func NewPeerRepository(d *db.DB) PeerRepositoryImpl { return &PeerRepository{db: d} }

func mapErr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if c := repositories.UniqueViolation(err); c != "" {
		return ErrExists
	}
	return err
}

func (r *PeerRepository) List(ctx context.Context) ([]*Peer, error) {
	var out []*Peer
	err := r.db.Conn(ctx).Order("builtin DESC, created_at ASC, id ASC").Find(&out).Error
	return out, err
}

func (r *PeerRepository) first(ctx context.Context, where string, args ...any) (*Peer, error) {
	var p Peer
	if err := r.db.Conn(ctx).Where(where, args...).First(&p).Error; err != nil {
		return nil, mapErr(err)
	}
	return &p, nil
}

func (r *PeerRepository) Get(ctx context.Context, id string) (*Peer, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *PeerRepository) GetByURL(ctx context.Context, url string) (*Peer, error) {
	return r.first(ctx, "url = ?", url)
}

func (r *PeerRepository) GetBuiltin(ctx context.Context) (*Peer, error) {
	return r.first(ctx, "builtin")
}

func (r *PeerRepository) Create(ctx context.Context, p *Peer) error {
	if p.ID == "" {
		p.ID = ids.New(ids.Peer)
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if err := r.db.Conn(ctx).Create(p).Error; err != nil {
		return mapErr(err)
	}
	generation.Add(1)
	return nil
}

func (r *PeerRepository) Update(ctx context.Context, p *Peer, columns ...string) error {
	p.UpdatedAt = time.Now().UTC()
	columns = append(columns, "updated_at")
	res := r.db.Conn(ctx).Model(&Peer{ID: p.ID}).Select(columns).Updates(p)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	for _, c := range columns {
		if labelColumns[c] {
			generation.Add(1)
			break
		}
	}
	return nil
}

func (r *PeerRepository) Delete(ctx context.Context, id string) error {
	res := r.db.Conn(ctx).Where("id = ?", id).Delete(&Peer{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	generation.Add(1)
	return nil
}

func (r *PeerRepository) OriginIDs(ctx context.Context, origin string) ([]string, error) {
	var out []string
	err := r.db.Conn(ctx).Raw("SELECT origin_id FROM networks WHERE origin = ? AND origin_id IS NOT NULL", origin).
		Scan(&out).Error
	return out, err
}
