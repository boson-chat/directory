package commands

import (
	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories/peer"
	settingsrepo "github.com/boson-chat/directory/internal/db/repositories/settings"
	"github.com/boson-chat/directory/internal/services/settings"
	"github.com/spf13/cobra"
)

var exportConfigCmd = &cobra.Command{
	Use:   "export-config",
	Short: "Print the directory: section of the stack config file from the current env and database",
	Long: `Prints YAML for config/boson.yaml built from the env values, the
directory_settings row (values set from the admin panel) and the enabled
peers (peers:; the built-in main peer is covered by include_main and
main_peer_url). The config file itself (BOSON_CONFIG) is not read. Merge the
output into config/boson.yaml once when moving an install to the file.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		database, err := db.NewDatabase(ctx, cfg.DBConfig)
		if err != nil {
			return err
		}
		defer database.Close()
		dc, fc := cfg.DirectoryConfig, cfg.FederationConfig
		svc := settings.NewSettingsService(settings.Deps{
			Repo: settingsrepo.NewSettingsRepository(database),
			Defaults: settings.Values{
				Name: dc.Name, Operator: dc.Operator, Contact: dc.Contact,
				IncludeMain: fc.IncludeMain, MainPeerURL: fc.MainPeer, SyncEvery: fc.SyncEvery,
			},
		})
		snap, err := svc.Get(ctx)
		if err != nil {
			return err
		}
		list, err := peer.NewPeerRepository(database).List(ctx)
		if err != nil {
			return err
		}
		var urls []string
		for _, p := range list {
			if p.Enabled && !p.Builtin {
				urls = append(urls, p.URL)
			}
		}
		out, err := settings.ExportFile(snap.Values, urls)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	},
}

func init() {
	rootCmd.AddCommand(exportConfigCmd)
}
