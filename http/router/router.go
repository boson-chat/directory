// Package router is the extension point of the HTTP layer. Feature
// packages (core handlers, federation, …) register a Module in an init
// function; http.Build wires every registered module with the shared Deps
// and mounts its routes and background workers. See SKELETON.md.
package router

import (
	"context"
	nethttp "net/http"
	"sort"
	"sync"
	"time"

	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/leader"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/blocks"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/boson-chat/directory/internal/services/settings"
	"github.com/boson-chat/directory/internal/services/workers"
	"github.com/boson-chat/directory/metrics"
)

// Deps is everything a module may use. Built once by http.Build.
type Deps struct {
	Config      config.Config
	DB          *db.DB
	NetworkRepo network.NetworkRepositoryImpl
	Networks    networks.NetworkServiceImpl
	Blocks      blocks.BlockServiceImpl
	Audit       audit.AuditServiceImpl
	Media       media.MediaServiceImpl
	Verifier    dnsverify.Verifier
	Locker      leader.LockerImpl
	Metrics     *metrics.Metrics
	// Settings are the runtime settings (name, include_main, ...): env
	// defaults overridden from the admin panel.
	Settings settings.SettingsServiceImpl
	// HTTPClient is for outbound calls (peers, media import).
	HTTPClient *nethttp.Client
	Now        func() time.Time
}

// Module is a feature that mounts routes and/or runs workers.
type Module struct {
	// Name orders modules (stable) and labels logs.
	Name string
	// Routes mounts handlers on r. May be nil.
	Routes func(d *Deps, r *Router) error
	// Workers returns background jobs. May be nil.
	Workers func(d *Deps) []workers.Worker
	// Start runs once after wiring, before the listeners (e.g. seeding the
	// built-in peer). May be nil.
	Start func(ctx context.Context, d *Deps) error
}

var (
	mu      sync.Mutex
	modules = map[string]Module{}
)

// Register adds a module. Call it from an init function; the package must
// be imported (blank import in http/modules.go).
func Register(m Module) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := modules[m.Name]; dup {
		panic("router: module registered twice: " + m.Name)
	}
	modules[m.Name] = m
}

// Modules returns the registered modules sorted by name.
func Modules() []Module {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Module, 0, len(modules))
	for _, m := range modules {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Router wraps the public mux with the access levels routes need. Every
// route already runs behind Recover, ClientIP, Logging, SecurityHeaders,
// CORS and Authenticate (so principal.From works everywhere).
type Router struct {
	Mux *nethttp.ServeMux
}

// New wraps mux.
func New(mux *nethttp.ServeMux) *Router { return &Router{Mux: mux} }

// Public mounts a route anyone may call.
func (r *Router) Public(pattern string, h nethttp.HandlerFunc) { r.Mux.Handle(pattern, h) }

// Authed mounts a route that needs a principal (account or Network key).
func (r *Router) Authed(pattern string, h nethttp.HandlerFunc) {
	r.Mux.Handle(pattern, RequirePrincipal(h))
}

// Account mounts a route that needs a boson account (not a Network key).
func (r *Router) Account(pattern string, h nethttp.HandlerFunc) {
	r.Mux.Handle(pattern, RequireAccount(h))
}

// Operator mounts a route that needs perm (default directory.manage).
func (r *Router) Operator(pattern string, h nethttp.HandlerFunc, perm ...string) {
	need := principal.PermDirectoryManage
	if len(perm) > 0 {
		need = perm[0]
	}
	r.Mux.Handle(pattern, RequirePerm(need, h))
}

// RequirePrincipal 401s anonymous requests.
func RequirePrincipal(h nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		if principal.From(req.Context()) == nil {
			WriteError(w, req, ErrUnauthenticated)
			return
		}
		h.ServeHTTP(w, req)
	})
}

// RequireAccount 401s anything but an account.
func RequireAccount(h nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		p := principal.From(req.Context())
		if p == nil || p.Kind != principal.KindAccount {
			WriteError(w, req, ErrUnauthenticated)
			return
		}
		h.ServeHTTP(w, req)
	})
}

// RequirePerm 401s anonymous and 403s accounts without perm.
func RequirePerm(perm string, h nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		p := principal.From(req.Context())
		if p == nil || (p.Kind != principal.KindAccount && p.Kind != principal.KindInternal) {
			WriteError(w, req, ErrUnauthenticated)
			return
		}
		if !p.Has(perm) {
			WriteError(w, req, ErrForbidden(perm))
			return
		}
		h.ServeHTTP(w, req)
	})
}
