package networks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/hostname"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/audit"
	"github.com/boson-chat/directory/internal/services/blocks"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/metrics"
)

// Deps are the service's collaborators.
type Deps struct {
	Repo     network.NetworkRepositoryImpl
	Verifier dnsverify.Verifier
	Blocks   blocks.BlockServiceImpl
	Media    media.MediaServiceImpl
	Audit    audit.AuditServiceImpl
	Metrics  *metrics.Metrics
	Options  Options
}

type NetworkService struct {
	repo     network.NetworkRepositoryImpl
	verifier dnsverify.Verifier
	blocks   blocks.BlockServiceImpl
	media    media.MediaServiceImpl
	audit    audit.AuditServiceImpl
	metrics  *metrics.Metrics
	o        Options

	mu        sync.Mutex
	lastCheck map[string]time.Time
}

// DefaultOptions are the production timings.
func DefaultOptions() Options {
	return Options{
		TokenTTL: 72 * time.Hour, LapseAfter: 14 * 24 * time.Hour,
		VerifiedEvery: 24 * time.Hour, PendingEvery: time.Hour, LapsedEvery: 7 * 24 * time.Hour,
		OwnerRateLimit: 30 * time.Second, Now: time.Now,
	}
}

func NewNetworkService(d Deps) *NetworkService {
	o := d.Options
	def := DefaultOptions()
	if o.TokenTTL <= 0 {
		o.TokenTTL = def.TokenTTL
	}
	if o.LapseAfter <= 0 {
		o.LapseAfter = def.LapseAfter
	}
	if o.VerifiedEvery <= 0 {
		o.VerifiedEvery = def.VerifiedEvery
	}
	if o.PendingEvery <= 0 {
		o.PendingEvery = def.PendingEvery
	}
	if o.LapsedEvery <= 0 {
		o.LapsedEvery = def.LapsedEvery
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := d.Metrics
	if m == nil {
		m = metrics.Noop()
	}
	return &NetworkService{
		repo: d.Repo, verifier: d.Verifier, blocks: d.Blocks, media: d.Media, audit: d.Audit,
		metrics: m, o: o, lastCheck: map[string]time.Time{},
	}
}

func (s *NetworkService) Options() Options { return s.o }

func (s *NetworkService) now() time.Time { return s.o.Now().UTC() }

func mapRepo(err error) error {
	switch {
	case errors.Is(err, network.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, network.ErrHostnameTaken):
		return ErrHostnameTaken
	case errors.Is(err, network.ErrBadCursor):
		return ErrBadCursor
	}
	return err
}

func target(n *network.Network) audit.Target {
	return audit.Target{NetworkID: n.ID, Hostname: n.Hostname}
}

// ---- public ----

func encodeCursor(c network.Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*network.Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) > 1024 {
		return nil, ErrBadCursor
	}
	var c network.Cursor
	if err := json.Unmarshal(b, &c); err != nil || c.ID == "" {
		return nil, ErrBadCursor
	}
	return &c, nil
}

// SourceOrigin maps the source query parameter to an origin filter.
func SourceOrigin(src string) string {
	switch src = strings.TrimSpace(src); {
	case src == "" || src == "all":
		return ""
	case src == network.OriginLocal:
		return network.OriginLocal
	case strings.HasPrefix(src, "peer:"):
		return src
	default:
		return network.PeerOrigin(src)
	}
}

func (s *NetworkService) List(ctx context.Context, p *principal.Principal, q ListQuery) (*Page, error) {
	if q.Sort == "" {
		q.Sort = network.SortUsers
	}
	if !network.ValidSort(q.Sort) {
		return nil, apperr.Invalid("sort must be users, active, newest or name")
	}
	switch {
	case q.Limit <= 0:
		q.Limit = 25
	case q.Limit > 100:
		q.Limit = 100
	}
	f := network.ListFilter{
		Query: q.Query, Language: q.Language, Tag: strings.ToLower(q.Tag), IncludeNSFW: q.IncludeNSFW,
		Source: SourceOrigin(q.Source), Sort: q.Sort, Limit: q.Limit + 1,
	}
	if q.Owner != "" {
		if p == nil || !(p.Has(principal.PermUsersView) || p.IsOperator() || (p.Kind == principal.KindAccount && p.AuthUserID == q.Owner)) {
			return nil, apperr.New(403, "forbidden", "listing someone's networks needs the users.view permission")
		}
		f.OwnerAuthUserID, f.AnyStatus, f.IncludeNSFW = q.Owner, true, true
	}
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, err
		}
		if c.Sort != q.Sort {
			return nil, ErrBadCursor
		}
		f.After = c
	}
	rows, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, mapRepo(err)
	}
	page := &Page{Networks: rows}
	if len(rows) > q.Limit {
		page.Networks = rows[:q.Limit]
		last := page.Networks[q.Limit-1]
		page.NextCursor = encodeCursor(network.Cursor{Sort: q.Sort, Keys: network.KeysOf(last, q.Sort), ID: last.ID})
	}
	if q.Cursor == "" && len(rows) <= q.Limit {
		page.TotalEstimate = int64(len(rows))
	} else if page.TotalEstimate, err = s.repo.Count(ctx, f); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *NetworkService) visible(p *principal.Principal, n *network.Network) bool {
	return n.Status == network.StatusVerified || s.CanManage(p, n) || p.IsOperator()
}

