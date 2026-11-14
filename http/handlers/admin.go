package handlers

import (
	"encoding/base64"
	"encoding/json"
	nethttp "net/http"
	"strconv"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/http/views"
	"github.com/boson-chat/directory/internal/apperr"
	auditrepo "github.com/boson-chat/directory/internal/db/repositories/audit"
	blockrepo "github.com/boson-chat/directory/internal/db/repositories/block"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/networks"
)

// Operator API: needs directory.manage in the access token's perms.
func (h *Handlers) registerAdmin(r *router.Router) {
	r.Operator("GET /v1/admin/networks", h.adminList)
	r.Operator("POST /v1/admin/networks/{id}/block", h.moderate(networks.ModerateBlock))
	r.Operator("POST /v1/admin/networks/{id}/unblock", h.moderate(networks.ModerateUnblock))
	r.Operator("POST /v1/admin/networks/{id}/recheck", h.moderate(networks.ModerateRecheck))
	r.Operator("DELETE /v1/admin/networks/{id}", h.adminDelete)
	r.Operator("GET /v1/admin/blocks", h.listBlocks)
	r.Operator("POST /v1/admin/blocks", h.addBlock)
	r.Operator("DELETE /v1/admin/blocks/{id}", h.removeBlock)
	r.Operator("GET /v1/admin/audit", h.listAudit)
}

