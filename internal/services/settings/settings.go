// Package settings holds the directory's runtime settings: the values an
// operator changes from the stack admin panel (or PATCH
// /v1/admin/settings) without editing .env or restarting.
//
// Each setting has an env default (DIRECTORY_NAME, DIRECTORY_OPERATOR,
// DIRECTORY_CONTACT, DIRECTORY_INCLUDE_MAIN, DIRECTORY_MAIN_PEER,
// DIRECTORY_SYNC_EVERY). An override is stored in the directory_settings
// row and wins over the env until it is reset (PATCH with null), so env
// values are only defaults. Secrets and infrastructure (database, auth,
// internal token, media storage) stay env-only.
//
// Readers use Current, an in-memory copy refreshed after every change in
// this process and at most CacheTTL old elsewhere (other replicas).
//
// With the stack config file (BOSON_CONFIG, file.go) present, its
// directory: section is the source of truth instead (SetFile): file > env
// > default, the row is ignored and Update answers 409
// managed_by_config_file.
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/boson-chat/directory/internal/apperr"
	repo "github.com/boson-chat/directory/internal/db/repositories/settings"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
)

// Setting names (JSON keys of the operator API).
const (
	FieldName        = "name"
	FieldOperator    = "operator"
	FieldContact     = "contact"
	FieldIncludeMain = "include_main"
	FieldMainPeerURL = "main_peer_url"
	FieldSyncEvery   = "sync_every"
)

// Fields lists every runtime setting in display order.
var Fields = []string{FieldName, FieldOperator, FieldContact, FieldIncludeMain, FieldMainPeerURL, FieldSyncEvery}

// Limits.
const (
	MaxNameLen     = 100
	MaxOperatorLen = 100
	MaxContactLen  = 200
	MinSyncEvery   = time.Minute
	MaxSyncEvery   = 7 * 24 * time.Hour
)

// CacheTTL bounds how stale Current may be (changes made by another replica).
var CacheTTL = 15 * time.Second

// Values are effective settings (override, else env default).
type Values struct {
	Name        string
	Operator    string
	Contact     string
	IncludeMain bool
	// MainPeerURL is the main directory's base URL ("" = none).
	MainPeerURL string
	SyncEvery   time.Duration
}

// Opt is a PATCH field: absent (Set=false), null (reset to the env
// default) or a value.
type Opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

// Value returns an Opt holding v.
func Value[T any](v T) Opt[T] { return Opt[T]{Set: true, V: v} }

// Reset returns an Opt that resets the field to its env default.
func Reset[T any]() Opt[T] { return Opt[T]{Set: true, Null: true} }

// Patch is PATCH /v1/admin/settings. SyncEvery is a Go duration ("10m").
type Patch struct {
	Name        Opt[string] `json:"name"`
	Operator    Opt[string] `json:"operator"`
	Contact     Opt[string] `json:"contact"`
	IncludeMain Opt[bool]   `json:"include_main"`
	MainPeerURL Opt[string] `json:"main_peer_url"`
	SyncEvery   Opt[string] `json:"sync_every"`
}

// Has reports whether the patch touches field.
func (p Patch) Has(field string) bool {
	switch field {
	case FieldName:
		return p.Name.Set
	case FieldOperator:
		return p.Operator.Set
	case FieldContact:
		return p.Contact.Set
	case FieldIncludeMain:
		return p.IncludeMain.Set
	case FieldMainPeerURL:
		return p.MainPeerURL.Set
	case FieldSyncEvery:
		return p.SyncEvery.Set
	}
	return false
}

// Snapshot is GET /v1/admin/settings.
type Snapshot struct {
	Values   Values
	Defaults Values
	// Overridden lists the fields set at runtime (not the env default).
	Overridden []string
	UpdatedAt  time.Time
	UpdatedBy  *string
	// Source is SourceFile (stack config file) or SourceDB.
	Source string
}

type SettingsServiceImpl interface {
	// Current returns the effective settings (cached; see CacheTTL).
	Current() Values
	// Defaults are the env values.
	Defaults() Values
	// Load reads the row into the cache (start-up).
	Load(ctx context.Context) error
	// Get reads the row afresh.
	Get(ctx context.Context) (*Snapshot, error)
	// Update validates and stores p, refreshes the cache and audits the
	// change. It returns the settings before and after.
	Update(ctx context.Context, pr *principal.Principal, p Patch) (before, after Values, err error)
	// SeededPeers / MarkSeeded track DIRECTORY_PEERS URLs already seeded.
	SeededPeers(ctx context.Context) ([]string, error)
	MarkSeeded(ctx context.Context, urls []string) error
	// SetFile switches to file mode with the file's values (nil: back to
	// the database). File returns them (nil in database mode).
	SetFile(f *FileValues)
	File() *FileValues
}

// Deps are the service's collaborators.
type Deps struct {
	Repo     repo.SettingsRepositoryImpl
	Audit    audit.AuditServiceImpl
	Defaults Values
	// NormalizeURL validates main_peer_url (federation.NormalizeURL).
	NormalizeURL func(string) (string, error)
	Now          func() time.Time
}