func (s *NetworkService) Get(ctx context.Context, p *principal.Principal, id string) (*network.Network, error) {
	n, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepo(err)
	}
	if !s.visible(p, n) {
		return nil, ErrNotFound
	}
	return n, nil
}

func (s *NetworkService) Lookup(ctx context.Context, p *principal.Principal, host string) (*network.Network, error) {
	h, err := hostname.Normalize(host)
	if err != nil {
		return nil, apperr.New(400, "invalid_hostname", err.Error())
	}
	n, err := s.repo.GetByHostname(ctx, h)
	if err != nil {
		return nil, mapRepo(err)
	}
	if !s.visible(p, n) {
		return nil, ErrNotFound
	}
	return n, nil
}

func (s *NetworkService) Facets(ctx context.Context, includeNSFW bool) ([]network.Facet, []network.Facet, error) {
	return s.repo.Facets(ctx, includeNSFW, 50)
}

// ---- owner ----

func (s *NetworkService) CanManage(p *principal.Principal, n *network.Network) bool {
	if p == nil || n == nil {
		return false
	}
	if p.IsOperator() {
		return true
	}
	if !n.IsLocal() {
		return false
	}
	switch p.Kind {
	case principal.KindAccount:
		return n.OwnerAuthUserID != nil && *n.OwnerAuthUserID == p.AuthUserID
	case principal.KindNetworkKey:
		return n.ManagementKeyHash != nil && p.KeyHash != "" && *n.ManagementKeyHash == p.KeyHash
	}
	return false
}

// owns is CanManage without the operator override (idempotent create).
func owns(p *principal.Principal, n *network.Network) bool {
	switch p.Kind {
	case principal.KindAccount:
		return n.OwnerAuthUserID != nil && *n.OwnerAuthUserID == p.AuthUserID
	case principal.KindNetworkKey:
		return n.ManagementKeyHash != nil && *n.ManagementKeyHash == p.KeyHash
	}
	return false
}

func (s *NetworkService) Mine(ctx context.Context, p *principal.Principal) ([]*network.Network, error) {
	if p == nil {
		return nil, ErrUnauthenticate
	}
	if p.Kind == principal.KindInternal {
		return nil, errInternalOwner
	}
	f := network.ListFilter{AnyStatus: true, IncludeNSFW: true, Sort: network.SortNewest, Limit: 500}
	if p.Kind == principal.KindNetworkKey {
		f.KeyHash = p.KeyHash
	} else {
		f.OwnerAuthUserID = p.AuthUserID
	}
	return s.repo.List(ctx, f)
}

var (
	tagRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)
	langRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
)

func cleanName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len([]rune(v)) > 64 {
		return "", apperr.Invalid("the name must be 1–64 characters")
	}
	return v, nil
}

func cleanDescription(v string) (string, error) {
	v = strings.TrimSpace(v)
	if len([]rune(v)) > 500 {
		return "", apperr.Invalid("the description must be at most 500 characters")
	}
	return v, nil
}

func cleanTags(in []string) ([]string, error) {
	if len(in) > 8 {
		return nil, apperr.Invalid("at most 8 tags")
	}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if !tagRe.MatchString(t) {
			return nil, apperr.Invalid("tags are lowercase words like gaming or open-source")
		}
		if !contains(out, t) {
			out = append(out, t)
		}
	}
	return out, nil
}

