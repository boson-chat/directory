package network

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/boson-chat/directory/internal/ids"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Move statuses.
const (
	MovePending   = "pending"
	MoveCompleted = "completed"
	MoveCancelled = "cancelled"
	MoveExpired   = "expired"
)

// MaxPreviousHostnames caps networks.previous_hostnames (most recent first).
const MaxPreviousHostnames = 10

var (
	// ErrNoMove: the listing has no pending move (or not the one asked for).
	ErrNoMove = errors.New("no pending move")
	// ErrMoveStale: the listing changed under the move (blocked, or its
	// hostname is no longer the one the move started from).
	ErrMoveStale = errors.New("the listing changed since the move was requested")
)

// Move is a row of network_moves: a request to move a listing to a new
// hostname/port/tls, pending until the new hostname's TXT record carries
// Token, and kept as history afterwards.
type Move struct {
	ID            string `gorm:"primaryKey"`
	NetworkID     string
	FromHostname  string
	FromPort      int
	FromTLS       bool `gorm:"column:from_tls"`
	ToHostname    string
	ToPort        int
	ToTLS         bool `gorm:"column:to_tls"`
	Token         string
	Status        string
	RequestedAt   time.Time
	TokenIssuedAt time.Time
	LastCheckedAt *time.Time
	EndedAt       *time.Time
	ActorKind     string
	ActorID       string
}

func (Move) TableName() string { return "network_moves" }

// MoveCompletion switches a listing to its pending move's target.
type MoveCompletion struct {
	Move *Move
	At   time.Time
	// Replaceable says whether a listing that holds the new hostname may be
	// deleted to make room (an imported or abandoned claim). Anything else
	// holding it is ErrHostnameTaken.
	Replaceable func(holder *Network) bool
}

// PreviousHostnames returns prev with from prepended, without to, without
// duplicates, at most MaxPreviousHostnames long.
func PreviousHostnames(prev []string, from, to string) []string {
	out := make([]string, 0, len(prev)+1)
	for _, h := range append([]string{from}, prev...) {
		if h == "" || h == to || slices.Contains(out, h) {
			continue
		}
		out = append(out, h)
		if len(out) == MaxPreviousHostnames {
			break
		}
	}
	return out
}

