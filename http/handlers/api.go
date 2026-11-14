package handlers

import (
	"errors"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/http/views"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/networks"
)

func (h *Handlers) registerAPI(r *router.Router) {
	// Public (anonymous allowed; owners see more on their own listings).
	r.Public("GET /v1/networks", h.list)
	r.Public("GET /v1/networks/lookup", h.lookup)
	r.Public("GET /v1/networks/{id}", h.get)
	r.Public("GET /v1/facets", h.facets)

	// Owner: a boson account or a Network key; operators may manage any listing.
	r.Authed("GET /v1/me/networks", h.mine)
	r.Authed("POST /v1/networks", h.writes(h.create))
	r.Authed("PATCH /v1/networks/{id}", h.writes(h.update))
	r.Authed("DELETE /v1/networks/{id}", h.writes(h.remove))
	r.Authed("POST /v1/networks/{id}/verify", h.writes(h.verify))
	r.Authed("POST /v1/networks/{id}/regenerate-token", h.writes(h.regenerate))
	for _, kind := range []networks.ImageKind{networks.ImageIcon, networks.ImageBanner} {
		r.Authed("PUT /v1/networks/{id}/"+string(kind), h.writes(h.putImage(kind)))
		r.Authed("DELETE /v1/networks/{id}/"+string(kind), h.writes(h.deleteImage(kind)))
	}
}

func (h *Handlers) listing(r *nethttp.Request, n *network.Network) views.Listing {
	p := principal.From(r.Context())
	l := views.NewListing(n, h.d.Media, h.d.Networks.CanManage(p, n), h.d.Networks.Options().TokenTTL)
	l.Network.WithOriginName(h.peerNames(r.Context()))
	return l
}

type listJSON struct {
	Networks      []views.Network `json:"networks"`
	NextCursor    *string         `json:"next_cursor"`
	TotalEstimate int64           `json:"total_estimate"`
}

func boolParam(v string) bool {
	b, _ := strconv.ParseBool(v)
	return b
}

func (h *Handlers) pageJSON(r *nethttp.Request, page *networks.Page, withOwner bool) listJSON {
	p := principal.From(r.Context())
	out := listJSON{Networks: make([]views.Network, 0, len(page.Networks)), TotalEstimate: page.TotalEstimate}
	names := h.peerNames(r.Context())
	for _, n := range page.Networks {
		v := views.NewNetwork(n, h.d.Media)
		v.WithOriginName(names)
		if withOwner || (p != nil && !p.IsOperator() && h.d.Networks.CanManage(p, n)) {
			v.Owner = views.OwnerOf(n)
		}
		out.Networks = append(out.Networks, v)
	}
	if page.NextCursor != "" {
		c := page.NextCursor
		out.NextCursor = &c
	}
	return out
}

// GET /v1/networks?q=&lang=&tag=&nsfw=&source=&sort=&limit=&cursor=&owner=
func (h *Handlers) list(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		var err error
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
			router.WriteError(w, r, apperr.Invalid("limit must be 1–100"))
			return
		}
	}
	page, err := h.d.Networks.List(r.Context(), principal.From(r.Context()), networks.ListQuery{
		Query: q.Get("q"), Language: q.Get("lang"), Tag: q.Get("tag"), IncludeNSFW: boolParam(q.Get("nsfw")),
		Source: q.Get("source"), Owner: q.Get("owner"), Sort: network.Sort(q.Get("sort")), Cursor: q.Get("cursor"), Limit: limit,
	})
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	if q.Get("owner") == "" {
		w.Header().Set("Cache-Control", "public, max-age=30")
	}
	router.WriteJSON(w, nethttp.StatusOK, h.pageJSON(r, page, q.Get("owner") != ""))
}