func cleanLanguages(in []string) ([]string, error) {
	if len(in) > 5 {
		return nil, apperr.Invalid("at most 5 languages")
	}
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.TrimSpace(l)
		if !langRe.MatchString(l) {
			return nil, apperr.Invalid("languages are codes like en or pt-BR")
		}
		if !contains(out, l) {
			out = append(out, l)
		}
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// cleanURL returns nil for empty, else requires https.
func cleanURL(v *string, name string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil, nil
	}
	u, err := url.Parse(t)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(t) > 2048 {
		return nil, apperr.Invalid(name + " must be an https:// URL")
	}
	return &t, nil
}

// NewToken returns a fresh verification token (32 random bytes, base64url).
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

var errInternalOwner = apperr.New(403, "forbidden", "an internal token can't own listings; register with a Network key or an account")

func (s *NetworkService) Create(ctx context.Context, p *principal.Principal, sub Submission) (*CreateResult, error) {
	if p == nil {
		return nil, ErrUnauthenticate
	}
	if p.Kind == principal.KindInternal {
		return nil, errInternalOwner
	}
	host, err := hostname.Normalize(sub.Hostname)
	if err != nil {
		return nil, apperr.New(400, "invalid_hostname", err.Error()+"; its TXT record proves you run it")
	}
	if sub.Port < 1 || sub.Port > 65535 {
		return nil, apperr.Invalid("the port must be between 1 and 65535")
	}
	if b, err := s.blocks.Match(ctx, host); err != nil {
		return nil, err
	} else if b != nil {
		return nil, ErrBlocked
	}
	name := ""
	if sub.Name != nil {
		name = *sub.Name
	}
	n := &network.Network{Hostname: host, Port: sub.Port, TLS: sub.TLS == nil || *sub.TLS, Origin: network.OriginLocal}
	if n.Name, err = cleanName(name); err != nil {
		return nil, err
	}
	if sub.Description != nil {
		if n.Description, err = cleanDescription(*sub.Description); err != nil {
			return nil, err
		}
	}
	if n.Tags, err = cleanTags(sub.Tags); err != nil {
		return nil, err
	}
	if n.Languages, err = cleanLanguages(sub.Languages); err != nil {
		return nil, err
	}
	n.IsNSFW = sub.IsNSFW != nil && *sub.IsNSFW
	if n.WebsiteURL, err = cleanURL(sub.WebsiteURL, "the website"); err != nil {
		return nil, err
	}
	if n.AuthURL, err = cleanURL(sub.AuthURL, "the sign-in URL"); err != nil {
		return nil, err
	}

	if cur, err := s.repo.GetByHostname(ctx, host); err == nil {
		switch {
		case owns(p, cur):
			return &CreateResult{Network: cur}, nil
		case !cur.IsLocal() && cur.Status != network.StatusBlocked:
			// A local listing beats any imported one.
			if err := s.repo.Delete(ctx, cur.ID); err != nil && !errors.Is(err, network.ErrNotFound) {
				return nil, err
			}
			s.audit.Record(ctx, p, "network.replace_imported", target(cur), map[string]any{"origin": cur.Origin})
		case cur.IsLocal() && cur.Status == network.StatusPending && s.now().Sub(cur.TokenIssuedAt) > s.o.TokenTTL:
			// An abandoned claim (never verified, token expired) doesn't
			// hold the hostname; DNS decides who runs it.
			if err := s.repo.Delete(ctx, cur.ID); err != nil && !errors.Is(err, network.ErrNotFound) {
				return nil, err
			}
			s.dropImages(ctx, cur)
			s.audit.Record(ctx, p, "network.replace_expired", target(cur), nil)
		default:
			return nil, ErrHostnameTaken
		}
	} else if !errors.Is(err, network.ErrNotFound) {
		return nil, err
	}

	if p.Kind == principal.KindNetworkKey {
		h := p.KeyHash
		n.ManagementKeyHash = &h
	} else {
		id := p.AuthUserID
		n.OwnerAuthUserID = &id
	}
	n.Status = network.StatusPending
	n.VerificationToken = NewToken()
	n.TokenIssuedAt = s.now()
	n.CreatedAt = s.now()
	if err := s.repo.Create(ctx, n); err != nil {
		return nil, mapRepo(err)
	}
	s.audit.Record(ctx, p, "network.create", target(n), map[string]any{"port": n.Port, "tls": n.TLS})
	s.metrics.Registration(ctx, string(p.Kind))
	return &CreateResult{Network: n, Created: true}, nil
}

