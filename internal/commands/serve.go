package commands

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/boson-chat/directory/db"
	dirhttp "github.com/boson-chat/directory/http"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/metrics"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the public (API + web UI) and internal (/metrics) listeners and the background workers",
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		log := logger.FromCtx(ctx)

		if cfg.DBConfig.AutoMigrate {
			if err := db.MigrateUp(cfg.DBConfig.DSN()); err != nil {
				return err
			}
			log.Info().Msg("migrations up to date")
		}
		mp, err := metrics.NewProvider(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = mp.Shutdown(sctx)
		}()
		m, err := metrics.New(mp.MeterProvider())
		if err != nil {
			return err
		}
		app, err := dirhttp.Build(ctx, cfg, dirhttp.Options{Metrics: m, MetricsHandler: mp.Handler()})
		if err != nil {
			return err
		}
		defer app.Close()
		if !cfg.AuthConfig.Enabled() {
			log.Warn().Msg("AUTH_ISSUER is empty: account sign-in is off, only Network keys can register")
		}
		app.RunBackground(ctx)
		return dirhttp.Serve(ctx, app)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
