// Package federation lets directories import each other's listings.
//
// A directory exports its own (origin=local, verified) listings as a
// seq-ordered change feed (GET /v1/feed). Operators add other directories
// as peers; the sync worker pulls each enabled peer's feed from a saved
// cursor and upserts the listings with origin=peer:<id> and the peer's
// verification token. An imported listing starts pending and only counts
// as verified here once this directory's own DNS check finds the token in
// the network's _boson TXT record. Peers never share account or key data.
//
// Conflicts (enforced by the network repository): a local listing always
// keeps its hostname; between peers the first verified listing wins.
// Blocks apply to imports. Disabling or deleting a peer removes its
// listings. The main directory (setting main_peer_url, env default
// DIRECTORY_MAIN_PEER) is a built-in peer, kept in place on start and
// whenever the include_main / main_peer_url settings change (env default
// DIRECTORY_INCLUDE_MAIN); an operator disabling it at runtime is kept
// across restarts. A directory never peers with itself: when the main
// peer URL is this directory, it *is* the main directory (IsMain) and has
// no builtin peer.
package federation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/db/repositories/peer"
	"github.com/boson-chat/directory/internal/leader"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/blocks"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/boson-chat/directory/internal/services/settings"
)

// Client-facing errors.
var (
	ErrPeerNotFound  = apperr.New(http.StatusNotFound, "not_found", "peer not found")
	ErrPeerExists    = apperr.New(http.StatusConflict, "peer_exists", "that directory is already a peer")
	ErrSelfPeer      = apperr.New(http.StatusBadRequest, "self_peer", "that URL is this directory; a directory can't peer with itself")
	ErrBuiltinDelete = apperr.New(http.StatusConflict, "builtin_peer", "the built-in main directory can't be deleted; disable it instead")
	ErrSyncBusy      = apperr.New(http.StatusConflict, "sync_in_progress", "a sync of this peer is already running")
	ErrPeerDisabled  = apperr.New(http.StatusConflict, "peer_disabled", "enable the peer before syncing it")
	ErrBadSince      = apperr.Invalid("since must be a non-negative integer")
)

func errUnreachable(err error) error {
	return apperr.New(http.StatusBadGateway, "peer_unreachable", err.Error())
}

// WorkerName is the background sync job (and its leader lock).
const WorkerName = "federation-sync"

// SyncPoll is how often the sync worker looks for due peers; sync_every
// (a runtime setting) decides when each peer is due.
var SyncPoll = 30 * time.Second

// SyncNowBudget caps an operator-triggered sync (the HTTP write timeout is 60s).
var SyncNowBudget = 45 * time.Second

// PeerUpdate is PATCH /v1/admin/peers/{id}; nil fields are left alone.
type PeerUpdate struct {
	Enabled *bool
	Name    *string
}

// SyncResult counts what one sync of a peer did.
type SyncResult struct {
	Full      bool     `json:"full"`
	Pages     int      `json:"pages"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Unchanged int      `json:"unchanged"`
	Deleted   int      `json:"deleted"`
	Conflicts int      `json:"conflicts"`
	Blocked   int      `json:"blocked"`
	Rejected  int      `json:"rejected"`
	Skipped   int      `json:"skipped"`
	Verified  int      `json:"verified"`
	Problems  []string `json:"problems,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type FederationServiceImpl interface {
	// Feed is GET /v1/feed: this directory's own listings changed after since.
	Feed(ctx context.Context, since int64, limit int) (*FeedPage, error)

	// Operator peer management.
	ListPeers(ctx context.Context) ([]*peer.Peer, error)
	GetPeer(ctx context.Context, id string) (*peer.Peer, error)
	// CheckPeer normalises rawURL and fetches/validates its .well-known
	// document without saving anything.
	CheckPeer(ctx context.Context, rawURL string) (string, *WellKnown, error)
	// Sources are the active peers, for the source filter / facets.
	Sources(ctx context.Context) ([]Source, error)
	// AddPeer validates the URL and the peer's .well-known document.
	AddPeer(ctx context.Context, p *principal.Principal, rawURL, name string) (*peer.Peer, error)
	UpdatePeer(ctx context.Context, p *principal.Principal, id string, u PeerUpdate) (*peer.Peer, error)
	DeletePeer(ctx context.Context, p *principal.Principal, id string) error
	SyncNow(ctx context.Context, p *principal.Principal, id string) (*peer.Peer, *SyncResult, error)

	// SyncAll is the background worker: every active peer whose last sync
	// is at least sync_every old, and removal of listings left by inactive
	// ones.
	SyncAll(ctx context.Context) error
	// EnsurePeers creates/updates the built-in main peer and seeds
	// DIRECTORY_PEERS once (module start).
	EnsurePeers(ctx context.Context) error
	// Active reports whether p is synced (enabled, and for the builtin,
	// include_main on and not this directory).
	Active(p *peer.Peer) bool

	// IsSelf reports whether rawURL is this directory's own URL.
	IsSelf(rawURL string) bool
	// IsMain reports whether this directory is the main directory (the
	// main peer URL is its own), so include_main does not apply.
	IsMain() bool
	// MainPeer returns the builtin peer row, nil when there is none (it
	// may exist while include_main is off; Active says whether it is used).
	MainPeer(ctx context.Context) (*peer.Peer, error)
	// ApplyMainSettings re-applies include_main / main_peer_url at runtime:
	// creates, re-points or switches off the builtin peer. enable also
	// re-enables a builtin an operator had disabled.
	ApplyMainSettings(ctx context.Context, pr *principal.Principal, enable bool) error

	// ApplyConfigFile brings the peers in line with the stack config file
	// (start-up and every reload; no-op in database mode): the builtin peer
	// follows include_main / main_peer_url (re-enabled when the file says
	// include_main: true), every directory.peers URL exists, is enabled and
	// is marked config-managed, and a config-managed peer the file no longer
	// lists is disabled (kept, not deleted) and released to the peers API.
	ApplyConfigFile(ctx context.Context) error
	// ManagedByConfigFile reports whether the stack config file owns p:
	// the peers API refuses to change or delete it (409).
	ManagedByConfigFile(p *peer.Peer) bool
}

