package federation

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/hostname"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/boson-chat/directory/internal/services/theme"
)

// Feed paging.
const (
	DefaultFeedLimit = 500
	MaxFeedLimit     = 1000
	// MaxFeedThemeBytes ends a page early once its themes add up to this
	// (up to 16 KB each), so a page stays well under a peer's response
	// cap (DIRECTORY_PEER_MAX_BYTES, 8 MiB).
	MaxFeedThemeBytes = 3 << 20
)

// FeedNetwork is the public part of a listing as GET /v1/feed exports it.
// Owner data (account, network key hash) and probe bookkeeping are never
// exported; peers run their own DNS and health checks.
type FeedNetwork struct {
	ID          string   `json:"id"`
	Hostname    string   `json:"hostname,omitempty"`
	Port        int      `json:"port,omitempty"`
	TLS         bool     `json:"tls"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	IsNSFW      bool     `json:"is_nsfw"`
	WebsiteURL  *string  `json:"website_url,omitempty"`
	AuthURL     *string  `json:"auth_url,omitempty"`
	IconURL     *string  `json:"icon_url,omitempty"`
	BannerURL   *string  `json:"banner_url,omitempty"`
	// Theme is the listing's boson-theme/1 document, image URLs absolute.
	Theme json.RawMessage `json:"theme,omitempty"`
	// Channels / AutojoinChannels: the network's fixed and auto-join
	// channels. Omitted when empty; a peer that doesn't send them means none.
	Channels         []string   `json:"channels,omitempty"`
	AutojoinChannels []string   `json:"autojoin_channels,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

// FeedChange is one entry of the feed. A deleted entry carries only the
// network's id: unverified hostnames are not public, so they don't leak
// through deletions either.
type FeedChange struct {
	Seq               int64       `json:"seq"`
	Network           FeedNetwork `json:"network"`
	VerificationToken string      `json:"verification_token,omitempty"`
	Deleted           bool        `json:"deleted"`
}

// FeedPage is GET /v1/feed. Clients continue with since=next_since while
// has_more is true; next_since is also the cursor to keep for later.
type FeedPage struct {
	Changes   []FeedChange `json:"changes"`
	NextSince int64        `json:"next_since"`
	HasMore   bool         `json:"has_more"`
}

// buildFeed turns repository changes into the exported feed.
//
// Only listings registered on this directory (origin=local) are exported:
// imported listings are never re-exported, so A→B→A loops and
// transitive trust can't happen; anyone who wants C's listings peers with
// C. A local listing that isn't verified (pending, lapsed, blocked) is
// exported as deleted, so peers drop it.
func buildFeed(changes []network.Change, since int64, limit int, mediaURL func(string) string) *FeedPage {
	page := &FeedPage{Changes: []FeedChange{}, NextSince: since, HasMore: len(changes) >= limit}
	themeBytes := 0
	for i, c := range changes {
		if themeBytes >= MaxFeedThemeBytes && i > 0 {
			page.HasMore = true
			break
		}
		page.NextSince = c.Seq
		switch {
		case c.Deleted != nil:
			t := c.Deleted
			if t.Origin != network.OriginLocal {
				continue
			}
			page.Changes = append(page.Changes, FeedChange{
				Seq: c.Seq, Deleted: true,
				Network: FeedNetwork{ID: t.NetworkID},
			})
		case c.Network != nil:
			n := c.Network
			if !n.IsLocal() {
				continue
			}
			if n.Status != network.StatusVerified {
				page.Changes = append(page.Changes, FeedChange{
					Seq: c.Seq, Deleted: true,
					Network: FeedNetwork{ID: n.ID},
				})
				continue
			}
			created, updated := n.CreatedAt.UTC(), n.UpdatedAt.UTC()
			th := theme.RenderJSON(n.Theme, mediaURL)
			themeBytes += len(th)
			page.Changes = append(page.Changes, FeedChange{
				Seq: c.Seq, VerificationToken: n.VerificationToken,
				Network: FeedNetwork{
					ID: n.ID, Hostname: n.Hostname, Port: n.Port, TLS: n.TLS, Name: n.Name,
					Description: n.Description, Tags: n.Tags, Languages: n.Languages, IsNSFW: n.IsNSFW,
					WebsiteURL: n.WebsiteURL, AuthURL: n.AuthURL,
					IconURL: mediaURLPtr(n.IconKey, mediaURL), BannerURL: mediaURLPtr(n.BannerKey, mediaURL),
					Theme:    th,
					Channels: n.Channels, AutojoinChannels: n.AutojoinChannels,
					CreatedAt: &created, UpdatedAt: &updated,
				},
			})
		}
	}
	return page
}

func mediaURLPtr(key *string, f func(string) string) *string {
	if key == nil || *key == "" {
		return nil
	}
	u := f(*key)
	return &u
}

var (
	originIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// Tokens are base64url today; legacy imports may carry other printable
	// forms. TXT matching is a substring test, so keep them simple.
	tokenRe = regexp.MustCompile(`^[A-Za-z0-9_.:+/=-]{8,256}$`)
)

// errRejected marks an imported change that failed validation.
var errRejected = errors.New("rejected")

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errRejected, fmt.Sprintf(format, a...))
}