// manageable loads id and checks p may manage it.
func (s *NetworkService) manageable(ctx context.Context, p *principal.Principal, id string, requireLocal bool) (*network.Network, error) {
	if p == nil {
		return nil, ErrUnauthenticate
	}
	n, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepo(err)
	}
	if !s.CanManage(p, n) {
		if !s.visible(p, n) {
			return nil, ErrNotFound
		}
		if !n.IsLocal() {
			return nil, ErrNotLocal
		}
		return nil, ErrNotOwner
	}
	if requireLocal && !n.IsLocal() {
		return nil, ErrNotLocal
	}
	return n, nil
}

func (s *NetworkService) Update(ctx context.Context, p *principal.Principal, id string, e Edit) (*network.Network, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	var cols []string
	changed := map[string]any{}
	if e.Name != nil {
		if n.Name, err = cleanName(*e.Name); err != nil {
			return nil, err
		}
		cols = append(cols, "name")
	}
	if e.Description != nil {
		if n.Description, err = cleanDescription(*e.Description); err != nil {
			return nil, err
		}
		cols = append(cols, "description")
	}
	if e.Tags != nil {
		if n.Tags, err = cleanTags(*e.Tags); err != nil {
			return nil, err
		}
		cols = append(cols, "tags")
	}
	if e.Languages != nil {
		if n.Languages, err = cleanLanguages(*e.Languages); err != nil {
			return nil, err
		}
		cols = append(cols, "languages")
	}
	if e.IsNSFW != nil {
		n.IsNSFW = *e.IsNSFW
		cols = append(cols, "is_nsfw")
	}
	if e.WebsiteURL != nil {
		if n.WebsiteURL, err = cleanURL(e.WebsiteURL, "the website"); err != nil {
			return nil, err
		}
		cols = append(cols, "website_url")
	}
	if e.AuthURL != nil {
		if n.AuthURL, err = cleanURL(e.AuthURL, "the sign-in URL"); err != nil {
			return nil, err
		}
		cols = append(cols, "auth_url")
	}
	if len(cols) == 0 {
		return n, nil
	}
	if err := s.repo.Update(ctx, n, cols...); err != nil {
		return nil, mapRepo(err)
	}
	changed["fields"] = cols
	s.audit.Record(ctx, p, "network.update", target(n), changed)
	return n, nil
}

func (s *NetworkService) dropImages(ctx context.Context, n *network.Network) {
	for _, k := range []*string{n.IconKey, n.BannerKey} {
		if k != nil {
			if err := s.media.Remove(ctx, *k); err != nil {
				logger.FromCtx(ctx).Warn().Err(err).Str("key", *k).Msg("removing listing image")
			}
		}
	}
}

func (s *NetworkService) Delete(ctx context.Context, p *principal.Principal, id string) error {
	n, err := s.manageable(ctx, p, id, false)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, n.ID); err != nil {
		return mapRepo(err)
	}
	if n.IsLocal() {
		s.dropImages(ctx, n)
	}
	s.audit.Record(ctx, p, "network.delete", target(n), map[string]any{"origin": n.Origin, "status": n.Status})
	return nil
}

func (s *NetworkService) RegenerateToken(ctx context.Context, p *principal.Principal, id string) (*network.Network, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	if n.Status == network.StatusBlocked {
		return nil, ErrBlocked
	}
	n.VerificationToken = NewToken()
	n.TokenIssuedAt = s.now()
	n.Status = network.StatusPending
	n.LastCheckedAt, n.LastVerifiedAt = nil, nil
	if err := s.repo.Update(ctx, n, "verification_token", "token_issued_at", "status", "last_checked_at", "last_verified_at"); err != nil {
		return nil, mapRepo(err)
	}
	s.audit.Record(ctx, p, "network.regenerate_token", target(n), nil)
	return n, nil
}