// ConfigActor is the audit actor of changes made by the config file.
const ConfigActor = "config file"

// Deps are the service's collaborators.
type Deps struct {
	Peers    peer.PeerRepositoryImpl
	Networks network.NetworkRepositoryImpl
	Blocks   blocks.BlockServiceImpl
	Verifier dnsverify.Verifier
	Audit    audit.AuditServiceImpl
	Locker   leader.LockerImpl
	Tx       db.TxManagerImpl
	Client   *Client
	// MediaURL turns a stored icon/banner key into its public URL.
	MediaURL func(key string) string
	Config   config.FederationConfig
	// Settings supplies include_main, main_peer_url and sync_every at
	// runtime. Nil: fixed values from Config.
	Settings settings.SettingsServiceImpl
	// PublicURL is this directory's DIRECTORY_PUBLIC_URL (self-detection).
	PublicURL string
	// Strict: ENV=production (https peers, public IPs, https image URLs).
	Strict bool
	// LapseAfter is the verification lapse window (state machine).
	LapseAfter time.Duration
	Now        func() time.Time
}

type FederationService struct {
	d    Deps
	self map[string]bool
}

func NewFederationService(d Deps) *FederationService {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.MediaURL == nil {
		d.MediaURL = func(k string) string { return k }
	}
	if d.Locker == nil {
		d.Locker = leader.Local{}
	}
	if d.Tx == nil {
		d.Tx = db.NoTx{}
	}
	if d.Settings == nil {
		d.Settings = settings.Static{V: settings.Values{
			IncludeMain: d.Config.IncludeMain, MainPeerURL: d.Config.MainPeer, SyncEvery: d.Config.SyncEvery,
		}}
	}
	s := &FederationService{d: d, self: map[string]bool{}}
	if n, err := NormalizeURL(d.PublicURL, false); err == nil {
		s.self[n] = true
	}
	return s
}

func (s *FederationService) now() time.Time { return s.d.Now().UTC() }

// isSelfURL reports whether base (normalised) is one of this directory's URLs.
func (s *FederationService) isSelfURL(base string) bool { return s.self[base] }

// isSelfWellKnown compares a peer's advertised API with this directory's.
func (s *FederationService) isSelfWellKnown(wk *WellKnown) bool {
	api := strings.TrimRight(strings.TrimSpace(wk.API), "/")
	if n, err := NormalizeURL(strings.TrimSuffix(api, "/v1"), false); err == nil {
		return s.self[n]
	}
	return false
}

// mainURL is the normalised main peer URL ("" when unset/invalid).
func (s *FederationService) mainURL() string { return s.mainURLOf(s.d.Settings.Current()) }

func (s *FederationService) mainURLOf(v settings.Values) string {
	if strings.TrimSpace(v.MainPeerURL) == "" {
		return ""
	}
	u, err := NormalizeURL(v.MainPeerURL, s.d.Strict)
	if err != nil {
		return ""
	}
	return u
}

// includeMain: the builtin peer is in use at all.
func (s *FederationService) includeMain() bool {
	v := s.d.Settings.Current()
	m := s.mainURLOf(v)
	return v.IncludeMain && m != "" && !s.isSelfURL(m)
}

