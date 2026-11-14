// Package audit records operator and owner actions.
package audit

import (
	"context"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories"
	auditrepo "github.com/boson-chat/directory/internal/db/repositories/audit"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/reqmeta"
)

// Target is what an action was about.
type Target struct {
	NetworkID string
	Hostname  string
}

type AuditServiceImpl interface {
	// Record writes an entry for p (nil p = system). Failures are logged,
	// never returned: auditing must not break the action.
	Record(ctx context.Context, p *principal.Principal, action string, t Target, meta map[string]any)
	// System records an action by a background worker.
	System(ctx context.Context, worker, action string, t Target, meta map[string]any)
	List(ctx context.Context, f auditrepo.Filter) ([]*auditrepo.Entry, error)
}

type AuditService struct {
	repo auditrepo.AuditRepositoryImpl
	now  func() time.Time
}

func NewAuditService(repo auditrepo.AuditRepositoryImpl, now func() time.Time) AuditServiceImpl {
	if now == nil {
		now = time.Now
	}
	return &AuditService{repo: repo, now: now}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *AuditService) write(ctx context.Context, e *auditrepo.Entry, t Target, meta map[string]any) {
	e.At = s.now().UTC()
	e.NetworkID, e.Hostname = strPtr(t.NetworkID), strPtr(t.Hostname)
	e.IP = reqmeta.From(ctx).IP
	e.Meta = repositories.JSONB(meta)
	if err := s.repo.Create(ctx, e); err != nil {
		logger.FromCtx(ctx).Error().Err(err).Str("action", e.Action).Msg("audit write failed")
	}
}

func (s *AuditService) Record(ctx context.Context, p *principal.Principal, action string, t Target, meta map[string]any) {
	e := &auditrepo.Entry{Action: action, ActorKind: auditrepo.ActorSystem}
	if p != nil {
		e.ActorID, e.ActorName = p.ActorID(), p.Username
		switch {
		case p.Kind == principal.KindNetworkKey:
			e.ActorKind = auditrepo.ActorNetworkKey
		case p.IsOperator():
			e.ActorKind = auditrepo.ActorOperator
		default:
			e.ActorKind = auditrepo.ActorAccount
		}
	}
	s.write(ctx, e, t, meta)
}

func (s *AuditService) System(ctx context.Context, worker, action string, t Target, meta map[string]any) {
	s.write(ctx, &auditrepo.Entry{Action: action, ActorKind: auditrepo.ActorSystem, ActorID: worker, ActorName: worker}, t, meta)
}

func (s *AuditService) List(ctx context.Context, f auditrepo.Filter) ([]*auditrepo.Entry, error) {
	return s.repo.List(ctx, f)
}