func (s *NetworkService) SetImage(ctx context.Context, p *principal.Principal, id string, kind ImageKind, raw []byte) (*network.Network, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	slot, ns, w, h := &n.IconKey, "network-icons", media.IconSize, media.IconSize
	col := "icon_key"
	if kind == ImageBanner {
		slot, ns, w, h, col = &n.BannerKey, "network-banners", media.BannerWidth, media.BannerHeight, "banner_key"
	}
	prev := ""
	if *slot != nil {
		prev = **slot
	}
	if raw == nil {
		*slot = nil
	} else {
		key, err := s.media.Process(ctx, ns, n.ID, raw, "", w, h)
		switch {
		case errors.Is(err, media.ErrTooLarge):
			return nil, apperr.New(413, "image_too_large", "the image is too large (5 MiB, 24 megapixels at most)")
		case errors.Is(err, media.ErrUnsupportedImage):
			return nil, apperr.New(400, "unsupported_image", "unsupported or invalid image (PNG, JPEG, GIF or WebP)")
		case err != nil:
			return nil, err
		}
		*slot = &key
	}
	if err := s.repo.Update(ctx, n, col); err != nil {
		if *slot != nil {
			_ = s.media.Remove(ctx, **slot)
		}
		return nil, mapRepo(err)
	}
	if prev != "" && (*slot == nil || **slot != prev) {
		_ = s.media.Remove(ctx, prev)
	}
	action := "network.set_" + string(kind)
	if raw == nil {
		action = "network.clear_" + string(kind)
	}
	s.audit.Record(ctx, p, action, target(n), nil)
	return n, nil
}

// ---- verification ----

// Transition is the state machine: the status and last_verified_at after
// a check of n that succeeded (or not) at now.
func Transition(n *network.Network, success bool, now time.Time, lapseAfter time.Duration) (string, *time.Time) {
	if n.Status == network.StatusBlocked {
		return network.StatusBlocked, nil
	}
	if success {
		return network.StatusVerified, &now
	}
	if n.Status == network.StatusVerified {
		// Measured from the last success, not the last check: a listing
		// checked daily and failing daily still lapses after 14 days.
		if n.LastVerifiedAt == nil || now.Sub(*n.LastVerifiedAt) > lapseAfter {
			return network.StatusLapsed, nil
		}
		return network.StatusVerified, nil
	}
	return n.Status, nil
}

// ModeFor is strict for a first verification, lenient for re-checks.
func ModeFor(n *network.Network) dnsverify.Mode {
	if n.Status == network.StatusPending {
		return dnsverify.ModeStrict
	}
	return dnsverify.ModeLenient
}

// tokenExpired: a local pending listing must verify within TokenTTL.
// Imported listings carry their origin's token and aren't subject to it.
func (s *NetworkService) tokenExpired(n *network.Network) bool {
	return n.IsLocal() && n.Status == network.StatusPending && s.now().Sub(n.TokenIssuedAt) > s.o.TokenTTL
}

func (s *NetworkService) check(ctx context.Context, n *network.Network) (*network.Network, dnsverify.Report, error) {
	mode := ModeFor(n)
	report, err := s.verifier.Verify(ctx, n.Hostname, n.VerificationToken, mode)
	if err != nil {
		s.metrics.Verification(ctx, "error")
		return n, report, err
	}
	now := s.now()
	status, verifiedAt := Transition(n, report.Success, now, s.o.LapseAfter)
	ok, err := s.repo.RecordVerify(ctx, network.VerifyResult{
		ID: n.ID, CheckedToken: n.VerificationToken, Status: status, CheckedAt: now, VerifiedAt: verifiedAt,
	})
	if err != nil {
		return n, report, err
	}
	outcome := "miss"
	if report.Success {
		outcome = "match"
	}
	s.metrics.Verification(ctx, outcome)
	if ok {
		if status != n.Status {
			s.audit.System(ctx, "verify", "network.status."+status, target(n), map[string]any{"from": n.Status})
			s.metrics.StatusChange(ctx, n.Status, status)
		}
		n.Status, n.LastCheckedAt = status, &now
		if verifiedAt != nil {
			n.LastVerifiedAt = verifiedAt
		}
	}
	if fresh, err := s.repo.Get(ctx, n.ID); err == nil {
		n = fresh
	}
	return n, report, nil
}

func (s *NetworkService) Verify(ctx context.Context, p *principal.Principal, id string) (*VerifyOutcome, error) {
	n, err := s.manageable(ctx, p, id, false)
	if err != nil {
		return nil, err
	}
	if n.Status == network.StatusBlocked {
		return nil, ErrBlocked
	}
	if s.tokenExpired(n) {
		return nil, ErrTokenExpired
	}
	if !s.allowCheck(n.ID) {
		return nil, ErrRateLimited
	}
	n, report, err := s.check(ctx, n)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, p, "network.verify", target(n), map[string]any{"success": report.Success, "status": n.Status})
	return &VerifyOutcome{Network: n, Report: report}, nil
}