func (s *FederationService) IsSelf(rawURL string) bool {
	u, err := NormalizeURL(rawURL, false)
	return err == nil && s.isSelfURL(u)
}

func (s *FederationService) IsMain() bool {
	m := s.mainURL()
	return m != "" && s.isSelfURL(m)
}

func (s *FederationService) MainPeer(ctx context.Context) (*peer.Peer, error) {
	p, err := s.d.Peers.GetBuiltin(ctx)
	if errors.Is(err, peer.ErrNotFound) {
		return nil, nil
	}
	return p, err
}

func (s *FederationService) ApplyMainSettings(ctx context.Context, pr *principal.Principal, enable bool) error {
	if err := s.ensureBuiltin(ctx); err != nil {
		return err
	}
	if !enable || !s.includeMain() {
		return nil
	}
	p, err := s.MainPeer(ctx)
	if err != nil || p == nil || p.Enabled {
		return err
	}
	p.Enabled = true
	if err := s.d.Peers.Update(ctx, p, "enabled"); err != nil {
		return err
	}
	s.d.Audit.Record(ctx, pr, "peer.enable", audit.Target{}, map[string]any{"peer_id": p.ID, "url": p.URL, "builtin": true})
	return nil
}

func (s *FederationService) Active(p *peer.Peer) bool {
	return p.Enabled && (!p.Builtin || s.includeMain())
}

// visible: operators see every peer except a builtin that is switched off
// by the include_main setting (or the main peer is this directory).
func (s *FederationService) visible(p *peer.Peer) bool { return !p.Builtin || s.includeMain() }

// ---- feed ----

func (s *FederationService) Feed(ctx context.Context, since int64, limit int) (*FeedPage, error) {
	if since < 0 {
		return nil, ErrBadSince
	}
	if limit <= 0 {
		limit = DefaultFeedLimit
	}
	limit = min(limit, MaxFeedLimit)
	changes, err := s.d.Networks.Changes(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	return buildFeed(changes, since, limit, s.d.MediaURL), nil
}

// ---- peers ----

func (s *FederationService) ListPeers(ctx context.Context) ([]*peer.Peer, error) {
	all, err := s.d.Peers.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*peer.Peer, 0, len(all))
	for _, p := range all {
		if s.visible(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *FederationService) GetPeer(ctx context.Context, id string) (*peer.Peer, error) {
	p, err := s.d.Peers.Get(ctx, id)
	if errors.Is(err, peer.ErrNotFound) || (err == nil && !s.visible(p)) {
		return nil, ErrPeerNotFound
	}
	return p, err
}

func cleanPeerName(v string) (string, error) {
	v = strings.TrimSpace(v)
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	if len([]rune(v)) > 100 {
		return "", apperr.Invalid("the peer name must be at most 100 characters")
	}
	return v, nil
}

// Source is an active peer as a listing source (?source=<id>).
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (s *FederationService) Sources(ctx context.Context) ([]Source, error) {
	all, err := s.d.Peers.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []Source{}
	for _, p := range all {
		if s.Active(p) {
			out = append(out, Source{ID: p.ID, Name: p.Name, URL: p.URL})
		}
	}
	return out, nil
}

func (s *FederationService) CheckPeer(ctx context.Context, rawURL string) (string, *WellKnown, error) {
	u, err := NormalizeURL(rawURL, s.d.Strict)
	if err != nil {
		return "", nil, apperr.Invalid(err.Error())
	}
	if s.isSelfURL(u) {
		return "", nil, ErrSelfPeer
	}
	wk, err := s.d.Client.FetchWellKnown(ctx, u)
	if err != nil {
		return "", nil, errUnreachable(err)
	}
	if s.isSelfWellKnown(wk) {
		return "", nil, ErrSelfPeer
	}
	return u, wk, nil
}

func (s *FederationService) AddPeer(ctx context.Context, p *principal.Principal, rawURL, name string) (*peer.Peer, error) {
	u, err := NormalizeURL(rawURL, s.d.Strict)
	if err != nil {
		return nil, apperr.Invalid(err.Error())
	}
	if s.isSelfURL(u) {
		return nil, ErrSelfPeer
	}
	if name, err = cleanPeerName(name); err != nil {
		return nil, err
	}
	if _, err := s.d.Peers.GetByURL(ctx, u); err == nil {
		return nil, ErrPeerExists
	} else if !errors.Is(err, peer.ErrNotFound) {
		return nil, err
	}
	wk, err := s.d.Client.FetchWellKnown(ctx, u)
	if err != nil {
		return nil, errUnreachable(err)
	}
	if s.isSelfWellKnown(wk) {
		return nil, ErrSelfPeer
	}
	if name == "" {
		name, _ = cleanPeerName(wk.Name)
	}
	np := &peer.Peer{URL: u, Name: name, Enabled: true, API: wk.API}
	if err := s.d.Peers.Create(ctx, np); err != nil {
		if errors.Is(err, peer.ErrExists) {
			return nil, ErrPeerExists
		}
		return nil, err
	}
	s.d.Audit.Record(ctx, p, "peer.add", audit.Target{}, map[string]any{"peer_id": np.ID, "url": u})
	return np, nil
}

// dropListings removes every listing imported from p and resets its
// cursor, so a later enable re-imports from scratch (and is due for the
// sync worker at once).
func (s *FederationService) dropListings(ctx context.Context, p *peer.Peer) (int64, error) {
	var n int64
	err := s.d.Tx.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if n, err = s.d.Networks.DeleteAllByOrigin(ctx, network.PeerOrigin(p.ID)); err != nil {
			return err
		}
		p.Cursor, p.ListingCount, p.LastFullSyncAt, p.LastSyncAt = 0, 0, nil, nil
		return s.d.Peers.Update(ctx, p, "cursor", "listing_count", "last_full_sync_at", "last_sync_at")
	})
	return n, err
}