// GET /v1/admin/networks?status=&q=&source=&sort=&limit=&cursor= — every
// status and origin (status narrows), NSFW included.
func (h *Handlers) adminList(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	sort := network.Sort(q.Get("sort"))
	if sort == "" {
		sort = network.SortNewest
	}
	if !network.ValidSort(sort) {
		router.WriteError(w, r, apperr.Invalid("sort must be users, active, newest or name"))
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	f := network.ListFilter{
		Query: q.Get("q"), Language: q.Get("lang"), Tag: q.Get("tag"), IncludeNSFW: true,
		Source: networks.SourceOrigin(firstNonEmpty(q.Get("source"), q.Get("origin"))), Sort: sort, Limit: limit + 1, AnyStatus: true,
	}
	if s := q.Get("status"); s != "" {
		f.AnyStatus, f.Statuses = false, []string{s}
	}
	if c := q.Get("cursor"); c != "" {
		cur, err := decodeAdminCursor(c)
		if err != nil || cur.Sort != sort {
			router.WriteError(w, r, networks.ErrBadCursor)
			return
		}
		f.After = cur
	}
	rows, err := h.d.NetworkRepo.List(r.Context(), f)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	total, err := h.d.NetworkRepo.Count(r.Context(), f)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	page := &networks.Page{Networks: rows, TotalEstimate: total}
	if len(rows) > limit {
		page.Networks = rows[:limit]
		last := page.Networks[limit-1]
		page.NextCursor = encodeAdminCursor(network.Cursor{Sort: sort, Keys: network.KeysOf(last, sort), ID: last.ID})
	}
	router.WriteJSON(w, nethttp.StatusOK, h.pageJSON(r, page, true))
}

func encodeAdminCursor(c network.Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAdminCursor(s string) (*network.Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var c network.Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// DELETE /v1/admin/networks/{id}: any listing, imported ones included.
func (h *Handlers) adminDelete(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.d.Networks.Delete(r.Context(), principal.From(r.Context()), r.PathValue("id")); err != nil {
		router.WriteError(w, r, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

type moderateBody struct {
	Reason string `json:"reason"`
}

type moderateJSON struct {
	views.Listing
	Report any `json:"report,omitempty"`
}

func (h *Handlers) moderate(action networks.ModerationAction) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var body moderateBody
		if err := router.DecodeOptional(w, r, &body); err != nil {
			router.WriteError(w, r, err)
			return
		}
		n, report, err := h.d.Networks.Moderate(r.Context(), principal.From(r.Context()), r.PathValue("id"), action, body.Reason)
		if err != nil {
			router.WriteError(w, r, err)
			return
		}
		out := moderateJSON{Listing: h.listing(r, n)}
		if report != nil {
			out.Report = report
		}
		router.WriteJSON(w, nethttp.StatusOK, out)
	}
}

type blockJSON struct {
	ID        string    `json:"id"`
	Pattern   string    `json:"pattern"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func toBlock(b *blockrepo.Block) blockJSON {
	return blockJSON{ID: b.ID, Pattern: b.Pattern, Reason: b.Reason, CreatedBy: b.CreatedBy, CreatedAt: b.CreatedAt}
}

func (h *Handlers) listBlocks(w nethttp.ResponseWriter, r *nethttp.Request) {
	list, err := h.d.Blocks.List(r.Context())
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	out := make([]blockJSON, 0, len(list))
	for _, b := range list {
		out = append(out, toBlock(b))
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"blocks": out})
}

func (h *Handlers) addBlock(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body struct {
		Pattern string `json:"pattern"`
		Reason  string `json:"reason"`
	}
	if err := router.Decode(w, r, &body); err != nil {
		router.WriteError(w, r, err)
		return
	}
	b, changed, err := h.d.Blocks.Add(r.Context(), principal.From(r.Context()), body.Pattern, body.Reason)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	if changed == nil {
		changed = []string{}
	}
	router.WriteJSON(w, nethttp.StatusCreated, map[string]any{"block": toBlock(b), "blocked_network_ids": changed})
}

func (h *Handlers) removeBlock(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.d.Blocks.Remove(r.Context(), principal.From(r.Context()), r.PathValue("id")); err != nil {
		router.WriteError(w, r, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

type auditJSON struct {
	ID        string         `json:"id"`
	At        time.Time      `json:"at"`
	ActorKind string         `json:"actor_kind"`
	ActorID   string         `json:"actor_id"`
	ActorName string         `json:"actor_name,omitempty"`
	Action    string         `json:"action"`
	NetworkID *string        `json:"network_id,omitempty"`
	Hostname  *string        `json:"hostname,omitempty"`
	IP        string         `json:"ip,omitempty"`
	Meta      map[string]any `json:"meta"`
	// Actor and Target group the fields above (web UI shape).
	Actor  auditActor  `json:"actor"`
	Target auditTarget `json:"target"`
}

type auditActor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type auditTarget struct {
	NetworkID *string `json:"network_id,omitempty"`
	Hostname  *string `json:"hostname,omitempty"`
}

// GET /v1/admin/audit?network_id=&action=&limit=&cursor=
func (h *Handlers) listAudit(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	f := auditrepo.Filter{NetworkID: q.Get("network_id"), Action: q.Get("action"), Limit: limit + 1}
	if c := q.Get("cursor"); c != "" {
		b, err := base64.RawURLEncoding.DecodeString(c)
		var cur auditrepo.Cursor
		if err != nil || json.Unmarshal(b, &cur) != nil || cur.ID == "" {
			router.WriteError(w, r, networks.ErrBadCursor)
			return
		}
		f.After = &cur
	}
	list, err := h.d.Audit.List(r.Context(), f)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	var next *string
	if len(list) > limit {
		list = list[:limit]
		last := list[limit-1]
		b, _ := json.Marshal(auditrepo.Cursor{At: last.At, ID: last.ID})
		s := base64.RawURLEncoding.EncodeToString(b)
		next = &s
	}
	out := make([]auditJSON, 0, len(list))
	for _, e := range list {
		meta := map[string]any(e.Meta)
		if meta == nil {
			meta = map[string]any{}
		}
		out = append(out, auditJSON{ID: e.ID, At: e.At, ActorKind: e.ActorKind, ActorID: e.ActorID, ActorName: e.ActorName,
			Action: e.Action, NetworkID: e.NetworkID, Hostname: e.Hostname, IP: e.IP, Meta: meta,
			Actor:  auditActor{Kind: e.ActorKind, ID: e.ActorID, Name: e.ActorName},
			Target: auditTarget{NetworkID: e.NetworkID, Hostname: e.Hostname}})
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"entries": out, "next_cursor": next})
}
