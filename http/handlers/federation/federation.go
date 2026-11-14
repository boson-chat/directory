// Package federation mounts the federation module: the public change feed
// (GET /v1/feed), the operator peer API (/v1/admin/peers…), the sync
// worker and the built-in main peer at start-up.
package federation

import (
	"context"
	nethttp "net/http"
	"strconv"
	"sync"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/peer"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/federation"
	"github.com/boson-chat/directory/internal/services/workers"
)

func init() {
	router.Register(router.Module{
		Name: "federation",
		Routes: func(d *router.Deps, r *router.Router) error {
			h := &handler{svc: serviceFor(d)}
			r.Public("GET /v1/feed", h.feed)
			r.Operator("GET /v1/admin/peers", h.listPeers)
			r.Operator("POST /v1/admin/peers", h.addPeer)
			r.Operator("GET /v1/admin/peers/check", h.checkPeer)
			r.Operator("GET /v1/admin/peers/{id}", h.getPeer)
			r.Operator("PATCH /v1/admin/peers/{id}", h.updatePeer)
			r.Operator("DELETE /v1/admin/peers/{id}", h.deletePeer)
			r.Operator("POST /v1/admin/peers/{id}/sync", h.syncPeer)
			return nil
		},
		Workers: func(d *router.Deps) []workers.Worker {
			if !d.Config.FederationConfig.SyncEnabled {
				return nil
			}
			svc := serviceFor(d)
			// Poll for due peers; the sync_every setting spaces each peer's syncs.
			every := min(d.Config.FederationConfig.SyncEvery, federation.SyncPoll)
			return []workers.Worker{{Name: federation.WorkerName, Every: every, Run: svc.SyncAll}}
		},
		Start: func(ctx context.Context, d *router.Deps) error {
			return serviceFor(d).EnsurePeers(ctx)
		},
		// The stack config file changed: builtin + directory.peers.
		ConfigReloaded: func(ctx context.Context, d *router.Deps) error {
			return serviceFor(d).ApplyConfigFile(ctx)
		},
	})
}

var (
	svcMu sync.Mutex
	svcs  = map[*router.Deps]federation.FederationServiceImpl{}
)

// Service returns the federation service of the app wired with d (for
// other modules, e.g. the core facets handler listing peer sources).
func Service(d *router.Deps) federation.FederationServiceImpl { return serviceFor(d) }

// serviceFor builds the service once per wired app (Routes, Workers and
// Start share it).
func serviceFor(d *router.Deps) federation.FederationServiceImpl {
	svcMu.Lock()
	defer svcMu.Unlock()
	if s, ok := svcs[d]; ok {
		return s
	}
	fc := d.Config.FederationConfig
	strict := d.Config.AppConfig.IsProduction()
	s := federation.NewFederationService(federation.Deps{
		Peers:    peer.NewPeerRepository(d.DB),
		Networks: d.NetworkRepo,
		Blocks:   d.Blocks,
		Verifier: d.Verifier,
		Audit:    d.Audit,
		Locker:   d.Locker,
		Tx:       d.DB,
		Client: federation.NewClient(federation.ClientOptions{
			Timeout: fc.PeerTimeout, MaxBytes: fc.MaxResponseBytes, Strict: strict,
		}),
		MediaURL:   d.Media.URLFor,
		Config:     fc,
		Settings:   d.Settings,
		PublicURL:  d.Config.HTTPConfig.PublicURL,
		Strict:     strict,
		LapseAfter: d.Config.VerifyConfig.LapseAfter,
		Now:        d.Now,
	})
	svcs[d] = s
	return s
}

type handler struct {
	svc federation.FederationServiceImpl
}

func (h *handler) feed(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	var since int64
	if v := q.Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			router.WriteError(w, r, federation.ErrBadSince)
			return
		}
		since = n
	}
	limit := federation.DefaultFeedLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > federation.MaxFeedLimit {
			router.WriteError(w, r, apperr.Invalid("limit must be between 1 and 1000"))
			return
		}
		limit = n
	}
	page, err := h.svc.Feed(r.Context(), since, limit)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	router.WriteJSON(w, nethttp.StatusOK, page)
}

// peerJSON is a peer as the operator API returns it.
type peerJSON struct {
	ID             string     `json:"id"`
	URL            string     `json:"url"`
	Name           string     `json:"name"`
	API            string     `json:"api,omitempty"`
	Enabled        bool       `json:"enabled"`
	Builtin        bool       `json:"builtin"`
	Active         bool       `json:"active"`
	Cursor         int64      `json:"cursor"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastFullSyncAt *time.Time `json:"last_full_sync_at"`
	LastError      *string    `json:"last_error"`
	ListingCount   int        `json:"listing_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// ManagedByConfigFile: the stack config file owns this peer (listed in
	// directory.peers, or the builtin while the file sets include_main);
	// PATCH / DELETE answer 409 managed_by_config_file.
	ManagedByConfigFile bool `json:"managed_by_config_file"`
}

// PeerJSON is p as the operator API returns it (for other modules).
func PeerJSON(svc federation.FederationServiceImpl, p *peer.Peer) any {
	return (&handler{svc: svc}).toJSON(p)
}

func (h *handler) toJSON(p *peer.Peer) peerJSON {
	return peerJSON{
		ID: p.ID, URL: p.URL, Name: p.Name, API: p.API, Enabled: p.Enabled, Builtin: p.Builtin,
		Active: h.svc.Active(p), Cursor: p.Cursor, LastSyncAt: p.LastSyncAt, LastFullSyncAt: p.LastFullSyncAt,
		LastError: p.LastError, ListingCount: p.ListingCount, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		ManagedByConfigFile: h.svc.ManagedByConfigFile(p),
	}
}

func (h *handler) listPeers(w nethttp.ResponseWriter, r *nethttp.Request) {
	list, err := h.svc.ListPeers(r.Context())
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	out := make([]peerJSON, 0, len(list))
	for _, p := range list {
		out = append(out, h.toJSON(p))
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"peers": out})
}

func (h *handler) getPeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, err := h.svc.GetPeer(r.Context(), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"peer": h.toJSON(p)})
}

func (h *handler) checkPeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	u, wk, err := h.svc.CheckPeer(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"url": u, "well_known": wk})
}

func (h *handler) addPeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := router.Decode(w, r, &body); err != nil {
		router.WriteError(w, r, err)
		return
	}
	p, err := h.svc.AddPeer(r.Context(), principal.From(r.Context()), body.URL, body.Name)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusCreated, map[string]any{"peer": h.toJSON(p)})
}

func (h *handler) updatePeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body struct {
		Enabled *bool   `json:"enabled"`
		Name    *string `json:"name"`
	}
	if err := router.Decode(w, r, &body); err != nil {
		router.WriteError(w, r, err)
		return
	}
	p, err := h.svc.UpdatePeer(r.Context(), principal.From(r.Context()), r.PathValue("id"),
		federation.PeerUpdate{Enabled: body.Enabled, Name: body.Name})
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"peer": h.toJSON(p)})
}

func (h *handler) deletePeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.svc.DeletePeer(r.Context(), principal.From(r.Context()), r.PathValue("id")); err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusNoContent, nil)
}

func (h *handler) syncPeer(w nethttp.ResponseWriter, r *nethttp.Request) {
	p, res, err := h.svc.SyncNow(r.Context(), principal.From(r.Context()), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"peer": h.toJSON(p), "result": res})
}