// importedNetwork validates a feed entry with the same rules as a local
// registration and returns the row to upsert for origin. strict requires
// https image URLs (production).
func importedNetwork(origin string, c FeedChange, strict bool) (*network.Network, error) {
	fn := c.Network
	if !originIDRe.MatchString(fn.ID) {
		return nil, reject("bad id %q", trunc(fn.ID))
	}
	host, err := hostname.Normalize(fn.Hostname)
	if err != nil || host != fn.Hostname {
		return nil, reject("%s: bad hostname", fn.ID)
	}
	if fn.Port < 1 || fn.Port > 65535 {
		return nil, reject("%s: bad port", fn.ID)
	}
	if !tokenRe.MatchString(c.VerificationToken) {
		return nil, reject("%s: bad verification token", fn.ID)
	}
	id := fn.ID
	n := &network.Network{
		Hostname: host, Port: fn.Port, TLS: fn.TLS, IsNSFW: fn.IsNSFW,
		Origin: origin, OriginID: &id, VerificationToken: c.VerificationToken,
		Status: network.StatusPending, HealthStatus: network.HealthUnknown,
	}
	if n.Name, err = networks.CleanName(fn.Name); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.Description, err = networks.CleanDescription(fn.Description); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.Tags, err = networks.CleanTags(fn.Tags); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.Languages, err = networks.CleanLanguages(fn.Languages); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.WebsiteURL, err = networks.CleanURL(fn.WebsiteURL, "website_url"); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.AuthURL, err = networks.CleanURL(fn.AuthURL, "auth_url"); err != nil {
		return nil, reject("%s: %v", id, err)
	}
	if n.IconKey, err = imageURL(fn.IconURL, strict); err != nil {
		return nil, reject("%s: icon_url: %v", id, err)
	}
	if n.BannerKey, err = imageURL(fn.BannerURL, strict); err != nil {
		return nil, reject("%s: banner_url: %v", id, err)
	}
	if fn.CreatedAt != nil && !fn.CreatedAt.IsZero() && fn.CreatedAt.Before(time.Now().Add(time.Hour)) {
		n.CreatedAt = fn.CreatedAt.UTC()
	}
	return n, nil
}

// imageURL accepts an absolute image URL (https; http too outside
// production). Imported rows keep it as their icon/banner key.
func imageURL(v *string, strict bool) (*string, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	t := strings.TrimSpace(*v)
	u, err := url.Parse(t)
	if err != nil || u.Host == "" || len(t) > 2048 || u.User != nil {
		return nil, errors.New("must be an absolute URL")
	}
	if u.Scheme != "https" && (strict || u.Scheme != "http") {
		return nil, errors.New("must be an https:// URL")
	}
	return &t, nil
}

// importedTheme validates a peer's theme with the same rules as an owner's
// write. Image fields must be absolute URLs (https in production): an
// imported listing hot-links its images, like its icon and banner.
func importedTheme(raw json.RawMessage, strict bool) (network.JSONB, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	t, err := theme.Parse(raw, theme.Options{AllowHTTP: !strict})
	if err != nil {
		return nil, err
	}
	return t.Encode(), nil
}

// importedChannels validates a peer's channel lists like an owner's write.
// A bad list is dropped (the listing shows without it), not the listing.
func importedChannels(n *network.Network, fn FeedNetwork) []string {
	var problems []string
	var err error
	if n.Channels, err = networks.CleanChannels(fn.Channels, "channels"); err != nil {
		n.Channels, problems = []string{}, append(problems, "channels dropped: "+err.Error())
	}
	if n.AutojoinChannels, err = networks.CleanChannels(fn.AutojoinChannels, "autojoin_channels"); err != nil {
		n.AutojoinChannels, problems = []string{}, append(problems, "autojoin_channels dropped: "+err.Error())
	}
	return problems
}

func trunc(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
