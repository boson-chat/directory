// Package handlers is the core module: the public /v1 API, the owner API,
// the operator API (blocks, moderation, audit), the legacy /servers API
// and the verify/health workers.
package handlers

import (
	"context"
	nethttp "net/http"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/internal/services/health"
	"github.com/boson-chat/directory/internal/services/workers"
)

// Version of the API advertised in .well-known.
const APIVersion = "1"

// Handlers holds the core module's dependencies.
type Handlers struct {
	d *router.Deps
}

func init() {
	router.Register(router.Module{
		Name: "core",
		Routes: func(d *router.Deps, r *router.Router) error {
			h := &Handlers{d: d}
			h.RegisterPublic(r)
			return nil
		},
		Workers: coreWorkers,
	})
}

func coreWorkers(d *router.Deps) []workers.Worker {
	var out []workers.Worker
	cfg := d.Config
	if cfg.VerifyConfig.WorkerEnabled {
		out = append(out, workers.Worker{Name: "verify", Every: cfg.VerifyConfig.WorkerTick, Run: func(ctx context.Context) error {
			_, err := d.Networks.VerifyDue(ctx)
			return err
		}})
	}
	if cfg.HealthConfig.WorkerEnabled {
		hc := cfg.HealthConfig
		c := &health.Checker{
			Repo: d.NetworkRepo, Prober: health.IRCProber{DialTimeout: hc.DialTimeout},
			Every: hc.Every, UserCount: hc.UserCount, UserCountEvery: hc.UserCountEvery,
			Concurrency: hc.Concurrency, Metrics: d.Metrics, Now: d.Now,
		}
		// Tick more often than Every; RunDue only probes rows that are due.
		tick := max(hc.Every/3, 30_000_000_000)
		out = append(out, workers.Worker{Name: "health", Every: tick, Run: func(ctx context.Context) error {
			_, err := c.RunDue(ctx)
			return err
		}})
	}
	return out
}

func (h *Handlers) RegisterPublic(r *router.Router) {
	r.Public("GET /.well-known/boson-directory.json", h.wellKnown)
}

type wellKnownJSON struct {
	Name       string `json:"name"`
	API        string `json:"api"`
	Version    string `json:"version"`
	Operator   string `json:"operator"`
	Contact    string `json:"contact"`
	Federation bool   `json:"federation"`
}

func (h *Handlers) wellKnown(w nethttp.ResponseWriter, _ *nethttp.Request) {
	c := h.d.Config
	w.Header().Set("Cache-Control", "public, max-age=300")
	router.WriteJSON(w, nethttp.StatusOK, wellKnownJSON{
		Name: c.DirectoryConfig.Name, API: c.HTTPConfig.PublicURL + "/v1", Version: APIVersion,
		Operator: c.DirectoryConfig.Operator, Contact: c.DirectoryConfig.Contact, Federation: true,
	})
}
