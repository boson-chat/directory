// Package importer copies the backend's servers table into networks
// (`directory import-legacy`).
//
//   - registered_by (a boson user uuid) maps to owner_auth_user_id through
//     the backend's auth_identities; rows whose owner has no mapping are
//     imported without an owner (operators can still manage them).
//   - Hostnames are normalised; duplicates resolve to the verified row, then
//     the oldest. Invalid hostnames (IPs, single labels) are skipped.
//   - The backend's uuid is kept in legacy_id so /servers/{uuid} still works.
//     Re-running skips rows already imported.
//   - Images: with CDNURL the keys become absolute URLs on the old CDN; with
//     CopyMedia the images are downloaded and stored in this directory's
//     media storage under the same key; with neither the keys are kept
//     (MEDIA_* points at the same bucket).
//   - A verified row gets last_verified_at = its last check (or now), so it
//     doesn't lapse on its first re-check.
package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/hostname"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/networks"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Options struct {
	DryRun    bool
	CDNURL    string
	CopyMedia bool
	HTTP      *http.Client
	Now       func() time.Time
	// Log receives one line per decision.
	Log func(format string, args ...any)
}

// Report summarises an import.
type Report struct {
	Read         int
	Imported     int
	AlreadyThere int
	Duplicates   int
	Invalid      int
	Conflicts    int
	NoOwner      int
	MediaCopied  int
	MediaFailed  int
}

func (r Report) String() string {
	return fmt.Sprintf("read %d, imported %d, already imported %d, duplicates dropped %d, invalid hostnames %d, hostname conflicts %d, without owner %d, media copied %d (failed %d)",
		r.Read, r.Imported, r.AlreadyThere, r.Duplicates, r.Invalid, r.Conflicts, r.NoOwner, r.MediaCopied, r.MediaFailed)
}

type row struct {
	ID            uuid.UUID
	Hostname      string
	Port          int
	TLS           bool
	Name          string
	Description   sql.NullString
	Tags          pq.StringArray
	Languages     pq.StringArray
	IsNSFW        bool
	Status        string
	Token         sql.NullString
	TokenIssued   sql.NullTime
	LastChecked   sql.NullTime
	Health        string
	HealthChecked sql.NullTime
	UserCount     sql.NullInt64
	UserCountAt   sql.NullTime
	RegisteredAt  time.Time
	Icon          sql.NullString
	Banner        sql.NullString
	AuthUserID    sql.NullString
	host          string
}

const query = `
SELECT s.id, s.hostname, s.port, s.tls, s.name, s.description, s.tags, s.languages, s.is_nsfw,
       s.verification_status, s.verification_token, s.verification_token_issued_at, s.verification_last_checked_at,
       s.health_status, s.health_last_checked_at, s.user_count, s.user_count_updated_at, s.registered_at,
       s.icon_storage_key, s.banner_storage_key, ai.auth_user_id
FROM servers s
LEFT JOIN auth_identities ai ON ai.user_id = s.registered_by
ORDER BY s.registered_at ASC, s.id ASC`

func rank(status string) int {
	switch status {
	case network.StatusVerified:
		return 0
	case network.StatusLapsed:
		return 1
	}
	return 2
}

// Import copies src's servers into repo.
func Import(ctx context.Context, src *sql.DB, repo network.NetworkRepositoryImpl, m media.MediaServiceImpl, o Options) (Report, error) {
	var rep Report
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if o.CopyMedia && o.CDNURL == "" {
		return rep, errors.New("--copy-media needs --legacy-cdn-url")
	}
	o.CDNURL = strings.TrimRight(o.CDNURL, "/")

	rows, err := src.QueryContext(ctx, query)
	if err != nil {
		return rep, fmt.Errorf("reading servers: %w", err)
	}
	defer rows.Close()
	byHost := map[string][]*row{}
	var order []string
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Hostname, &r.Port, &r.TLS, &r.Name, &r.Description, &r.Tags, &r.Languages, &r.IsNSFW,
			&r.Status, &r.Token, &r.TokenIssued, &r.LastChecked, &r.Health, &r.HealthChecked, &r.UserCount, &r.UserCountAt,
			&r.RegisteredAt, &r.Icon, &r.Banner, &r.AuthUserID); err != nil {
			return rep, err
		}
		rep.Read++
		h, err := hostname.Normalize(r.Hostname)
		if err != nil {
			rep.Invalid++
			o.Log("skip %s (%q): invalid hostname", r.ID, r.Hostname)
			continue
		}
		r.host = h
		if _, ok := byHost[h]; !ok {
			order = append(order, h)
		}
		byHost[h] = append(byHost[h], &r)
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}

	for _, h := range order {
		group := byHost[h]
		sort.SliceStable(group, func(i, j int) bool {
			if a, b := rank(group[i].Status), rank(group[j].Status); a != b {
				return a < b
			}
			return group[i].RegisteredAt.Before(group[j].RegisteredAt)
		})
		win := group[0]
		for _, d := range group[1:] {
			rep.Duplicates++
			o.Log("drop %s (%s, %s): duplicate of %s", d.ID, h, d.Status, win.ID)
		}
		if _, err := repo.GetByLegacyID(ctx, win.ID); err == nil {
			rep.AlreadyThere++
			continue
		} else if !errors.Is(err, network.ErrNotFound) {
			return rep, err
		}
		if cur, err := repo.GetByHostname(ctx, h); err == nil {
			rep.Conflicts++
			o.Log("skip %s (%s): hostname already listed here as %s", win.ID, h, cur.ID)
			continue
		} else if !errors.Is(err, network.ErrNotFound) {
			return rep, err
		}
		n := toNetwork(win, o.Now().UTC())
		if n.OwnerAuthUserID == nil {
			rep.NoOwner++
		}
		var mediaErr error
		n.IconKey, mediaErr = importMedia(ctx, win.Icon, m, o, &rep)
		if mediaErr != nil {
			o.Log("icon of %s: %v", h, mediaErr)
		}
		n.BannerKey, mediaErr = importMedia(ctx, win.Banner, m, o, &rep)
		if mediaErr != nil {
			o.Log("banner of %s: %v", h, mediaErr)
		}
		o.Log("import %s → %s (%s, owner %s)", win.ID, h, n.Status, ownerLabel(n))
		rep.Imported++
		if o.DryRun {
			continue
		}
		if err := repo.Create(ctx, n); err != nil {
			return rep, fmt.Errorf("importing %s: %w", h, err)
		}
	}
	return rep, nil
}