func (r *NetworkRepository) PendingMove(ctx context.Context, networkID string) (*Move, error) {
	var m Move
	err := r.db.Conn(ctx).Where("network_id = ? AND status = ?", networkID, MovePending).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoMove
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *NetworkRepository) Moves(ctx context.Context, networkID string, limit int) ([]*Move, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out []*Move
	err := r.db.Conn(ctx).Where("network_id = ?", networkID).Order("requested_at DESC, id DESC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *NetworkRepository) ReplaceMove(ctx context.Context, m *Move, endPrevious func(*Move) string) error {
	if m.ID == "" {
		m.ID = ids.New(ids.Move)
	}
	m.Status = MovePending
	return r.db.WithTx(ctx, func(ctx context.Context) error {
		var cur []*Move
		if err := r.db.Conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("network_id = ? AND status = ?", m.NetworkID, MovePending).Find(&cur).Error; err != nil {
			return err
		}
		for _, c := range cur {
			status := MoveCancelled
			if endPrevious != nil {
				status = endPrevious(c)
			}
			if err := r.db.Conn(ctx).Model(&Move{}).Where("id = ?", c.ID).
				Updates(map[string]any{"status": status, "ended_at": m.RequestedAt}).Error; err != nil {
				return err
			}
		}
		return r.db.Conn(ctx).Create(m).Error
	})
}

func (r *NetworkRepository) EndMove(ctx context.Context, id, status string, at time.Time) (bool, error) {
	res := r.db.Conn(ctx).Model(&Move{}).Where("id = ? AND status = ?", id, MovePending).
		Updates(map[string]any{"status": status, "ended_at": at})
	return res.RowsAffected > 0, res.Error
}

func (r *NetworkRepository) RecordMoveCheck(ctx context.Context, id string, at time.Time) error {
	return r.db.Conn(ctx).Model(&Move{}).Where("id = ? AND status = ?", id, MovePending).
		Update("last_checked_at", at).Error
}

func (r *NetworkRepository) DueMoves(ctx context.Context, now time.Time, every, ttl time.Duration, limit int) ([]*Move, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []*Move
	err := r.db.Conn(ctx).
		Where("status = ? AND token_issued_at > ? AND (last_checked_at IS NULL OR last_checked_at < ?)",
			MovePending, now.Add(-ttl), now.Add(-every)).
		Order("last_checked_at ASC NULLS FIRST, id ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *NetworkRepository) ExpireMoves(ctx context.Context, issuedBefore, at time.Time) (int64, error) {
	res := r.db.Conn(ctx).Model(&Move{}).Where("status = ? AND token_issued_at < ?", MovePending, issuedBefore).
		Updates(map[string]any{"status": MoveExpired, "ended_at": at})
	return res.RowsAffected, res.Error
}

func (r *NetworkRepository) GetByPreviousHostname(ctx context.Context, hostname string) ([]*Network, error) {
	var out []*Network
	err := r.db.Conn(ctx).Where("? = ANY(previous_hostnames)", hostname).
		Order("(status = 'verified') DESC, (origin = 'local') DESC, updated_at DESC, id ASC").Limit(10).Find(&out).Error
	return out, err
}

func (r *NetworkRepository) CompleteMove(ctx context.Context, c MoveCompletion) (*Network, *Network, error) {
	m := c.Move
	var moved, replaced *Network
	err := r.db.WithTx(ctx, func(ctx context.Context) error {
		// Serialise with imports and other moves of the same hostname.
		if err := r.db.Conn(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network:"+m.ToHostname).Error; err != nil {
			return err
		}
		var cur Move
		err := r.db.Conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ? AND token = ?", m.ID, MovePending, m.Token).First(&cur).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoMove
		}
		if err != nil {
			return err
		}
		var n Network
		if err := r.db.Conn(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", cur.NetworkID).First(&n).Error; err != nil {
			return mapErr(err)
		}
		if n.Status == StatusBlocked || n.Hostname != cur.FromHostname {
			return ErrMoveStale
		}
		var holder Network
		err = r.db.Conn(ctx).Where("hostname = ? AND id <> ?", cur.ToHostname, n.ID).First(&holder).Error
		switch {
		case err == nil:
			if c.Replaceable == nil || !c.Replaceable(&holder) {
				return ErrHostnameTaken
			}
			if err := r.db.Conn(ctx).Where("id = ?", holder.ID).Delete(&Network{}).Error; err != nil {
				return err
			}
			replaced = &holder
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		at := c.At.UTC()
		upd := map[string]any{
			"hostname": cur.ToHostname, "port": cur.ToPort, "tls": cur.ToTLS,
			"verification_token": cur.Token, "token_issued_at": cur.TokenIssuedAt,
			"status": StatusVerified, "last_verified_at": at, "last_checked_at": at,
			// Due for a health probe of the new address right away.
			"health_checked_at":  nil,
			"previous_hostnames": pq.StringArray(PreviousHostnames(n.PreviousHostnames, n.Hostname, cur.ToHostname)),
		}
		if err := r.db.Conn(ctx).Model(&Network{}).Where("id = ?", n.ID).Updates(upd).Error; err != nil {
			return mapErr(err)
		}
		if err := r.db.Conn(ctx).Model(&Move{}).Where("id = ?", cur.ID).
			Updates(map[string]any{"status": MoveCompleted, "ended_at": at, "last_checked_at": at}).Error; err != nil {
			return err
		}
		got, err := r.Get(ctx, n.ID)
		if err != nil {
			return err
		}
		moved = got
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return moved, replaced, nil
}
