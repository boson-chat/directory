package handlers

import (
	"errors"
	nethttp "net/http"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/http/views"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/networks"
)

// Moving a listing to a new hostname. Same auth as every owner write: the
// owner's account (Bearer token or the boson_at cookie), the listing's
// Network key (the stack admin panel), or an operator.

func (h *Handlers) now() time.Time {
	if h.d.Now != nil {
		return h.d.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *Handlers) moveView(m *network.Move) *views.Move {
	return views.NewMove(m, h.d.Networks.Options().TokenTTL, h.now())
}

// moveJSON is a Listing plus the move history (GET only).
type moveJSON struct {
	views.Listing
	History []*views.Move `json:"history,omitempty"`
}

func (h *Handlers) moveState(r *nethttp.Request, st *networks.MoveState) moveJSON {
	out := moveJSON{Listing: h.listing(r, st.Network)}
	out.Move = h.moveView(st.Move)
	if st.History != nil {
		out.History = make([]*views.Move, 0, len(st.History))
		for _, m := range st.History {
			out.History = append(out.History, h.moveView(m))
		}
	}
	return out
}

// GET /v1/networks/{id}/move → Listing + history.
func (h *Handlers) getMove(w nethttp.ResponseWriter, r *nethttp.Request) {
	st, err := h.d.Networks.GetMove(r.Context(), principal.From(r.Context()), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	out := h.moveState(r, st)
	if out.History == nil {
		out.History = []*views.Move{}
	}
	router.WriteJSON(w, nethttp.StatusOK, out)
}

// POST /v1/networks/{id}/move {hostname, port?, tls?} → 201 Listing with
// the pending move (200 when the same request was already pending, or when
// only port/tls changed and that applied at once).
func (h *Handlers) requestMove(w nethttp.ResponseWriter, r *nethttp.Request) {
	var body networks.MoveRequest
	if err := router.Decode(w, r, &body); err != nil {
		router.WriteError(w, r, err)
		return
	}
	st, err := h.d.Networks.RequestMove(r.Context(), principal.From(r.Context()), r.PathValue("id"), body)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	status := nethttp.StatusOK
	if st.Created {
		status = nethttp.StatusCreated
	}
	router.WriteJSON(w, status, h.moveState(r, st))
}

// DELETE /v1/networks/{id}/move → Listing (move gone). Idempotent.
func (h *Handlers) cancelMove(w nethttp.ResponseWriter, r *nethttp.Request) {
	st, err := h.d.Networks.CancelMove(r.Context(), principal.From(r.Context()), r.PathValue("id"))
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, h.moveState(r, st))
}

// verifyMoveJSON mirrors verifyJSON: 200 when the listing moved, 409 with
// the per-resolver report while the record isn't seen everywhere yet.
type verifyMoveJSON struct {
	Network      views.Network       `json:"network"`
	Verification *views.Verification `json:"verification"`
	Move         *views.Move         `json:"move"`
	Moved        bool                `json:"moved"`
	Success      bool                `json:"success"`
	Report       any                 `json:"report"`
}

// POST /v1/networks/{id}/move/verify.
func (h *Handlers) verifyMove(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	out, err := h.d.Networks.CheckMove(r.Context(), principal.From(r.Context()), id)
	if err != nil {
		if errors.Is(err, networks.ErrRateLimited) {
			h.writeRateLimited(w, r, networks.MoveCheckKey(id))
		}
		router.WriteError(w, r, err)
		return
	}
	l := h.listing(r, out.Network)
	status := nethttp.StatusOK
	if !out.Moved {
		status = nethttp.StatusConflict
	}
	router.WriteJSON(w, status, verifyMoveJSON{
		Network: l.Network, Verification: l.Verification, Move: h.moveView(out.Move),
		Moved: out.Moved, Success: out.Report.Success, Report: out.Report,
	})
}
