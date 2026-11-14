// Package blocks manages hostname blocks. A block is an exact hostname or
// "*.example.com" (example.com and every subdomain). Blocks apply to local
// and imported listings alike: matching listings go to status blocked and
// new registrations or imports are refused.
package blocks

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db"
	blockrepo "github.com/boson-chat/directory/internal/db/repositories/block"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/hostname"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
)

var (
	ErrInvalidPattern = apperr.Invalid("a block is a hostname like irc.example.org or a pattern like *.example.org")
	ErrExists         = apperr.New(http.StatusConflict, "block_exists", "that block already exists")
	ErrNotFound       = apperr.New(http.StatusNotFound, "not_found", "not found")
)

type BlockServiceImpl interface {
	List(ctx context.Context) ([]*blockrepo.Block, error)
	// Add creates a block and blocks every matching listing.
	Add(ctx context.Context, p *principal.Principal, pattern, reason string) (*blockrepo.Block, []string, error)
	// Remove deletes a block and releases listings no other block matches.
	Remove(ctx context.Context, p *principal.Principal, id string) error
	// Match returns the block covering hostname, or nil.
	Match(ctx context.Context, hostname string) (*blockrepo.Block, error)
}

type BlockService struct {
	repo     blockrepo.BlockRepositoryImpl
	networks network.NetworkRepositoryImpl
	tx       db.TxManagerImpl
	audit    audit.AuditServiceImpl
	ttl      time.Duration

	mu       sync.Mutex
	cache    []*blockrepo.Block
	cachedAt time.Time
}

func NewBlockService(repo blockrepo.BlockRepositoryImpl, networks network.NetworkRepositoryImpl, tx db.TxManagerImpl, a audit.AuditServiceImpl) *BlockService {
	return &BlockService{repo: repo, networks: networks, tx: tx, audit: a, ttl: 15 * time.Second}
}

// NormalizePattern canonicalises a hostname or *.suffix pattern.
func NormalizePattern(p string) (string, error) {
	p = strings.TrimSpace(strings.ToLower(p))
	suffix, wild := strings.CutPrefix(p, "*.")
	h, err := hostname.Normalize(suffix)
	if err != nil {
		return "", ErrInvalidPattern
	}
	if wild {
		return "*." + h, nil
	}
	return h, nil
}

// Matches reports whether pattern covers host.
func Matches(pattern, host string) bool {
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return host == suffix || strings.HasSuffix(host, "."+suffix)
	}
	return host == pattern
}

func (s *BlockService) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func (s *BlockService) all(ctx context.Context) ([]*blockrepo.Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil && time.Since(s.cachedAt) < s.ttl {
		return s.cache, nil
	}
	list, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []*blockrepo.Block{}
	}
	s.cache, s.cachedAt = list, time.Now()
	return list, nil
}

func (s *BlockService) List(ctx context.Context) ([]*blockrepo.Block, error) { return s.repo.List(ctx) }

func (s *BlockService) Match(ctx context.Context, host string) (*blockrepo.Block, error) {
	list, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range list {
		if Matches(b.Pattern, host) {
			return b, nil
		}
	}
	return nil, nil
}

func (s *BlockService) Add(ctx context.Context, p *principal.Principal, pattern, reason string) (*blockrepo.Block, []string, error) {
	norm, err := NormalizePattern(pattern)
	if err != nil {
		return nil, nil, err
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 {
		return nil, nil, apperr.Invalid("the reason must be at most 500 characters")
	}
	b := &blockrepo.Block{Pattern: norm, Reason: reason, CreatedBy: p.ActorID()}
	var changed []string
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, b); err != nil {
			if err == blockrepo.ErrExists {
				return ErrExists
			}
			return err
		}
		changed, err = s.networks.BlockMatching(ctx, norm)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	s.invalidate()
	s.audit.Record(ctx, p, "block.add", audit.Target{}, map[string]any{"pattern": norm, "reason": reason, "blocked": changed})
	return b, changed, nil
}

func (s *BlockService) Remove(ctx context.Context, p *principal.Principal, id string) error {
	b, err := s.repo.Get(ctx, id)
	if err == blockrepo.ErrNotFound {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var released []string
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := s.repo.Delete(ctx, id); err != nil {
			return err
		}
		rest, err := s.repo.List(ctx)
		if err != nil {
			return err
		}
		released, err = s.networks.UnblockMatching(ctx, b.Pattern, func(h string) bool {
			for _, o := range rest {
				if Matches(o.Pattern, h) {
					return true
				}
			}
			return false
		})
		return err
	})
	if err != nil {
		return err
	}
	s.invalidate()
	s.audit.Record(ctx, p, "block.remove", audit.Target{}, map[string]any{"pattern": b.Pattern, "released": released})
	return nil
}
