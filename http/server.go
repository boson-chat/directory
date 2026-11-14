// Package http wires the service together (constructor DI in one place) and
// runs the public and internal listeners.
package http

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/http/middleware"
	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/http/spa"
	"github.com/boson-chat/directory/internal/db"
	auditrepo "github.com/boson-chat/directory/internal/db/repositories/audit"
	blockrepo "github.com/boson-chat/directory/internal/db/repositories/block"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/leader"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/blocks"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/boson-chat/directory/internal/services/workers"
	"github.com/boson-chat/directory/metrics"
	"github.com/boson-chat/directory/ui"
)

// Options override dependencies (tests, CLI).
type Options struct {
	Metrics        *metrics.Metrics
	MetricsHandler nethttp.Handler
	Now            func() time.Time
	// Verifier replaces the DNS verifier chosen by DNS_VERIFIER.
	Verifier dnsverify.Verifier
	// Storage replaces the media storage chosen by MEDIA_*.
	Storage media.Storage
	// TokenVerifier replaces the JWKS verifier (tests).
	TokenVerifier middleware.TokenVerifier
	// Locker replaces the advisory-lock leader election.
	Locker     leader.LockerImpl
	HTTPClient *nethttp.Client
	// UI replaces the embedded web UI.
	UI fs.FS
	// NoUI skips the SPA (CLI commands).
	NoUI bool
}

// App is the wired service.
type App struct {
	Config   config.Config
	DB       *db.DB
	Deps     *router.Deps
	Public   nethttp.Handler
	Internal nethttp.Handler
	Workers  []workers.Worker
	runner   *workers.Runner
}

// Build constructs everything: database → repositories → services →
// modules (routes, workers).
func Build(ctx context.Context, cfg config.Config, opts Options) (*App, error) {
	trusted, err := cfg.HTTPConfig.TrustedProxyPrefixes()
	if err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	m := opts.Metrics
	if m == nil {
		m = metrics.Noop()
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &nethttp.Client{Timeout: 20 * time.Second}
	}

	// 1. Database
	database, err := db.NewDatabase(ctx, cfg.DBConfig)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*App, error) {
		_ = database.Close()
		return nil, err
	}
	sqlDB, err := database.SQL()
	if err != nil {
		return fail(err)
	}

	// 2. Repositories
	networkRepo := network.NewNetworkRepository(database)
	blockRepo := blockrepo.NewBlockRepository(database)
	auditRepo := auditrepo.NewAuditRepository(database)

	// 3. Services
	verifier := opts.Verifier
	if verifier == nil {
		if verifier, err = newVerifier(ctx, cfg); err != nil {
			return fail(err)
		}
	}
	storage := opts.Storage
	var local *media.LocalStorage
	if storage == nil {
		if cfg.MediaConfig.UseS3() {
			mc := cfg.MediaConfig
			storage = media.NewS3Storage(mc.S3Endpoint, mc.S3Region, mc.S3Bucket, mc.S3AccessKey, mc.S3SecretKey)
		} else {
			if local, err = media.NewLocalStorage(cfg.MediaConfig.Dir); err != nil {
				return fail(err)
			}
			storage = local
		}
	} else if ls, ok := storage.(*media.LocalStorage); ok {
		local = ls
	}
	mediaService := media.NewMediaService(storage, cfg.MediaConfig.PublicURL)
	auditService := audit.NewAuditService(auditRepo, opts.Now)
	blockService := blocks.NewBlockService(blockRepo, networkRepo, database, auditService)
	vc := cfg.VerifyConfig
	networkService := networks.NewNetworkService(networks.Deps{
		Repo: networkRepo, Verifier: verifier, Blocks: blockService, Media: mediaService, Audit: auditService, Metrics: m,
		Options: networks.Options{
			TokenTTL: vc.TokenTTL, LapseAfter: vc.LapseAfter, VerifiedEvery: vc.VerifiedEvery,
			PendingEvery: vc.PendingEvery, LapsedEvery: vc.LapsedEvery, OwnerRateLimit: vc.OwnerRateLimit, Now: opts.Now,
		},
	})
	locker := opts.Locker
	if locker == nil {
		locker = leader.NewPGLocker(sqlDB)
	}
	tokens := opts.TokenVerifier
	if tokens == nil && cfg.AuthConfig.Enabled() {
		v, err := middleware.NewJWTVerifier(ctx, cfg.AuthConfig)
		if err != nil {
			return fail(err)
		}
		tokens = v
	}

	deps := &router.Deps{
		Config: cfg, DB: database, NetworkRepo: networkRepo, Networks: networkService, Blocks: blockService,
		Audit: auditService, Media: mediaService, Verifier: verifier, Locker: locker, Metrics: m,
		HTTPClient: httpClient, Now: opts.Now,
	}

	// 4. Handlers: every registered module (http/modules.go).
	publicMux := nethttp.NewServeMux()
	r := router.New(publicMux)
	var ws []workers.Worker
	for _, mod := range router.Modules() {
		if mod.Routes != nil {
			if err := mod.Routes(deps, r); err != nil {
				return fail(fmt.Errorf("module %s: %w", mod.Name, err))
			}
		}
		if mod.Workers != nil {
			ws = append(ws, mod.Workers(deps)...)
		}
	}
	for _, mod := range router.Modules() {
		if mod.Start != nil {
			if err := mod.Start(ctx, deps); err != nil {
				return fail(fmt.Errorf("module %s start: %w", mod.Name, err))
			}
		}
	}
	if local != nil {
		publicMux.Handle("GET /media/", mediaHandler(local.FS()))
	}
	if !opts.NoUI {
		uiFS := opts.UI
		if uiFS == nil {
			uiFS = ui.FS()
		}
		s, err := spa.NewSPA(uiFS)
		if err != nil {
			return fail(err)
		}
		// The UI signs people in against the auth service from the browser.
		csp := s.CSP(spa.Origin(cfg.AuthConfig.PublicURL))
		// Listing images can come from any directory's media host.
		csp = strings.Replace(csp, "img-src 'self' data:", "img-src 'self' data: https:", 1)
		publicMux.Handle("GET /", withCSP(csp, s))
	}

	internalMux := nethttp.NewServeMux()
	ready := func(w nethttp.ResponseWriter, req *nethttp.Request) {
		if err := sqlDB.PingContext(req.Context()); err != nil {
			router.WriteJSON(w, nethttp.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		router.WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}
	live := func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		router.WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	}
	for _, mux := range []*nethttp.ServeMux{publicMux, internalMux} {
		mux.HandleFunc("GET /healthz", live)
		mux.HandleFunc("GET /readyz", ready)
	}
	if opts.MetricsHandler != nil {
		internalMux.Handle("GET /metrics", opts.MetricsHandler)
	}

	return &App{
		Config: cfg, DB: database, Deps: deps,
		Public: middleware.Chain(publicMux,
			middleware.Recover, middleware.ClientIP(trusted), middleware.Logging, middleware.SecurityHeaders,
			middleware.CORS(config.SplitList(cfg.HTTPConfig.CORSAllowedOrigins)),
			middleware.Authenticate(tokens, cfg.AuthConfig.AccessCookie, cfg.DirectoryConfig.InternalToken, router.WriteError)),
		Internal: middleware.Chain(internalMux, middleware.Recover, middleware.ClientIP(nil), middleware.Logging),
		Workers:  ws,
		runner:   workers.NewRunner(locker, m),
	}, nil
}