// allowCheck rate-limits owner-triggered checks per network.
func (s *NetworkService) allowCheck(id string) bool {
	if s.o.OwnerRateLimit <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.o.Now()
	if last, ok := s.lastCheck[id]; ok && now.Sub(last) < s.o.OwnerRateLimit {
		return false
	}
	s.lastCheck[id] = now
	if len(s.lastCheck) > 10000 {
		for k, t := range s.lastCheck {
			if now.Sub(t) >= s.o.OwnerRateLimit {
				delete(s.lastCheck, k)
			}
		}
	}
	return true
}

// RetryAfter is how long until id may be checked again by its owner.
func (s *NetworkService) RetryAfter(id string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.lastCheck[id]; ok {
		if d := s.o.OwnerRateLimit - s.o.Now().Sub(last); d > 0 {
			return d
		}
	}
	return 0
}

func (s *NetworkService) CheckOne(ctx context.Context, n *network.Network) (*network.Network, dnsverify.Report, error) {
	if n.Status == network.StatusBlocked || s.tokenExpired(n) {
		return n, dnsverify.Report{}, nil
	}
	return s.check(ctx, n)
}

func (s *NetworkService) VerifyDue(ctx context.Context) (int, error) {
	checked := 0
	for round := 0; round < 50; round++ {
		due, err := s.repo.DueForVerify(ctx, network.VerifyDue{
			Now: s.now(), VerifiedEvery: s.o.VerifiedEvery, PendingEvery: s.o.PendingEvery,
			LapsedEvery: s.o.LapsedEvery, TokenTTL: s.o.TokenTTL, Limit: 100,
		})
		if err != nil {
			return checked, err
		}
		if len(due) == 0 {
			return checked, nil
		}
		progressed := 0
		for _, n := range due {
			if ctx.Err() != nil {
				return checked, ctx.Err()
			}
			if _, _, err := s.CheckOne(ctx, n); err != nil {
				logger.FromCtx(ctx).Warn().Err(err).Str("hostname", n.Hostname).Msg("verify check failed")
				// Record the attempt so a failing row doesn't spin.
				now := s.now()
				if _, err := s.repo.RecordVerify(ctx, network.VerifyResult{ID: n.ID, CheckedToken: n.VerificationToken, Status: n.Status, CheckedAt: now}); err != nil {
					return checked, err
				}
			}
			checked++
			progressed++
		}
		if progressed == 0 {
			return checked, nil
		}
	}
	return checked, nil
}

func (s *NetworkService) Moderate(ctx context.Context, p *principal.Principal, id string, action ModerationAction, reason string) (*network.Network, *dnsverify.Report, error) {
	if !p.IsOperator() {
		return nil, nil, apperr.New(403, "forbidden", "this needs the directory.manage permission")
	}
	n, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, nil, mapRepo(err)
	}
	meta := map[string]any{"from": n.Status}
	if reason = strings.TrimSpace(reason); reason != "" {
		meta["reason"] = reason
	}
	switch action {
	case ModerateBlock:
		if n.Status != network.StatusBlocked {
			n.Status = network.StatusBlocked
			if err := s.repo.Update(ctx, n, "status"); err != nil {
				return nil, nil, mapRepo(err)
			}
		}
	case ModerateUnblock:
		if n.Status == network.StatusBlocked {
			if b, err := s.blocks.Match(ctx, n.Hostname); err != nil {
				return nil, nil, err
			} else if b != nil {
				return nil, nil, apperr.New(409, "blocked_by_pattern", "the hostname matches the block "+b.Pattern+"; remove that block instead")
			}
			n.Status = network.StatusPending
			if n.LastVerifiedAt != nil {
				n.Status = network.StatusLapsed
			}
			n.LastCheckedAt = nil
			if err := s.repo.Update(ctx, n, "status", "last_checked_at"); err != nil {
				return nil, nil, mapRepo(err)
			}
		}
	case ModerateRecheck:
		if n.Status == network.StatusBlocked {
			return nil, nil, ErrBlocked
		}
		n, report, err := s.check(ctx, n)
		if err != nil {
			return nil, nil, err
		}
		meta["success"] = report.Success
		s.audit.Record(ctx, p, "network.recheck", target(n), meta)
		return n, &report, nil
	default:
		return nil, nil, apperr.Invalid("action must be block, unblock or recheck")
	}
	s.audit.Record(ctx, p, "network."+string(action), target(n), meta)
	return n, nil, nil
}
