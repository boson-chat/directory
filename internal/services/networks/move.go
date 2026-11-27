package networks

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/hostname"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/dnsverify"
)

// Moving a listing to a new hostname.
//
//	verified|lapsed --POST /move {hostname}-->  move pending (new token for the new hostname;
//	                                            the listing keeps serving the old one)
//	move pending    --strict 3/3 on _boson.<new>, within TokenTTL-->
//	                   listing: hostname/port/tls switched, token = the move's, verified now,
//	                   old hostname prepended to previous_hostnames; move: completed
//	move pending    --DELETE /move-->           cancelled
//	move pending    --token older than TokenTTL--> not checked any more (410 token_expired);
//	                   POST /move again for a new token. Closed as expired after MoveRetention.
//
// Same hostname with another port/tls needs no DNS proof and applies at once.

// MoveRetention is how long an expired move request stays visible (and
// can be cancelled or re-requested) before the worker closes it.
const MoveRetention = 30 * 24 * time.Hour

// Move errors.
var (
	ErrNoMove        = apperr.New(http.StatusNotFound, "no_pending_move", "this listing has no pending move")
	ErrMoveNotListed = apperr.New(http.StatusConflict, "not_verified",
		"only a verified (or lapsed) listing can move; while it is pending, delete it and list the new hostname instead")
	ErrMoveStale = apperr.New(http.StatusConflict, "move_stale", "the listing changed since the move was requested; request it again")
	ErrSameAddr  = apperr.New(http.StatusBadRequest, "invalid_request", "that is already the listing's address")
)

// MoveRequest is POST /v1/networks/{id}/move. Port 0 and TLS nil keep the
// listing's current values.
type MoveRequest struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port,omitempty"`
	TLS      *bool  `json:"tls,omitempty"`
}

// MoveState is a listing with its pending move (nil if none) and its move
// history (newest first, when asked for).
type MoveState struct {
	Network *network.Network
	Move    *network.Move
	History []*network.Move
	// Created: a new move request was made (201).
	Created bool
	// Applied: same hostname, new port/tls, switched at once.
	Applied bool
}

// MoveOutcome is the result of checking a pending move's TXT record.
type MoveOutcome struct {
	Network *network.Network
	// Move is the still-pending move, nil once the listing moved.
	Move   *network.Move
	Report dnsverify.Report
	Moved  bool
}

// MoveExpired reports whether m's token is older than the token TTL.
func (s *NetworkService) MoveExpired(m *network.Move) bool {
	return m != nil && s.now().Sub(m.TokenIssuedAt) > s.o.TokenTTL
}

// replaceable: a listing holding a hostname that a local listing may take
// over. The same rules as a registration: an imported listing (unless an
// operator blocked it) or a local claim that was never verified and whose
// token expired.
func (s *NetworkService) replaceable(holder *network.Network) bool {
	if !holder.IsLocal() {
		return holder.Status != network.StatusBlocked
	}
	return holder.Status == network.StatusPending && s.now().Sub(holder.TokenIssuedAt) > s.o.TokenTTL
}

func moveMeta(m *network.Move) map[string]any {
	return map[string]any{
		"from": map[string]any{"hostname": m.FromHostname, "port": m.FromPort, "tls": m.FromTLS},
		"to":   map[string]any{"hostname": m.ToHostname, "port": m.ToPort, "tls": m.ToTLS},
	}
}

func actorKind(p *principal.Principal) string {
	switch {
	case p == nil:
		return "system"
	case p.Kind == principal.KindNetworkKey:
		return "network_key"
	case p.IsOperator():
		return "operator"
	}
	return "account"
}

// PendingMoveOf returns n's pending move, nil if none (or on error, logged).
func (s *NetworkService) PendingMoveOf(ctx context.Context, n *network.Network) *network.Move {
	if n == nil || !n.IsLocal() {
		return nil
	}
	m, err := s.repo.PendingMove(ctx, n.ID)
	if err != nil {
		if !errors.Is(err, network.ErrNoMove) {
			logger.FromCtx(ctx).Warn().Err(err).Str("network_id", n.ID).Msg("loading pending move")
		}
		return nil
	}
	return m
}

// GetMove is GET /v1/networks/{id}/move: the pending move and the history.
func (s *NetworkService) GetMove(ctx context.Context, p *principal.Principal, id string) (*MoveState, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	hist, err := s.repo.Moves(ctx, n.ID, 20)
	if err != nil {
		return nil, err
	}
	return &MoveState{Network: n, Move: s.PendingMoveOf(ctx, n), History: hist}, nil
}