type SettingsService struct {
	d    Deps
	file atomic.Pointer[FileValues]

	mu       sync.Mutex // serialises Update and cache refresh
	cur      Values
	loadedAt time.Time
	loaded   bool
}

func NewSettingsService(d Deps) *SettingsService {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NormalizeURL == nil {
		d.NormalizeURL = func(s string) (string, error) { return strings.TrimRight(strings.TrimSpace(s), "/"), nil }
	}
	return &SettingsService{d: d, cur: d.Defaults}
}

func (s *SettingsService) Defaults() Values { return s.d.Defaults }

// effective applies row's overrides to the env defaults.
func (s *SettingsService) effective(r *repo.Row) Values {
	v := s.d.Defaults
	if r == nil {
		return v
	}
	if r.Name != nil {
		v.Name = *r.Name
	}
	if r.Operator != nil {
		v.Operator = *r.Operator
	}
	if r.Contact != nil {
		v.Contact = *r.Contact
	}
	if r.IncludeMain != nil {
		v.IncludeMain = *r.IncludeMain
	}
	if r.MainPeerURL != nil {
		v.MainPeerURL = *r.MainPeerURL
	}
	if r.SyncEverySeconds != nil {
		v.SyncEvery = time.Duration(*r.SyncEverySeconds) * time.Second
	}
	return v
}

func overridden(r *repo.Row) []string {
	out := []string{}
	set := map[string]bool{
		FieldName: r.Name != nil, FieldOperator: r.Operator != nil, FieldContact: r.Contact != nil,
		FieldIncludeMain: r.IncludeMain != nil, FieldMainPeerURL: r.MainPeerURL != nil, FieldSyncEvery: r.SyncEverySeconds != nil,
	}
	for _, f := range Fields {
		if set[f] {
			out = append(out, f)
		}
	}
	return out
}

