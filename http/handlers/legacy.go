package handlers

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/google/uuid"
)

// Legacy /servers API: the exact shapes of the backend's
// http/handlers/servers.go, mapped onto networks, so client 0.45, the
// website and old links keep working (Traefik routes api.$DOMAIN
// PathPrefix(/servers) here). Errors are {"error": "<message>"} as before.
// Fixed on the way: GET /servers/{id} no longer shows unverified rows to
// strangers, hostnames are normalised and unique.
func (h *Handlers) registerLegacy(r *router.Router) {
	r.Public("GET /servers", h.legacyList)
	r.Authed("GET /servers/me", h.legacyMine)
	r.Public("GET /servers/{id}", h.legacyGet)
	r.Authed("POST /servers", h.writes(h.legacyCreate))
	r.Authed("POST /servers/{id}/verify", h.writes(h.legacyVerify))
	r.Authed("POST /servers/{id}/regenerate-token", h.writes(h.legacyRegenerate))
	r.Authed("PATCH /servers/{id}", h.writes(h.legacyUpdate))
	for _, kind := range []networks.ImageKind{networks.ImageIcon, networks.ImageBanner} {
		r.Authed("POST /servers/{id}/"+string(kind), h.writes(h.legacyImage(kind, true)))
		r.Authed("DELETE /servers/{id}/"+string(kind), h.writes(h.legacyImage(kind, false)))
	}
}

// LegacyServer is the backend's server JSON.
type LegacyServer struct {
	ID                        string     `json:"id"`
	Hostname                  string     `json:"hostname"`
	Port                      int        `json:"port"`
	TLS                       bool       `json:"tls"`
	Name                      string     `json:"name"`
	Description               *string    `json:"description,omitempty"`
	Tags                      []string   `json:"tags"`
	Languages                 []string   `json:"languages"`
	IsNSFW                    bool       `json:"is_nsfw"`
	IsFeatured                bool       `json:"is_featured"`
	VerificationStatus        string     `json:"verification_status"`
	VerificationLastCheckedAt *time.Time `json:"verification_last_checked_at,omitempty"`
	HealthStatus              string     `json:"health_status"`
	HealthLastCheckedAt       *time.Time `json:"health_last_checked_at,omitempty"`
	UserCount                 *int       `json:"user_count,omitempty"`
	UserCountUpdatedAt        *time.Time `json:"user_count_updated_at,omitempty"`
	RegisteredAt              time.Time  `json:"registered_at"`
	IconURL                   string     `json:"icon_url,omitempty"`
	BannerURL                 string     `json:"banner_url,omitempty"`
}

// LegacyServerWithToken is the owner view of a pending row.
type LegacyServerWithToken struct {
	LegacyServer
	VerificationToken         string    `json:"verification_token"`
	VerificationTokenIssuedAt time.Time `json:"verification_token_issued_at"`
}

// LegacyID is the id a network has on the legacy API: the backend's uuid
// for imported rows, else its net_ id.
func LegacyID(n *network.Network) string {
	if n.LegacyID != nil {
		return n.LegacyID.String()
	}
	return n.ID
}

func (h *Handlers) legacyServer(n *network.Network) LegacyServer {
	s := LegacyServer{
		ID: LegacyID(n), Hostname: n.Hostname, Port: n.Port, TLS: n.TLS, Name: n.Name,
		Tags: nonNil(n.Tags), Languages: nonNil(n.Languages), IsNSFW: n.IsNSFW,
		VerificationStatus: n.Status, VerificationLastCheckedAt: n.LastCheckedAt,
		HealthStatus: n.HealthStatus, HealthLastCheckedAt: n.HealthCheckedAt,
		UserCount: n.UserCount, UserCountUpdatedAt: n.UserCountAt, RegisteredAt: n.CreatedAt,
	}
	if n.Description != "" {
		d := n.Description
		s.Description = &d
	}
	if n.IconKey != nil {
		s.IconURL = h.d.Media.URLFor(*n.IconKey)
	}
	if n.BannerKey != nil {
		s.BannerURL = h.d.Media.URLFor(*n.BannerKey)
	}
	return s
}

// legacyOwnerView adds the token while the row is pending (ToOwnerView).
func (h *Handlers) legacyOwnerView(n *network.Network) any {
	s := h.legacyServer(n)
	if n.Status != network.StatusPending {
		return s
	}
	return LegacyServerWithToken{LegacyServer: s, VerificationToken: n.VerificationToken, VerificationTokenIssuedAt: n.TokenIssuedAt}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func legacyError(w nethttp.ResponseWriter, status int, msg string) {
	router.WriteJSON(w, status, map[string]string{"error": msg})
}

// legacyFail maps service errors onto the old status codes and messages.
func legacyFail(w nethttp.ResponseWriter, r *nethttp.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		router.WriteError(w, r, err) // logs; still {"error": ...}
		return
	}
	msg := e.Message
	switch e {
	case networks.ErrNotFound:
		msg = "not found"
	case networks.ErrNotOwner, networks.ErrNotLocal:
		msg = "not the owner"
	case router.ErrUnauthenticated, networks.ErrUnauthenticate:
		msg = "missing bearer token"
	}
	legacyError(w, e.Status, msg)
}

// resolveLegacyID maps a legacy uuid to the network id.
func (h *Handlers) resolveLegacyID(r *nethttp.Request) (string, error) {
	raw := r.PathValue("id")
	if u, err := uuid.Parse(raw); err == nil {
		n, err := h.d.NetworkRepo.GetByLegacyID(r.Context(), u)
		if errors.Is(err, network.ErrNotFound) {
			return "", networks.ErrNotFound
		}
		if err != nil {
			return "", err
		}
		return n.ID, nil
	}
	if strings.HasPrefix(raw, "net_") {
		return raw, nil
	}
	return "", apperr.New(nethttp.StatusBadRequest, "invalid_id", "invalid id")
}