// RequestMove starts (or repeats) a move of listing id to r's address.
func (s *NetworkService) RequestMove(ctx context.Context, p *principal.Principal, id string, r MoveRequest) (*MoveState, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	switch n.Status {
	case network.StatusBlocked:
		return nil, ErrBlocked
	case network.StatusVerified, network.StatusLapsed:
	default:
		return nil, ErrMoveNotListed
	}
	host, err := hostname.Normalize(r.Hostname)
	if err != nil {
		return nil, apperr.New(400, "invalid_hostname", err.Error()+"; its TXT record proves you run it")
	}
	port, tls := n.Port, n.TLS
	if r.Port != 0 {
		if r.Port < 1 || r.Port > 65535 {
			return nil, apperr.Invalid("the port must be between 1 and 65535")
		}
		port = r.Port
	}
	if r.TLS != nil {
		tls = *r.TLS
	}
	if host == n.Hostname {
		return s.changePort(ctx, p, n, port, tls)
	}
	if b, err := s.blocks.Match(ctx, host); err != nil {
		return nil, err
	} else if b != nil {
		return nil, ErrBlocked
	}
	if holder, err := s.repo.GetByHostname(ctx, host); err == nil {
		if !s.replaceable(holder) {
			return nil, ErrHostnameTaken
		}
	} else if !errors.Is(err, network.ErrNotFound) {
		return nil, err
	}
	if cur := s.PendingMoveOf(ctx, n); cur != nil && cur.ToHostname == host && cur.ToPort == port && cur.ToTLS == tls && !s.MoveExpired(cur) {
		// The same request again: keep its token (the owner may already
		// have published it).
		return &MoveState{Network: n, Move: cur}, nil
	}
	now := s.now()
	m := &network.Move{
		NetworkID: n.ID, FromHostname: n.Hostname, FromPort: n.Port, FromTLS: n.TLS,
		ToHostname: host, ToPort: port, ToTLS: tls, Token: NewToken(),
		RequestedAt: now, TokenIssuedAt: now, ActorKind: actorKind(p), ActorID: p.ActorID(),
	}
	err = s.repo.ReplaceMove(ctx, m, func(prev *network.Move) string {
		if s.MoveExpired(prev) {
			return network.MoveExpired
		}
		return network.MoveCancelled
	})
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, p, "network.move_request", target(n), moveMeta(m))
	return &MoveState{Network: n, Move: m, Created: true}, nil
}

// changePort switches port/tls on the same hostname: the hostname's
// TXT record already proves control, so no new token is needed.
func (s *NetworkService) changePort(ctx context.Context, p *principal.Principal, n *network.Network, port int, tls bool) (*MoveState, error) {
	if port == n.Port && tls == n.TLS {
		return nil, ErrSameAddr
	}
	meta := map[string]any{
		"from": map[string]any{"hostname": n.Hostname, "port": n.Port, "tls": n.TLS},
		"to":   map[string]any{"hostname": n.Hostname, "port": port, "tls": tls},
	}
	n.Port, n.TLS, n.HealthCheckedAt = port, tls, nil
	if err := s.repo.Update(ctx, n, "port", "tls", "health_checked_at"); err != nil {
		return nil, mapRepo(err)
	}
	s.audit.Record(ctx, p, "network.move", target(n), meta)
	return &MoveState{Network: n, Move: s.PendingMoveOf(ctx, n), Applied: true}, nil
}

// CancelMove drops the listing's pending move (a no-op without one).
func (s *NetworkService) CancelMove(ctx context.Context, p *principal.Principal, id string) (*MoveState, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	if m := s.PendingMoveOf(ctx, n); m != nil {
		ok, err := s.repo.EndMove(ctx, m.ID, network.MoveCancelled, s.now())
		if err != nil {
			return nil, err
		}
		if ok {
			s.audit.Record(ctx, p, "network.move_cancel", target(n), moveMeta(m))
		}
	}
	return &MoveState{Network: n}, nil
}

// CheckMove is the owner's "Check": the new hostname's TXT record, and the
// move when it is there.
func (s *NetworkService) CheckMove(ctx context.Context, p *principal.Principal, id string) (*MoveOutcome, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	m := s.PendingMoveOf(ctx, n)
	if m == nil {
		return nil, ErrNoMove
	}
	if n.Status == network.StatusBlocked {
		return nil, ErrBlocked
	}
	if s.MoveExpired(m) {
		return nil, ErrTokenExpired
	}
	if !s.allowCheck(MoveCheckKey(n.ID)) {
		return nil, ErrRateLimited
	}
	out, err := s.checkMove(ctx, p, n, m)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, p, "network.move_check", target(n), map[string]any{"success": out.Report.Success, "to_hostname": m.ToHostname})
	return out, nil
}

