// Package block is the hostname blocks repository.
package block

import (
	"context"
	"errors"
	"time"

	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories"
	"github.com/boson-chat/directory/internal/ids"
	"gorm.io/gorm"
)

var (
	ErrNotFound = errors.New("block not found")
	ErrExists   = errors.New("block already exists")
)

// Block is an exact hostname or a "*.suffix" pattern.
type Block struct {
	ID        string `gorm:"primaryKey"`
	Pattern   string
	Reason    string
	CreatedBy string
	CreatedAt time.Time
}

func (Block) TableName() string { return "blocks" }

type BlockRepositoryImpl interface {
	Create(ctx context.Context, b *Block) error
	Get(ctx context.Context, id string) (*Block, error)
	List(ctx context.Context) ([]*Block, error)
	Delete(ctx context.Context, id string) error
}

type BlockRepository struct{ db *db.DB }

func NewBlockRepository(d *db.DB) BlockRepositoryImpl { return &BlockRepository{db: d} }

func (r *BlockRepository) Create(ctx context.Context, b *Block) error {
	if b.ID == "" {
		b.ID = ids.New(ids.Block)
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	err := r.db.Conn(ctx).Create(b).Error
	if repositories.UniqueViolation(err) != "" {
		return ErrExists
	}
	return err
}

func (r *BlockRepository) Get(ctx context.Context, id string) (*Block, error) {
	var b Block
	err := r.db.Conn(ctx).Where("id = ?", id).First(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &b, err
}

func (r *BlockRepository) List(ctx context.Context) ([]*Block, error) {
	var out []*Block
	err := r.db.Conn(ctx).Order("created_at DESC, id DESC").Find(&out).Error
	return out, err
}

func (r *BlockRepository) Delete(ctx context.Context, id string) error {
	res := r.db.Conn(ctx).Where("id = ?", id).Delete(&Block{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