func (h *Handlers) legacyList(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	sort := network.Sort(q.Get("sort"))
	if !network.ValidSort(sort) {
		sort = network.SortUsers
	}
	rows, err := h.d.NetworkRepo.List(r.Context(), network.ListFilter{
		Query: q.Get("q"), Language: q.Get("lang"), IncludeNSFW: q.Get("nsfw") == "true",
		Sort: sort, Limit: limit, Offset: max(offset, 0),
	})
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	out := make([]LegacyServer, 0, len(rows))
	for _, n := range rows {
		out = append(out, h.legacyServer(n))
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"servers": out, "count": len(out)})
}

func (h *Handlers) legacyMine(w nethttp.ResponseWriter, r *nethttp.Request) {
	list, err := h.d.Networks.Mine(r.Context(), principal.From(r.Context()))
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	out := make([]any, 0, len(list))
	for _, n := range list {
		out = append(out, h.legacyOwnerView(n))
	}
	router.WriteJSON(w, nethttp.StatusOK, map[string]any{"servers": out, "count": len(out)})
}

func (h *Handlers) legacyGet(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := h.resolveLegacyID(r)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	n, err := h.d.Networks.Get(r.Context(), principal.From(r.Context()), id)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.legacyServer(n))
}

type legacyCreateBody struct {
	Hostname    string   `json:"hostname"`
	Port        int      `json:"port"`
	TLS         *bool    `json:"tls,omitempty"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	IsNSFW      bool     `json:"is_nsfw,omitempty"`
}

func (h *Handlers) legacyCreate(w nethttp.ResponseWriter, r *nethttp.Request) {
	var b legacyCreateBody
	if err := router.Decode(w, r, &b); err != nil {
		legacyError(w, nethttp.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(b.Hostname) == "" || strings.TrimSpace(b.Name) == "" || b.Port < 1 || b.Port > 65535 {
		legacyError(w, nethttp.StatusBadRequest, "hostname, name, and 1<=port<=65535 are required")
		return
	}
	nsfw := b.IsNSFW
	res, err := h.d.Networks.Create(r.Context(), principal.From(r.Context()), networks.Submission{
		Hostname: b.Hostname, Port: b.Port, TLS: b.TLS, Name: &b.Name, Description: b.Description,
		Tags: b.Tags, Languages: b.Languages, IsNSFW: &nsfw,
	})
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusCreated, h.legacyOwnerView(res.Network))
}

func (h *Handlers) legacyVerify(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := h.resolveLegacyID(r)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	out, err := h.d.Networks.Verify(r.Context(), principal.From(r.Context()), id)
	if err != nil {
		if errors.Is(err, networks.ErrRateLimited) {
			h.writeRateLimited(w, r, id)
			legacyError(w, nethttp.StatusTooManyRequests, "rate limited")
			return
		}
		legacyFail(w, r, err)
		return
	}
	status := nethttp.StatusOK
	if !out.Report.Success {
		status = nethttp.StatusConflict
	}
	router.WriteJSON(w, status, map[string]any{
		"server": h.legacyOwnerView(out.Network), "report": legacyReport(out.Report), "success": out.Report.Success,
	})
}

// legacyReport keeps the report JSON as the backend sent it.
func legacyReport(r dnsverify.Report) dnsverify.Report {
	if r.Results == nil {
		r.Results = map[string]dnsverify.Result{}
	}
	return r
}

func (h *Handlers) legacyRegenerate(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := h.resolveLegacyID(r)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	n, err := h.d.Networks.RegenerateToken(r.Context(), principal.From(r.Context()), id)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.legacyOwnerView(n))
}

func (h *Handlers) legacyUpdate(w nethttp.ResponseWriter, r *nethttp.Request) {
	id, err := h.resolveLegacyID(r)
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	var b struct {
		Name        *string   `json:"name,omitempty"`
		Description *string   `json:"description,omitempty"`
		Tags        *[]string `json:"tags,omitempty"`
		Languages   *[]string `json:"languages,omitempty"`
		IsNSFW      *bool     `json:"is_nsfw,omitempty"`
	}
	if err := router.Decode(w, r, &b); err != nil {
		legacyError(w, nethttp.StatusBadRequest, "invalid json")
		return
	}
	if b.Name != nil && strings.TrimSpace(*b.Name) == "" {
		legacyError(w, nethttp.StatusBadRequest, "name must be non-empty")
		return
	}
	n, err := h.d.Networks.Update(r.Context(), principal.From(r.Context()), id, networks.Edit{
		Name: b.Name, Description: b.Description, Tags: b.Tags, Languages: b.Languages, IsNSFW: b.IsNSFW,
	})
	if err != nil {
		legacyFail(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.legacyOwnerView(n))
}

func (h *Handlers) legacyImage(kind networks.ImageKind, upload bool) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		id, err := h.resolveLegacyID(r)
		if err != nil {
			legacyFail(w, r, err)
			return
		}
		var data []byte
		if upload {
			if data, err = readImage(w, r); err != nil {
				legacyFail(w, r, err)
				return
			}
		}
		n, err := h.d.Networks.SetImage(r.Context(), principal.From(r.Context()), id, kind, data)
		if err != nil {
			legacyFail(w, r, err)
			return
		}
		router.WriteJSON(w, nethttp.StatusOK, h.legacyOwnerView(n))
	}
}