// MoveCheckKey is the rate-limit key of owner move checks (RetryAfter).
func MoveCheckKey(id string) string { return "move:" + id }

func (s *NetworkService) checkMove(ctx context.Context, p *principal.Principal, n *network.Network, m *network.Move) (*MoveOutcome, error) {
	if b, err := s.blocks.Match(ctx, m.ToHostname); err != nil {
		return nil, err
	} else if b != nil {
		return nil, ErrBlocked
	}
	report, err := s.verifier.Verify(ctx, m.ToHostname, m.Token, dnsverify.ModeStrict)
	now := s.now()
	if rerr := s.repo.RecordMoveCheck(ctx, m.ID, now); rerr != nil {
		return nil, rerr
	}
	m.LastCheckedAt = &now
	if err != nil {
		s.metrics.Verification(ctx, "error")
		return nil, err
	}
	if !report.Success {
		s.metrics.Verification(ctx, "miss")
		return &MoveOutcome{Network: n, Move: m, Report: report}, nil
	}
	s.metrics.Verification(ctx, "match")
	moved, replaced, err := s.repo.CompleteMove(ctx, network.MoveCompletion{Move: m, At: now, Replaceable: s.replaceable})
	switch {
	case errors.Is(err, network.ErrNoMove):
		return nil, ErrNoMove
	case errors.Is(err, network.ErrMoveStale):
		return nil, ErrMoveStale
	case err != nil:
		return nil, mapRepo(err)
	}
	if replaced != nil {
		action := "network.replace_imported"
		if replaced.IsLocal() {
			s.dropImages(ctx, replaced)
			action = "network.replace_expired"
		}
		s.record(ctx, p, action, target(replaced), map[string]any{"origin": replaced.Origin, "by_move_of": n.ID})
	}
	meta := moveMeta(m)
	meta["move_id"] = m.ID
	if n.Status != network.StatusVerified {
		meta["status_from"] = n.Status
		s.metrics.StatusChange(ctx, n.Status, network.StatusVerified)
	}
	s.record(ctx, p, "network.move", audit.Target{NetworkID: n.ID, Hostname: m.FromHostname}, meta)
	return &MoveOutcome{Network: moved, Report: report, Moved: true}, nil
}

// record audits as p, or as the verify worker when p is nil.
func (s *NetworkService) record(ctx context.Context, p *principal.Principal, action string, t audit.Target, meta map[string]any) {
	if p == nil {
		s.audit.System(ctx, "verify", action, t, meta)
		return
	}
	s.audit.Record(ctx, p, action, t, meta)
}

// MovesDue is the verify worker's share of moves: close requests that
// expired more than MoveRetention ago, and check the pending ones that are
// due (every PendingEvery while their token is valid). Returns how many it
// checked.
func (s *NetworkService) MovesDue(ctx context.Context) (int, error) {
	now := s.now()
	if _, err := s.repo.ExpireMoves(ctx, now.Add(-s.o.TokenTTL-MoveRetention), now); err != nil {
		return 0, err
	}
	due, err := s.repo.DueMoves(ctx, now, s.o.PendingEvery, s.o.TokenTTL, 100)
	if err != nil {
		return 0, err
	}
	checked := 0
	for _, m := range due {
		if ctx.Err() != nil {
			return checked, ctx.Err()
		}
		n, err := s.repo.Get(ctx, m.NetworkID)
		if err != nil {
			continue
		}
		if n.Status == network.StatusBlocked {
			_ = s.repo.RecordMoveCheck(ctx, m.ID, now)
			continue
		}
		if _, err := s.checkMove(ctx, nil, n, m); err != nil {
			logger.FromCtx(ctx).Warn().Err(err).Str("network_id", n.ID).Str("to_hostname", m.ToHostname).Msg("move check failed")
			_ = s.repo.RecordMoveCheck(ctx, m.ID, now)
		}
		checked++
	}
	return checked, nil
}

// lookupMoved finds a listing visible to p that moved away from host.
func (s *NetworkService) lookupMoved(ctx context.Context, p *principal.Principal, host string) (*network.Network, error) {
	list, err := s.repo.GetByPreviousHostname(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, n := range list {
		if s.visible(p, n) {
			return n, nil
		}
	}
	return nil, ErrNotFound
}
