package commands

import (
	"database/sql"
	"errors"
	"fmt"

	dirhttp "github.com/boson-chat/directory/http"
	"github.com/boson-chat/directory/internal/services/importer"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx database/sql driver
	"github.com/spf13/cobra"
)

var (
	importFrom    string
	importDryRun  bool
	importCDN     string
	importCopy    bool
	importVerbose bool
)

var importLegacyCmd = &cobra.Command{
	Use:   "import-legacy --from <backend DSN>",
	Short: "Copy the boson backend's servers table into this directory",
	Long: `Copies servers into networks: maps registered_by to the owner's auth user id
through auth_identities, normalises hostnames (verified beats others, then the
oldest), keeps the backend uuid for /servers/{id}, and copies image keys
(--legacy-cdn-url turns them into absolute URLs; add --copy-media to download
them into this directory's media storage). Safe to re-run: imported rows are skipped.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if importFrom == "" {
			return errors.New("--from is required")
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		src, err := sql.Open("pgx", importFrom)
		if err != nil {
			return err
		}
		defer src.Close()
		if err := src.PingContext(ctx); err != nil {
			return fmt.Errorf("source database: %w", err)
		}
		app, err := dirhttp.Build(ctx, cfg, dirhttp.Options{NoUI: true})
		if err != nil {
			return err
		}
		defer app.Close()
		out := cmd.OutOrStdout()
		rep, err := importer.Import(ctx, src, app.Deps.NetworkRepo, app.Deps.Media, importer.Options{
			DryRun: importDryRun, CDNURL: importCDN, CopyMedia: importCopy, HTTP: app.Deps.HTTPClient,
			Log: func(format string, args ...any) {
				if importVerbose || importDryRun {
					fmt.Fprintf(out, format+"\n", args...)
				}
			},
		})
		prefix := ""
		if importDryRun {
			prefix = "dry run (nothing written): "
		}
		fmt.Fprintln(out, prefix+rep.String())
		return err
	},
}

func init() {
	f := importLegacyCmd.Flags()
	f.StringVar(&importFrom, "from", "", "backend Postgres DSN (reads servers and auth_identities)")
	f.BoolVar(&importDryRun, "dry-run", false, "print what would be imported, write nothing")
	f.StringVar(&importCDN, "legacy-cdn-url", "", "base URL the backend served image keys from (e.g. https://cdn.boson.chat)")
	f.BoolVar(&importCopy, "copy-media", false, "download images from --legacy-cdn-url into this directory's media storage")
	f.BoolVarP(&importVerbose, "verbose", "v", false, "print every decision")
	rootCmd.AddCommand(importLegacyCmd)
}