func (s *FederationService) UpdatePeer(ctx context.Context, pr *principal.Principal, id string, u PeerUpdate) (*peer.Peer, error) {
	p, err := s.GetPeer(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.ManagedByConfigFile(p) {
		return nil, settings.ErrManagedByConfigFile
	}
	if u.Name != nil {
		name, err := cleanPeerName(*u.Name)
		if err != nil {
			return nil, err
		}
		if name != p.Name {
			p.Name = name
			if err := s.d.Peers.Update(ctx, p, "name"); err != nil {
				return nil, err
			}
			s.d.Audit.Record(ctx, pr, "peer.rename", audit.Target{}, map[string]any{"peer_id": p.ID, "name": name})
		}
	}
	if u.Enabled != nil && *u.Enabled != p.Enabled {
		p.Enabled = *u.Enabled
		if err := s.d.Peers.Update(ctx, p, "enabled"); err != nil {
			return nil, err
		}
		meta := map[string]any{"peer_id": p.ID, "url": p.URL, "builtin": p.Builtin}
		if p.Enabled {
			s.d.Audit.Record(ctx, pr, "peer.enable", audit.Target{}, meta)
		} else {
			removed, err := s.dropListings(ctx, p)
			if err != nil {
				return nil, err
			}
			meta["removed"] = removed
			s.d.Audit.Record(ctx, pr, "peer.disable", audit.Target{}, meta)
		}
	}
	return s.GetPeer(ctx, id)
}

func (s *FederationService) DeletePeer(ctx context.Context, pr *principal.Principal, id string) error {
	p, err := s.GetPeer(ctx, id)
	if err != nil {
		return err
	}
	if p.Builtin {
		return ErrBuiltinDelete
	}
	if s.ManagedByConfigFile(p) {
		return settings.ErrManagedByConfigFile
	}
	var removed int64
	err = s.d.Tx.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if removed, err = s.d.Networks.DeleteAllByOrigin(ctx, network.PeerOrigin(p.ID)); err != nil {
			return err
		}
		return s.d.Peers.Delete(ctx, p.ID)
	})
	if errors.Is(err, peer.ErrNotFound) {
		return ErrPeerNotFound
	}
	if err != nil {
		return err
	}
	s.d.Audit.Record(ctx, pr, "peer.delete", audit.Target{}, map[string]any{"peer_id": p.ID, "url": p.URL, "removed": removed})
	return nil
}

func peerLock(id string) string { return "federation-peer:" + id }

