// Package settings mounts the operator settings API (GET/PATCH
// /v1/admin/settings): the directory's runtime settings, changed from the
// stack admin panel instead of .env. Env values are the defaults; a PATCH
// stores an override in the database (null resets it to the default) and
// takes effect at once, without a restart.
package settings

import (
	nethttp "net/http"
	"time"

	"github.com/boson-chat/directory/http/handlers/federation"
	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/principal"
	fedsvc "github.com/boson-chat/directory/internal/services/federation"
	"github.com/boson-chat/directory/internal/services/settings"
)

func init() {
	router.Register(router.Module{
		Name: "settings",
		Routes: func(d *router.Deps, r *router.Router) error {
			h := &handler{d: d}
			r.Operator("GET /v1/admin/settings", h.get)
			r.Operator("PATCH /v1/admin/settings", h.patch)
			return nil
		},
	})
}

type handler struct{ d *router.Deps }

func (h *handler) fed() fedsvc.FederationServiceImpl { return federation.Service(h.d) }

// ErrIsMain: include_main can't be switched on in the main directory.
var ErrIsMain = apperr.New(nethttp.StatusConflict, "is_main_directory",
	"this is the main directory itself (its main peer URL is its own), so there is no main directory to include")

type valuesJSON struct {
	Name        string `json:"name"`
	Operator    string `json:"operator"`
	Contact     string `json:"contact"`
	IncludeMain bool   `json:"include_main"`
	MainPeerURL string `json:"main_peer_url"`
	SyncEvery   string `json:"sync_every"`
}

func toValues(v settings.Values) valuesJSON {
	return valuesJSON{
		Name: v.Name, Operator: v.Operator, Contact: v.Contact, IncludeMain: v.IncludeMain,
		MainPeerURL: v.MainPeerURL, SyncEvery: settings.FormatDuration(v.SyncEvery),
	}
}

type settingsJSON struct {
	Settings valuesJSON `json:"settings"`
	// Defaults are the env values a reset (null) goes back to.
	Defaults valuesJSON `json:"defaults"`
	// Overridden lists the settings changed at runtime.
	Overridden []string `json:"overridden"`
	// IsMain: this directory is the main directory (main_peer_url is its
	// own URL); include_main does not apply.
	IsMain bool `json:"is_main"`
	// MainActive: the builtin main peer is being synced (include_main on,
	// not the main directory, peer enabled).
	MainActive bool `json:"main_active"`
	// MainPeer is the builtin peer row (null when there is none).
	MainPeer  any       `json:"main_peer"`
	PublicURL string    `json:"public_url"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy *string   `json:"updated_by"`
	// Source is "file" (stack config file: read-only here, PATCH answers
	// 409 managed_by_config_file) or "db".
	Source string `json:"source"`
	// Peers are the config file's directory.peers (file mode, when set).
	Peers []string `json:"peers,omitempty"`
}

func (h *handler) snapshot(r *nethttp.Request) (*settingsJSON, error) {
	snap, err := h.d.Settings.Get(r.Context())
	if err != nil {
		return nil, err
	}
	fed := h.fed()
	out := &settingsJSON{
		Settings: toValues(snap.Values), Defaults: toValues(snap.Defaults), Overridden: snap.Overridden,
		IsMain: fed.IsMain(), PublicURL: h.d.Config.HTTPConfig.PublicURL, UpdatedAt: snap.UpdatedAt, UpdatedBy: snap.UpdatedBy,
		Source: snap.Source,
	}
	if f := h.d.Settings.File(); f != nil && f.PeersSet {
		out.Peers = f.Peers
	}
	mp, err := fed.MainPeer(r.Context())
	if err != nil {
		return nil, err
	}
	if mp != nil {
		out.MainPeer = federation.PeerJSON(fed, mp)
		out.MainActive = fed.Active(mp)
	}
	return out, nil
}

func (h *handler) get(w nethttp.ResponseWriter, r *nethttp.Request) {
	out, err := h.snapshot(r)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, out)
}

func (h *handler) patch(w nethttp.ResponseWriter, r *nethttp.Request) {
	if h.d.Settings.File() != nil {
		router.WriteError(w, r, settings.ErrManagedByConfigFile)
		return
	}
	var p settings.Patch
	if err := router.Decode(w, r, &p); err != nil {
		router.WriteError(w, r, err)
		return
	}
	fed := h.fed()
	// Switching include_main on only makes sense when the main peer
	// (after this patch) is another directory.
	if p.IncludeMain.Set && !p.IncludeMain.Null && p.IncludeMain.V {
		main := h.d.Settings.Current().MainPeerURL
		if p.MainPeerURL.Set {
			main = p.MainPeerURL.V
			if p.MainPeerURL.Null {
				main = h.d.Settings.Defaults().MainPeerURL
			}
		}
		if main != "" && fed.IsSelf(main) {
			router.WriteError(w, r, ErrIsMain)
			return
		}
	}
	pr := principal.From(r.Context())
	_, after, err := h.d.Settings.Update(r.Context(), pr, p)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	if p.IncludeMain.Set || p.MainPeerURL.Set {
		if err := fed.ApplyMainSettings(r.Context(), pr, p.IncludeMain.Set && after.IncludeMain); err != nil {
			router.WriteError(w, r, err)
			return
		}
	}
	out, err := h.snapshot(r)
	if err != nil {
		router.WriteError(w, r, err)
		return
	}
	router.WriteJSON(w, nethttp.StatusOK, out)
}