func (s *SettingsService) Load(ctx context.Context) error {
	r, err := s.d.Repo.Get(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cur, s.loadedAt, s.loaded = s.effective(r), s.d.Now(), true
	s.mu.Unlock()
	return nil
}

func (s *SettingsService) SetFile(f *FileValues) {
	if f != nil {
		c := *f
		f = &c
	}
	s.file.Store(f)
	s.mu.Lock()
	s.loaded = false // database mode again: re-read the row
	s.mu.Unlock()
}

func (s *SettingsService) File() *FileValues { return s.file.Load() }

func (s *SettingsService) Current() Values {
	if f := s.file.Load(); f != nil {
		return f.apply(s.d.Defaults)
	}
	s.mu.Lock()
	fresh := s.loaded && s.d.Now().Sub(s.loadedAt) < CacheTTL
	cur := s.cur
	s.mu.Unlock()
	if fresh {
		return cur
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Load(ctx); err != nil {
		logger.FromCtx(ctx).Warn().Err(err).Msg("settings: refresh failed; using the last known values")
		s.mu.Lock()
		s.loadedAt = s.d.Now() // don't hammer a failing database
		s.mu.Unlock()
		return cur
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *SettingsService) Get(ctx context.Context) (*Snapshot, error) {
	if f := s.file.Load(); f != nil {
		return &Snapshot{Values: f.apply(s.d.Defaults), Defaults: s.d.Defaults, Overridden: f.fields(), Source: SourceFile}, nil
	}
	r, err := s.d.Repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	v := s.effective(r)
	s.mu.Lock()
	s.cur, s.loadedAt, s.loaded = v, s.d.Now(), true
	s.mu.Unlock()
	return &Snapshot{Values: v, Defaults: s.d.Defaults, Overridden: overridden(r), UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy, Source: SourceDB}, nil
}

func cleanText(field, v string, maxLen int, required bool) (string, error) {
	v = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if required && v == "" {
		return "", apperr.Invalid(fmt.Sprintf("%s can't be empty (send null to use the default)", field))
	}
	if len([]rune(v)) > maxLen {
		return "", apperr.Invalid(fmt.Sprintf("%s must be at most %d characters", field, maxLen))
	}
	return v, nil
}

// ParseSyncEvery parses and bounds a sync interval ("10m", "1h").
func ParseSyncEvery(v string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, apperr.Invalid("sync_every must be a duration such as 10m or 1h")
	}
	if d < MinSyncEvery || d > MaxSyncEvery {
		return 0, apperr.Invalid("sync_every must be between 1m and 168h")
	}
	return d.Truncate(time.Second), nil
}

// FormatDuration renders d without trailing zero units ("10m", "1h30m").
func FormatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ErrNoChanges is a PATCH with no known field.
var ErrNoChanges = apperr.Invalid("nothing to change: send at least one of " + strings.Join(Fields, ", "))

func (s *SettingsService) Update(ctx context.Context, pr *principal.Principal, p Patch) (Values, Values, error) {
	if s.file.Load() != nil {
		cur := s.Current()
		return cur, cur, ErrManagedByConfigFile
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.d.Repo.Get(ctx)
	if err != nil {
		return Values{}, Values{}, err
	}
	before := s.effective(r)
	var cols []string
	text := func(o Opt[string], field string, maxLen int, required bool, dst **string) error {
		if !o.Set {
			return nil
		}
		cols = append(cols, field)
		if o.Null {
			*dst = nil
			return nil
		}
		v, err := cleanText(field, o.V, maxLen, required)
		if err != nil {
			return err
		}
		*dst = &v
		return nil
	}
	if err := text(p.Name, FieldName, MaxNameLen, true, &r.Name); err != nil {
		return before, before, err
	}
	if err := text(p.Operator, FieldOperator, MaxOperatorLen, false, &r.Operator); err != nil {
		return before, before, err
	}
	if err := text(p.Contact, FieldContact, MaxContactLen, false, &r.Contact); err != nil {
		return before, before, err
	}
	if p.IncludeMain.Set {
		cols = append(cols, FieldIncludeMain)
		r.IncludeMain = nil
		if !p.IncludeMain.Null {
			v := p.IncludeMain.V
			r.IncludeMain = &v
		}
	}
	if p.MainPeerURL.Set {
		cols = append(cols, FieldMainPeerURL)
		r.MainPeerURL = nil
		if !p.MainPeerURL.Null {
			v := strings.TrimSpace(p.MainPeerURL.V)
			if v != "" {
				if v, err = s.d.NormalizeURL(v); err != nil {
					return before, before, apperr.Invalid("main_peer_url: " + err.Error())
				}
			}
			r.MainPeerURL = &v
		}
	}
	if p.SyncEvery.Set {
		cols = append(cols, "sync_every_seconds")
		r.SyncEverySeconds = nil
		if !p.SyncEvery.Null {
			d, err := ParseSyncEvery(p.SyncEvery.V)
			if err != nil {
				return before, before, err
			}
			secs := int(d / time.Second)
			r.SyncEverySeconds = &secs
		}
	}
	if len(cols) == 0 {
		return before, before, ErrNoChanges
	}
	if pr != nil {
		by := pr.Username
		if by == "" {
			by = pr.ActorID()
		}
		r.UpdatedBy = &by
	}
	if err := s.d.Repo.Save(ctx, r, cols...); err != nil {
		return before, before, err
	}
	after := s.effective(r)
	s.cur, s.loadedAt, s.loaded = after, s.d.Now(), true

	changes := map[string]any{}
	for _, f := range Fields {
		if !p.Has(f) {
			continue
		}
		o, n := before.get(f), after.get(f)
		entry := map[string]any{"from": o, "to": n}
		if (f == FieldName && p.Name.Null) || (f == FieldOperator && p.Operator.Null) || (f == FieldContact && p.Contact.Null) ||
			(f == FieldIncludeMain && p.IncludeMain.Null) || (f == FieldMainPeerURL && p.MainPeerURL.Null) || (f == FieldSyncEvery && p.SyncEvery.Null) {
			entry["reset"] = true
		}
		changes[f] = entry
	}
	if s.d.Audit != nil {
		s.d.Audit.Record(ctx, pr, "settings.update", audit.Target{}, map[string]any{"changes": changes})
	}
	return before, after, nil
}

// get returns field's value for audit/diff (durations as strings).
func (v Values) get(field string) any {
	switch field {
	case FieldName:
		return v.Name
	case FieldOperator:
		return v.Operator
	case FieldContact:
		return v.Contact
	case FieldIncludeMain:
		return v.IncludeMain
	case FieldMainPeerURL:
		return v.MainPeerURL
	case FieldSyncEvery:
		return FormatDuration(v.SyncEvery)
	}
	return nil
}

func (s *SettingsService) SeededPeers(ctx context.Context) ([]string, error) {
	r, err := s.d.Repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return r.SeededPeers, nil
}

func (s *SettingsService) MarkSeeded(ctx context.Context, urls []string) error {
	if len(urls) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.d.Repo.Get(ctx)
	if err != nil {
		return err
	}
	list := append([]string(nil), r.SeededPeers...)
	added := false
	for _, u := range urls {
		if !slices.Contains(list, u) {
			list, added = append(list, u), true
		}
	}
	if !added {
		return nil
	}
	return s.d.Repo.SetSeededPeers(ctx, list)
}

// Static is a SettingsServiceImpl with fixed values (tests, tools).
type Static struct{ V Values }

func (s Static) Current() Values            { return s.V }
func (s Static) Defaults() Values           { return s.V }
func (s Static) Load(context.Context) error { return nil }
func (s Static) Get(context.Context) (*Snapshot, error) {
	return &Snapshot{Values: s.V, Defaults: s.V, Overridden: []string{}, Source: SourceDB}, nil
}
func (s Static) Update(context.Context, *principal.Principal, Patch) (Values, Values, error) {
	return s.V, s.V, errors.New("settings: static settings can't be changed")
}
func (s Static) SetFile(*FileValues)                           {}
func (s Static) File() *FileValues                             { return nil }
func (s Static) SeededPeers(context.Context) ([]string, error) { return nil, nil }
func (s Static) MarkSeeded(context.Context, []string) error    { return nil }