func (s *FederationService) SyncNow(ctx context.Context, pr *principal.Principal, id string) (*peer.Peer, *SyncResult, error) {
	p, err := s.GetPeer(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !s.Active(p) {
		return nil, nil, ErrPeerDisabled
	}
	// Bounded so the HTTP call answers; a sync cut short resumes from the
	// saved cursor on the next run.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), SyncNowBudget)
	defer cancel()
	var res *SyncResult
	ran, err := s.d.Locker.Run(sctx, peerLock(p.ID), func(ctx context.Context) error {
		var err error
		res, err = s.syncPeer(ctx, p)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	if !ran {
		return nil, nil, ErrSyncBusy
	}
	s.d.Audit.Record(ctx, pr, "peer.sync", audit.Target{}, map[string]any{"peer_id": p.ID, "result": res})
	p, err = s.GetPeer(ctx, id)
	return p, res, err
}

func (s *FederationService) SyncAll(ctx context.Context) error {
	list, err := s.d.Peers.List(ctx)
	if err != nil {
		return err
	}
	log := logger.FromCtx(ctx)
	every := s.d.Settings.Current().SyncEvery
	if every <= 0 {
		every = s.d.Config.SyncEvery
	}
	now := s.now()
	for _, p := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.Active(p) {
			// Listings of a disabled / switched-off peer never stay around
			// (covers include_main flipping to false).
			if n, err := s.d.Networks.CountByOrigin(ctx, network.PeerOrigin(p.ID)); err == nil && (n > 0 || p.ListingCount > 0) {
				if _, err := s.dropListings(ctx, p); err != nil {
					log.Warn().Err(err).Str("peer", p.ID).Msg("federation: removing listings of inactive peer")
				}
			}
			continue
		}
		// The worker polls often; each peer syncs once per sync_every (a
		// little slack so a poll landing just short of it doesn't wait a
		// whole extra round).
		if p.LastSyncAt != nil && now.Sub(*p.LastSyncAt) < every-SyncPoll/2 {
			continue
		}
		var res *SyncResult
		ran, err := s.d.Locker.Run(ctx, peerLock(p.ID), func(ctx context.Context) error {
			var err error
			res, err = s.syncPeer(ctx, p)
			return err
		})
		switch {
		case err != nil:
			log.Warn().Err(err).Str("peer", p.ID).Msg("federation: sync failed")
		case ran && res.Error != "":
			log.Warn().Str("peer", p.ID).Str("url", p.URL).Str("error", res.Error).Msg("federation: peer sync error")
		case ran:
			log.Info().Str("peer", p.ID).Int("created", res.Created).Int("updated", res.Updated).
				Int("deleted", res.Deleted).Int("verified", res.Verified).Msg("federation: peer synced")
		}
	}
	return nil
}

// ---- start-up ----

func (s *FederationService) EnsurePeers(ctx context.Context) error {
	log := logger.FromCtx(ctx)
	if err := s.ensureBuiltin(ctx); err != nil {
		return err
	}
	if f := s.d.Settings.File(); f != nil {
		if err := s.ApplyConfigFile(ctx); err != nil {
			return err
		}
		if f.PeersSet {
			// directory.peers in the file replaces DIRECTORY_PEERS.
			return nil
		}
	}
	// DIRECTORY_PEERS is an initial seed: each URL is added once, so a
	// peer the operator removes later stays removed.
	seeded, err := s.d.Settings.SeededPeers(ctx)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, u := range seeded {
		done[u] = true
	}
	var mark []string
	defer func() {
		if err := s.d.Settings.MarkSeeded(ctx, mark); err != nil {
			log.Warn().Err(err).Msg("DIRECTORY_PEERS: recording seeded peers")
		}
	}()
	for _, raw := range config.SplitList(s.d.Config.Peers) {
		u, err := NormalizeURL(raw, s.d.Strict)
		if err != nil {
			log.Warn().Str("url", raw).Err(err).Msg("DIRECTORY_PEERS: skipping invalid peer URL")
			continue
		}
		if s.isSelfURL(u) || u == s.mainURL() || done[u] {
			continue
		}
		done[u] = true
		if _, err := s.d.Peers.GetByURL(ctx, u); err == nil {
			mark = append(mark, u)
			continue
		} else if !errors.Is(err, peer.ErrNotFound) {
			return err
		}
		if err := s.d.Peers.Create(ctx, &peer.Peer{URL: u, Enabled: true}); err != nil && !errors.Is(err, peer.ErrExists) {
			return err
		}
		mark = append(mark, u)
		s.d.Audit.System(ctx, "federation", "peer.seed", audit.Target{}, map[string]any{"url": u})
	}
	return nil
}

func (s *FederationService) ManagedByConfigFile(p *peer.Peer) bool {
	f := s.d.Settings.File()
	if f == nil {
		return false
	}
	if p.Builtin {
		return f.Has(settings.FieldIncludeMain)
	}
	return p.ConfigManaged
}

func (s *FederationService) ApplyConfigFile(ctx context.Context) error {
	f := s.d.Settings.File()
	if err := s.ensureBuiltin(ctx); err != nil {
		return err
	}
	if f == nil {
		return nil
	}
	if f.IncludeMain != nil && *f.IncludeMain && s.includeMain() {
		if p, err := s.MainPeer(ctx); err != nil {
			return err
		} else if p != nil && !p.Enabled {
			p.Enabled = true
			if err := s.d.Peers.Update(ctx, p, "enabled"); err != nil {
				return err
			}
			s.d.Audit.System(ctx, ConfigActor, "peer.enable", audit.Target{}, map[string]any{"peer_id": p.ID, "url": p.URL, "builtin": true})
		}
	}
	return s.reconcileFilePeers(ctx, f.Peers)
}

func (s *FederationService) reconcileFilePeers(ctx context.Context, urls []string) error {
	log := logger.FromCtx(ctx)
	all, err := s.d.Peers.List(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, u := range urls {
		want[u] = true
	}
	byURL := map[string]*peer.Peer{}
	for _, p := range all {
		byURL[p.URL] = p
		if p.Builtin || !p.ConfigManaged || want[p.URL] {
			continue
		}
		// Dropped from the file: disable, keep the row and its state.
		wasEnabled := p.Enabled
		p.Enabled, p.ConfigManaged = false, false
		if err := s.d.Peers.Update(ctx, p, "enabled", "config_managed"); err != nil {
			return err
		}
		meta := map[string]any{"peer_id": p.ID, "url": p.URL, "builtin": false}
		if wasEnabled {
			removed, err := s.dropListings(ctx, p)
			if err != nil {
				return err
			}
			meta["removed"] = removed
		}
		s.d.Audit.System(ctx, ConfigActor, "peer.disable", audit.Target{}, meta)
	}
	for _, u := range urls {
		if s.isSelfURL(u) {
			log.Warn().Str("url", u).Msg("config file: directory.peers lists this directory itself; skipping")
			continue
		}
		p := byURL[u]
		switch {
		case p == nil:
			np := &peer.Peer{URL: u, Enabled: true, ConfigManaged: true}
			if err := s.d.Peers.Create(ctx, np); err != nil {
				if errors.Is(err, peer.ErrExists) {
					continue // another replica created it
				}
				return err
			}
			s.d.Audit.System(ctx, ConfigActor, "peer.add", audit.Target{}, map[string]any{"peer_id": np.ID, "url": u})
		case p.Builtin:
			log.Warn().Str("url", u).Msg("config file: directory.peers lists the main directory; include_main controls it")
		case !p.Enabled || !p.ConfigManaged:
			wasEnabled := p.Enabled
			p.Enabled, p.ConfigManaged = true, true
			if err := s.d.Peers.Update(ctx, p, "enabled", "config_managed"); err != nil {
				return err
			}
			if !wasEnabled {
				s.d.Audit.System(ctx, ConfigActor, "peer.enable", audit.Target{}, map[string]any{"peer_id": p.ID, "url": p.URL, "builtin": false})
			}
		}
	}
	return nil
}

func (s *FederationService) ensureBuiltin(ctx context.Context) error {
	cur, err := s.d.Peers.GetBuiltin(ctx)
	if err != nil && !errors.Is(err, peer.ErrNotFound) {
		return err
	}
	if errors.Is(err, peer.ErrNotFound) {
		cur = nil
	}
	if !s.includeMain() {
		// Off by configuration: keep the row (and the operator's choice)
		// for when it is turned back on, but import nothing.
		if cur != nil {
			if n, err := s.dropListings(ctx, cur); err != nil {
				return err
			} else if n > 0 {
				s.d.Audit.System(ctx, "federation", "peer.builtin_off", audit.Target{}, map[string]any{"peer_id": cur.ID, "removed": n})
			}
		}
		return nil
	}
	main := s.mainURL()
	switch {
	case cur == nil:
		if ex, err := s.d.Peers.GetByURL(ctx, main); err == nil {
			// The operator added it by hand before: make that row the builtin.
			ex.Builtin = true
			return s.d.Peers.Update(ctx, ex, "builtin")
		} else if !errors.Is(err, peer.ErrNotFound) {
			return err
		}
		p := &peer.Peer{URL: main, Name: "boson directory", Enabled: true, Builtin: true}
		if err := s.d.Peers.Create(ctx, p); err != nil && !errors.Is(err, peer.ErrExists) {
			return err
		}
		s.d.Audit.System(ctx, "federation", "peer.builtin_create", audit.Target{}, map[string]any{"peer_id": p.ID, "url": main})
	case cur.URL != main:
		// main_peer_url changed: re-point the builtin, keeping its
		// enabled flag; listings of the old main directory go.
		if ex, err := s.d.Peers.GetByURL(ctx, main); err == nil {
			if _, err := s.dropListings(ctx, ex); err != nil {
				return err
			}
			if err := s.d.Peers.Delete(ctx, ex.ID); err != nil {
				return err
			}
		}
		if _, err := s.dropListings(ctx, cur); err != nil {
			return err
		}
		old := cur.URL
		cur.URL, cur.API, cur.LastError = main, "", nil
		if err := s.d.Peers.Update(ctx, cur, "url", "api", "last_error"); err != nil {
			return err
		}
		s.d.Audit.System(ctx, "federation", "peer.builtin_repoint", audit.Target{}, map[string]any{"peer_id": cur.ID, "from": old, "to": main})
	}
	return nil
}

// ---- sync ----

const maxProblems = 10

func (r *SyncResult) problem(msg string) {
	if len(r.Problems) < maxProblems {
		r.Problems = append(r.Problems, msg)
	}
}

// syncPeer pulls p's feed and records the outcome on the peer row. Peer
// failures end up in res.Error / last_error; only database errors are
// returned.
func (s *FederationService) syncPeer(ctx context.Context, p *peer.Peer) (*SyncResult, error) {
	res := &SyncResult{}
	walkCtx, cancel := context.WithTimeout(ctx, s.d.Config.SyncTimeout)
	defer cancel()
	toVerify, walkErr := s.walk(walkCtx, p, res)
	var pe *PeerError
	if walkErr != nil && !errors.As(walkErr, &pe) && walkCtx.Err() == nil {
		return res, walkErr
	}
	if walkErr != nil {
		msg := walkErr.Error()
		if walkCtx.Err() != nil && !errors.As(walkErr, &pe) {
			msg = "sync timed out; it continues from the saved cursor next time"
		}
		res.Error = msg
	}
	if len(toVerify) > 0 && ctx.Err() == nil {
		vctx, vcancel := context.WithTimeout(ctx, s.d.Config.SyncTimeout)
		res.Verified = s.verifyImported(vctx, p, toVerify)
		vcancel()
	}

	now := s.now()
	p.LastSyncAt = &now
	p.LastError = nil
	if res.Error != "" {
		msg := res.Error
		if len(msg) > 500 {
			msg = msg[:500]
		}
		p.LastError = &msg
	}
	n, err := s.d.Networks.CountByOrigin(ctx, network.PeerOrigin(p.ID))
	if err != nil {
		return res, err
	}
	p.ListingCount = int(n)
	if err := s.d.Peers.Update(context.WithoutCancel(ctx), p, "cursor", "last_sync_at", "last_error", "listing_count", "last_full_sync_at", "api", "name"); err != nil {
		return res, err
	}
	return res, nil
}

// walk applies the peer's feed from its cursor (from zero for a full
// resync) and returns the listings that need a DNS check.
func (s *FederationService) walk(ctx context.Context, p *peer.Peer, res *SyncResult) ([]*network.Network, error) {
	origin := network.PeerOrigin(p.ID)
	cfg := s.d.Config
	wk, err := s.d.Client.FetchWellKnown(ctx, p.URL)
	if err != nil {
		return nil, err
	}
	if s.isSelfWellKnown(wk) {
		return nil, peerErr("this peer is this directory itself; nothing imported")
	}
	p.API = wk.API
	if p.Name == "" {
		p.Name, _ = cleanPeerName(wk.Name)
	}

	now := s.now()
	full := p.Cursor == 0 || p.LastFullSyncAt == nil || now.Sub(*p.LastFullSyncAt) >= cfg.ResyncEvery
	ids, err := s.d.Peers.OriginIDs(ctx, origin)
	if err != nil {
		return nil, err
	}
	existing := make(map[string]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	var seen map[string]bool
	since := p.Cursor
	startFull := func() {
		full, since, seen = true, 0, map[string]bool{}
	}
	if full {
		startFull()
	}
	res.Full = full

	var toVerify []*network.Network
	limitHit := false
	maxPages := cfg.MaxListingsPerPeer/DefaultFeedLimit*4 + 20
	complete := false
	for res.Pages < maxPages {
		if err := ctx.Err(); err != nil {
			return toVerify, err
		}
		page, err := s.d.Client.FetchFeed(ctx, p.URL, since, DefaultFeedLimit)
		if errors.Is(err, errCursorRegressed) {
			if since == 0 {
				return toVerify, peerErr("feed next_since went backwards")
			}
			startFull()
			res.Full = true
			continue
		}
		if err != nil {
			return toVerify, err
		}
		res.Pages++
		for _, c := range page.Changes {
			v, hit, err := s.apply(ctx, origin, c, existing, seen, res)
			if err != nil {
				return toVerify, err
			}
			limitHit = limitHit || hit
			if v != nil {
				toVerify = append(toVerify, v)
			}
		}
		since = page.NextSince
		p.Cursor = since
		if err := s.d.Peers.Update(ctx, p, "cursor"); err != nil {
			return toVerify, err
		}
		if !page.HasMore {
			complete = true
			break
		}
	}
	if full && complete {
		// Whatever the peer no longer exports goes.
		for id := range existing {
			if seen[id] {
				continue
			}
			if ok, err := s.d.Networks.DeleteByOrigin(ctx, origin, id); err != nil {
				return toVerify, err
			} else if ok {
				res.Deleted++
			}
		}
		p.LastFullSyncAt = &now
	}
	if limitHit {
		return toVerify, peerErr("listing limit reached (DIRECTORY_PEER_MAX_LISTINGS=%d); %d new listings skipped", cfg.MaxListingsPerPeer, res.Skipped)
	}
	if !complete {
		return toVerify, peerErr("feed not finished after %d pages; continuing next sync", res.Pages)
	}
	return toVerify, nil
}

// apply imports one feed change. It returns the row when it needs a DNS
// check, and whether the per-peer listing limit stopped it.
func (s *FederationService) apply(ctx context.Context, origin string, c FeedChange, existing, seen map[string]bool, res *SyncResult) (*network.Network, bool, error) {
	id := c.Network.ID
	if c.Deleted {
		if !originIDRe.MatchString(id) {
			res.Rejected++
			return nil, false, nil
		}
		if existing[id] {
			if _, err := s.d.Networks.DeleteByOrigin(ctx, origin, id); err != nil {
				return nil, false, err
			}
			delete(existing, id)
			res.Deleted++
		}
		return nil, false, nil
	}
	n, err := importedNetwork(origin, c, s.d.Strict)
	if err != nil {
		res.Rejected++
		res.problem(err.Error())
		if originIDRe.MatchString(id) && existing[id] {
			// Don't keep serving an old copy of something now invalid.
			if _, err := s.d.Networks.DeleteByOrigin(ctx, origin, id); err != nil {
				return nil, false, err
			}
			delete(existing, id)
			res.Deleted++
		}
		return nil, false, nil
	}
	if n.Theme, err = importedTheme(c.Network.Theme, s.d.Strict); err != nil {
		// A bad theme doesn't cost the listing; it shows without one.
		res.problem(fmt.Sprintf("%s: theme dropped: %v", trunc(id), err))
		n.Theme = nil
	}
	for _, p := range importedChannels(n, c.Network) {
		res.problem(trunc(id) + ": " + p)
	}
	for _, p := range importedPreviousHostnames(n, c.Network) {
		res.problem(trunc(id) + ": " + p)
	}
	if seen != nil {
		seen[id] = true
	}
	if b, err := s.d.Blocks.Match(ctx, n.Hostname); err != nil {
		return nil, false, err
	} else if b != nil {
		res.Blocked++
		if existing[id] {
			if _, err := s.d.Networks.DeleteByOrigin(ctx, origin, id); err != nil {
				return nil, false, err
			}
			delete(existing, id)
		}
		return nil, false, nil
	}
	if !existing[id] && len(existing) >= s.d.Config.MaxListingsPerPeer {
		res.Skipped++
		return nil, true, nil
	}
	r, err := s.d.Networks.UpsertByOrigin(ctx, n)
	if err != nil {
		return nil, false, err
	}
	switch r {
	case network.UpsertCreated:
		existing[id] = true
		res.Created++
	case network.UpsertUpdated:
		res.Updated++
	case network.UpsertUnchanged:
		res.Unchanged++
		return nil, false, nil
	case network.UpsertConflict:
		res.Conflicts++
		return nil, false, nil
	}
	if n.Status == network.StatusPending {
		return n, false, nil
	}
	return nil, false, nil
}

// verifyImported runs this directory's own DNS check (lenient: 2 of 3
// resolvers) on freshly imported pending listings. Listings it doesn't
// reach stay pending for the verify worker.
func (s *FederationService) verifyImported(ctx context.Context, p *peer.Peer, list []*network.Network) int {
	conc := max(s.d.Config.VerifyConcurrency, 1)
	sem := make(chan struct{}, conc)
	var mu sync.Mutex
	verified := 0
	var wg sync.WaitGroup
	for _, n := range list {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(n *network.Network) {
			defer func() { <-sem; wg.Done() }()
			report, err := s.d.Verifier.Verify(ctx, n.Hostname, n.VerificationToken, dnsverify.ModeLenient)
			if err != nil {
				return
			}
			now := s.now()
			status, verifiedAt := networks.Transition(n, report.Success, now, s.d.LapseAfter)
			ok, err := s.d.Networks.RecordVerify(ctx, network.VerifyResult{
				ID: n.ID, CheckedToken: n.VerificationToken, Status: status, CheckedAt: now, VerifiedAt: verifiedAt,
			})
			if err != nil || !ok {
				return
			}
			if status != n.Status {
				s.d.Audit.System(ctx, "federation", "network.status."+status, audit.Target{NetworkID: n.ID, Hostname: n.Hostname},
					map[string]any{"from": n.Status, "peer_id": p.ID})
			}
			if status == network.StatusVerified {
				mu.Lock()
				verified++
				mu.Unlock()
			}
		}(n)
	}
	wg.Wait()
	return verified
}