func ownerLabel(n *network.Network) string {
	if n.OwnerAuthUserID == nil {
		return "none"
	}
	return *n.OwnerAuthUserID
}

func toNetwork(r *row, now time.Time) *network.Network {
	id := r.ID
	n := &network.Network{
		Hostname: r.host, Port: r.Port, TLS: r.TLS, Name: strings.TrimSpace(r.Name), Description: strings.TrimSpace(r.Description.String),
		Tags: append(pq.StringArray{}, r.Tags...), Languages: append(pq.StringArray{}, r.Languages...), IsNSFW: r.IsNSFW,
		Origin: network.OriginLocal, LegacyID: &id, CreatedAt: r.RegisteredAt, HealthStatus: network.HealthUnknown,
	}
	if n.Name == "" {
		n.Name = r.host
	}
	if r.AuthUserID.Valid && r.AuthUserID.String != "" {
		a := r.AuthUserID.String
		n.OwnerAuthUserID = &a
	}
	switch r.Status {
	case network.StatusVerified, network.StatusLapsed:
		n.Status = r.Status
		v := now
		if r.LastChecked.Valid {
			v = r.LastChecked.Time
		}
		if r.Status == network.StatusVerified {
			// Treat the import as a success so it gets the full lapse window.
			v = now
		}
		n.LastVerifiedAt = &v
	default:
		n.Status = network.StatusPending
	}
	if r.LastChecked.Valid {
		t := r.LastChecked.Time
		n.LastCheckedAt = &t
	}
	n.VerificationToken = r.Token.String
	if n.VerificationToken == "" {
		n.VerificationToken = networks.NewToken()
	}
	n.TokenIssuedAt = r.RegisteredAt
	if r.TokenIssued.Valid {
		n.TokenIssuedAt = r.TokenIssued.Time
	}
	switch r.Health {
	case network.HealthUp, network.HealthDown:
		n.HealthStatus = r.Health
	}
	if r.HealthChecked.Valid {
		t := r.HealthChecked.Time
		n.HealthCheckedAt = &t
	}
	if r.UserCount.Valid {
		c := int(r.UserCount.Int64)
		n.UserCount = &c
		if r.UserCountAt.Valid {
			t := r.UserCountAt.Time
			n.UserCountAt = &t
		}
	}
	return n
}

func importMedia(ctx context.Context, key sql.NullString, m media.MediaServiceImpl, o Options, rep *Report) (*string, error) {
	if !key.Valid || key.String == "" {
		return nil, nil
	}
	k := key.String
	if media.IsAbsolute(k) {
		return &k, nil
	}
	if o.CDNURL == "" {
		return &k, nil
	}
	abs := o.CDNURL + "/" + strings.TrimLeft(k, "/")
	if !o.CopyMedia {
		return &abs, nil
	}
	if o.DryRun {
		return &k, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, abs, nil)
	if err != nil {
		rep.MediaFailed++
		return &abs, err
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		rep.MediaFailed++
		return &abs, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rep.MediaFailed++
		return &abs, fmt.Errorf("GET %s: %s", abs, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, media.MaxUploadBytes+1))
	if err != nil || len(data) > media.MaxUploadBytes {
		rep.MediaFailed++
		return &abs, fmt.Errorf("reading %s: too large or failed", abs)
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/png"
	}
	if err := m.Store(ctx, k, data, ct); err != nil {
		rep.MediaFailed++
		return &abs, err
	}
	rep.MediaCopied++
	return &k, nil
}