func (h *Handlers) get(w nethttp.ResponseWriter, r *nethttp.Request) {
	n, err := h.d.Networks.Get(r.Context(), principal.From(r.Context()), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
}

func (h *Handlers) lookup(w nethttp.ResponseWriter, r *nethttp.Request) {
	host := r.URL.Query().Get("hostname")
	if host == "" {
		router.WriteError(w, r, apperr.Invalid("hostname is required"))
		return
	}
	n, err := h.d.Networks.Lookup(r.Context(), principal.From(r.Context()), host)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	// A Network key only ever looks up its own listing: someone else's
	// is "not yours" (404), so the panel offers to register instead.
	if p := principal.From(r.Context()); p != nil && p.Kind == principal.KindNetworkKey && !h.d.Networks.CanManage(p, n) {
		router.WriteError(w, r, networks.ErrNotFound)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
}

type facetsJSON struct {
	Tags      []network.Facet `json:"tags"`
	Languages []network.Facet `json:"languages"`
	Sources   []sourceJSON    `json:"sources"`
}

// sourceJSON is a listing source: "local" or a peer (id usable as ?source=).
type sourceJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Count int64  `json:"count"`
}

func (h *Handlers) facets(w nethttp.ResponseWriter, r *nethttp.Request) {
	nsfw := boolParam(r.URL.Query().Get("nsfw"))
	tags, langs, err := h.d.Networks.Facets(r.Context(), nsfw)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	counts, err := h.d.NetworkRepo.OriginCounts(r.Context(), nsfw)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	c := h.d.Config
	sources := []sourceJSON{{ID: network.OriginLocal, Name: c.DirectoryConfig.Name, URL: c.HTTPConfig.PublicURL, Count: counts[network.OriginLocal]}}
	for _, p := range h.peerList(r.Context()) {
		if n := counts[network.PeerOrigin(p.ID)]; n > 0 || p.Enabled {
			sources = append(sources, sourceJSON{ID: p.ID, Name: peerLabel(p), URL: p.URL, Count: n})
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	router.WriteJSON(w, nethttp.StatusOK, facetsJSON{Tags: tags, Languages: langs, Sources: sources})
}

type listingsJSON struct {
	Listings []views.Listing `json:"listings"`
}

func (h *Handlers) mine(w nethttp.ResponseWriter, r *nethttp.Request) {
	list, err := h.d.Networks.Mine(r.Context(), principal.From(r.Context()))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	out := listingsJSON{Listings: make([]views.Listing, 0, len(list))}
	for _, n := range list {
		out.Listings = append(out.Listings, h.listing(r, n))
	}
	router.WriteJSON(w, nethttp.StatusOK, out)
}

func (h *Handlers) create(w nethttp.ResponseWriter, r *nethttp.Request) {
	var sub networks.Submission
	if err := router.Decode(w, r, &sub); err != nil {
		router.WriteError(w, r, err)
		return
	}
	res, err := h.d.Networks.Create(r.Context(), principal.From(r.Context()), sub)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	status := nethttp.StatusOK
	if res.Created {
		status = nethttp.StatusCreated
		w.Header().Set("Location", "/v1/networks/"+res.Network.ID)
	}
	router.WriteJSON(w, status, h.listing(r, res.Network))
}

// editJSON is PATCH /v1/networks/{id}. hostname/port/tls are fixed once
// listed and ignored here.
type editJSON struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	Languages   *[]string `json:"languages"`
	IsNSFW      *bool     `json:"is_nsfw"`
	WebsiteURL  *string   `json:"website_url"`
	AuthURL     *string   `json:"auth_url"`
}

func (e editJSON) edit() networks.Edit {
	out := networks.Edit{Description: e.Description, Tags: e.Tags, Languages: e.Languages, IsNSFW: e.IsNSFW,
		WebsiteURL: e.WebsiteURL, AuthURL: e.AuthURL}
	// An empty name means "unchanged" (the admin panel sends omitempty fields).
	if e.Name != nil && strings.TrimSpace(*e.Name) != "" {
		out.Name = e.Name
	}
	return out
}

func (h *Handlers) update(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body editJSON
	if err := router.Decode(w, r, &body); err != nil {
		router.WriteError(w, r, err)
		return
	}
	n, err := h.d.Networks.Update(r.Context(), principal.From(r.Context()), r.PathValue("id"), body.edit())
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
}

func (h *Handlers) remove(w nethttp.ResponseWriter, r *nethttp.Request) {
	if err := h.d.Networks.Delete(r.Context(), principal.From(r.Context()), r.PathValue("id")); err != nil {
		router.WriteError(w, r, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

// verifyJSON keeps the backend's POST /servers/{id}/verify shape (server,
// report, success) and adds network/verification. 200 on success, 409 with
// the per-resolver report otherwise.
type verifyJSON struct {
	Network      views.Network       `json:"network"`
	Server       views.Network       `json:"server"`
	Verification *views.Verification `json:"verification"`
	Success      bool                `json:"success"`
	Report       any                 `json:"report"`
}

func (h *Handlers) writeRateLimited(w nethttp.ResponseWriter, r *nethttp.Request, id string) {
	if d := h.d.Networks.RetryAfter(id); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int((d+time.Second-1)/time.Second)))
	}
	h.d.Metrics.RateLimited(r.Context(), "verify")
}

func (h *Handlers) verify(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	out, err := h.d.Networks.Verify(r.Context(), principal.From(r.Context()), id)
	if err != nil {
		if errors.Is(err, networks.ErrRateLimited) {
			h.writeRateLimited(w, r, id)
		}
		router.WriteError(w, r, err)
		return
	}
	l := h.listing(r, out.Network)
	status := nethttp.StatusOK
	if !out.Report.Success {
		status = nethttp.StatusConflict
	}
	router.WriteJSON(w, status, verifyJSON{
		Network: l.Network, Server: l.Network, Verification: l.Verification, Success: out.Report.Success, Report: out.Report,
	})
}

func (h *Handlers) regenerate(w nethttp.ResponseWriter, r *nethttp.Request) {
	n, err := h.d.Networks.RegenerateToken(r.Context(), principal.From(r.Context()), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
}

// readImage reads a raw image body (PUT with Content-Type image/*), or the
// first file of a multipart/form-data upload.
func readImage(w nethttp.ResponseWriter, r *nethttp.Request) ([]byte, error) {
	r.Body = nethttp.MaxBytesReader(w, r.Body, media.MaxUploadBytes+64<<10)
	var src io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(media.MaxUploadBytes); err != nil {
			return nil, apperr.New(nethttp.StatusRequestEntityTooLarge, "image_too_large", "the image is too large (5 MiB at most)")
		}
		for _, files := range r.MultipartForm.File {
			if len(files) > 0 {
				f, err := files[0].Open()
				if err != nil {
					return nil, err
				}
				defer f.Close()
				src = f
				break
			}
		}
	}
	data, err := io.ReadAll(io.LimitReader(src, media.MaxUploadBytes+1))
	var mbe *nethttp.MaxBytesError
	if errors.As(err, &mbe) || len(data) > media.MaxUploadBytes {
		return nil, apperr.New(nethttp.StatusRequestEntityTooLarge, "image_too_large", "the image is too large (5 MiB at most)")
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, apperr.New(nethttp.StatusBadRequest, "unsupported_image", "send the image as the request body")
	}
	return data, nil
}

func (h *Handlers) putImage(kind networks.ImageKind) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		data, err := readImage(w, r)
		if err != nil {
			router.WriteError(w, r, err)
			return
		}
		n, err := h.d.Networks.SetImage(r.Context(), principal.From(r.Context()), r.PathValue("id"), kind, data)
		if err != nil {
			router.WriteError(w, r, err)
			return
		}
		router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
	}
}

func (h *Handlers) deleteImage(kind networks.ImageKind) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		n, err := h.d.Networks.SetImage(r.Context(), principal.From(r.Context()), r.PathValue("id"), kind, nil)
		if err != nil {
			router.WriteError(w, r, err)
			return
		}
		router.WriteJSON(w, nethttp.StatusOK, h.listing(r, n))
	}
}
