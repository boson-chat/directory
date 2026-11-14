// Package stackconfig applies the stack config file (BOSON_CONFIG) to the
// running directory: its directory: section replaces the runtime settings
// (name, operator, contact, include_main, main_peer_url, sync_every) and
// manages the peers it lists, reloaded live when the file changes. Without
// BOSON_CONFIG (or while the file is missing) the env values and the
// database apply, as before.
package stackconfig

import (
	"context"
	"time"

	"github.com/boson-chat/directory/config"
	"github.com/boson-chat/directory/internal/configfile"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/settings"
)

// Actor is the audit actor of config.reloaded.
const Actor = "config file"

// Deps are the parts the file reconfigures.
type Deps struct {
	Config   config.AppConfig
	Settings settings.SettingsServiceImpl
	Audit    audit.AuditServiceImpl
	// NormalizeURL validates main_peer_url and peers (federation.NormalizeURL).
	NormalizeURL func(string) (string, error)
	// Reloaded runs after a reload was applied to Settings (module hooks:
	// peers, builtin). Errors are logged.
	Reloaded func(ctx context.Context) error
	Now      func() time.Time
}

type StackConfigServiceImpl interface {
	// Load reads the file at start-up. An invalid file is an error.
	Load(ctx context.Context) error
	// Run watches the file until ctx is done (no-op without BOSON_CONFIG).
	Run(ctx context.Context)
	// Check re-reads the file now.
	Check(ctx context.Context)
	// Status is GET /internal/config/status.
	Status() configfile.Status
	// SetReloaded sets Deps.Reloaded (wired after the modules).
	SetReloaded(fn func(ctx context.Context) error)
}

type StackConfigService struct {
	d Deps
	w *configfile.Watcher[settings.FileValues]
}

func NewStackConfigService(d Deps) *StackConfigService {
	s := &StackConfigService{d: d}
	if d.Config.ConfigFile != "" {
		log := logger.FromCtx(context.Background()).With().Str("component", "config").Logger()
		s.w = configfile.New(configfile.Options[settings.FileValues]{
			Path: d.Config.ConfigFile,
			Load: func(doc *configfile.Document) (settings.FileValues, error) {
				return settings.LoadFile(doc, d.NormalizeURL)
			},
			Apply: s.apply,
			Poll:  d.Config.ConfigPoll,
			Log:   &log,
			Now:   d.Now,
		})
	}
	return s
}

func (s *StackConfigService) SetReloaded(fn func(ctx context.Context) error) { s.d.Reloaded = fn }

func (s *StackConfigService) apply(ctx context.Context, c configfile.Change[settings.FileValues]) {
	log := logger.FromCtx(ctx)
	if c.Next.Present {
		v := c.Next.Value
		if len(v.Unknown) > 0 {
			log.Warn().Strs("keys", v.Unknown).Msg("config file: ignoring unknown keys")
		}
		s.d.Settings.SetFile(&v)
	} else {
		s.d.Settings.SetFile(nil)
	}
	if c.Initial {
		return
	}
	var before, after map[string]any
	if c.Prev.Present {
		before = c.Prev.Value.Flatten()
	}
	if c.Next.Present {
		after = c.Next.Value.Flatten()
	}
	changed := configfile.ChangedKeys(before, after)
	modeSwitch := c.Prev.Present != c.Next.Present
	if len(changed) == 0 && !modeSwitch {
		return // comments / other services' sections only
	}
	if s.d.Reloaded != nil {
		if err := s.d.Reloaded(ctx); err != nil {
			log.Error().Err(err).Msg("config file: applying the reloaded settings")
		}
	}
	meta := map[string]any{"changed": changed, "present": c.Next.Present}
	if modeSwitch {
		meta["mode"] = map[bool]string{true: settings.SourceFile, false: settings.SourceDB}[c.Next.Present]
	}
	log.Info().Strs("changed", changed).Msg("config reloaded")
	s.d.Audit.System(ctx, Actor, "config.reloaded", audit.Target{}, meta)
}

func (s *StackConfigService) Load(ctx context.Context) error {
	if s.w == nil {
		return nil
	}
	return s.w.Load(ctx)
}

func (s *StackConfigService) Run(ctx context.Context) {
	if s.w != nil {
		s.w.Run(ctx)
	}
}

func (s *StackConfigService) Check(ctx context.Context) {
	if s.w != nil {
		s.w.Check(ctx)
	}
}

func (s *StackConfigService) Status() configfile.Status {
	if s.w == nil {
		return configfile.Status{OK: true, RestartRequired: []string{}}
	}
	return s.w.Status()
}