func newVerifier(ctx context.Context, cfg config.Config) (dnsverify.Verifier, error) {
	vc := cfg.VerifyConfig
	if strings.EqualFold(vc.Verifier, "fake") {
		if cfg.AppConfig.IsProduction() {
			return nil, errors.New("DNS_VERIFIER=fake is not allowed with ENV=production")
		}
		logger.FromCtx(ctx).Warn().Msg("DNS_VERIFIER=fake: every DNS check succeeds (dev/CI only)")
		return dnsverify.NewFake(), nil
	}
	opts := []dnsverify.Option{dnsverify.WithPerResolverTimeout(vc.ResolverTimeout)}
	if list := config.SplitList(vc.Resolvers); len(list) > 0 {
		rs, err := dnsverify.ParseResolvers(list)
		if err != nil {
			return nil, err
		}
		opts = append(opts, dnsverify.WithResolvers(rs))
	}
	return dnsverify.NewVerifier(opts...), nil
}

func withCSP(csp string, h nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

// mediaHandler serves locally stored images. Keys are content-addressed,
// so responses are immutable.
func mediaHandler(fsys fs.FS) nethttp.Handler {
	files := nethttp.StripPrefix("/media/", nethttp.FileServerFS(fsys))
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || !strings.HasSuffix(r.URL.Path, ".png") {
			nethttp.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		files.ServeHTTP(w, r)
	})
}

// RunBackground starts the registered workers.
func (a *App) RunBackground(ctx context.Context) {
	a.runner.Start(ctx, a.Workers...)
}

// RunWorkerOnce runs the named worker now (CLI, tests).
func (a *App) RunWorkerOnce(ctx context.Context, name string) error {
	for _, w := range a.Workers {
		if w.Name == name {
			a.runner.Once(ctx, w)
			return nil
		}
	}
	return fmt.Errorf("no worker named %q", name)
}

// Close closes the database.
func (a *App) Close() error { return a.DB.Close() }

// Serve runs both listeners until ctx is done, then shuts down gracefully.
func Serve(ctx context.Context, app *App) error {
	cfg := app.Config.HTTPConfig
	servers := []*nethttp.Server{
		{Addr: cfg.Addr, Handler: app.Public, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second},
		{Addr: cfg.InternalAddr, Handler: app.Internal, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second},
	}
	errCh := make(chan error, len(servers))
	for i, srv := range servers {
		name := []string{"public", "internal"}[i]
		go func() {
			logger.FromCtx(ctx).Info().Str("addr", srv.Addr).Str("listener", name).Msg("listening")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
				errCh <- fmt.Errorf("%s listener: %w", name, err)
			}
		}()
	}
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdownCtx)
	}
	return runErr
}
