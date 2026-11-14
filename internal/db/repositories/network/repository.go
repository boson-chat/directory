package network

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/boson-chat/directory/internal/db"
	"github.com/boson-chat/directory/internal/db/repositories"
	"github.com/boson-chat/directory/internal/ids"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NetworkRepository struct {
	db *db.DB
}

func NewNetworkRepository(d *db.DB) NetworkRepositoryImpl {
	return &NetworkRepository{db: d}
}

func mapErr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if c := repositories.UniqueViolation(err); c == "networks_hostname_key" {
		return ErrHostnameTaken
	}
	return err
}

func (r *NetworkRepository) Create(ctx context.Context, n *Network) error {
	if n.ID == "" {
		n.ID = ids.New(ids.Network)
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if n.Languages == nil {
		n.Languages = []string{}
	}
	if n.Origin == "" {
		n.Origin = OriginLocal
	}
	if n.Status == "" {
		n.Status = StatusPending
	}
	if n.HealthStatus == "" {
		n.HealthStatus = HealthUnknown
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	if err := r.db.Conn(ctx).Create(n).Error; err != nil {
		return mapErr(err)
	}
	return r.reload(ctx, n)
}

// reload refreshes the database-maintained columns (seq, updated_at).
func (r *NetworkRepository) reload(ctx context.Context, n *Network) error {
	return r.db.Conn(ctx).Model(&Network{}).Select("seq", "updated_at").Where("id = ?", n.ID).
		Row().Scan(&n.Seq, &n.UpdatedAt)
}

func (r *NetworkRepository) first(ctx context.Context, where string, arg any) (*Network, error) {
	var n Network
	if err := r.db.Conn(ctx).Where(where, arg).First(&n).Error; err != nil {
		return nil, mapErr(err)
	}
	return &n, nil
}

func (r *NetworkRepository) Get(ctx context.Context, id string) (*Network, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *NetworkRepository) GetByHostname(ctx context.Context, hostname string) (*Network, error) {
	return r.first(ctx, "hostname = ?", hostname)
}

func (r *NetworkRepository) GetByLegacyID(ctx context.Context, id uuid.UUID) (*Network, error) {
	return r.first(ctx, "legacy_id = ?", id)
}

// orders: sort → (ORDER BY, keyset comparison with n placeholders).
var orders = map[Sort]struct {
	order string
	cmp   string
}{
	SortUsers:  {"coalesce(user_count, -1) DESC, created_at DESC, id DESC", "(coalesce(user_count, -1), created_at, id) < (?, ?, ?)"},
	SortActive: {"coalesce(user_count_at, 'epoch'::timestamptz) DESC, id DESC", "(coalesce(user_count_at, 'epoch'::timestamptz), id) < (?, ?)"},
	SortNewest: {"created_at DESC, id DESC", "(created_at, id) < (?, ?)"},
	SortName:   {"lower(name) ASC, id ASC", "(lower(name), id) > (?, ?)"},
}

const timeKey = time.RFC3339Nano

// KeysOf returns the cursor keys of n for sort s.
func KeysOf(n *Network, s Sort) []string {
	switch s {
	case SortActive:
		t := time.Unix(0, 0).UTC()
		if n.UserCountAt != nil {
			t = n.UserCountAt.UTC()
		}
		return []string{t.Format(timeKey)}
	case SortNewest:
		return []string{n.CreatedAt.UTC().Format(timeKey)}
	case SortName:
		return []string{strings.ToLower(n.Name)}
	default:
		c := -1
		if n.UserCount != nil {
			c = *n.UserCount
		}
		return []string{strconv.Itoa(c), n.CreatedAt.UTC().Format(timeKey)}
	}
}

// ErrBadCursor is returned for a cursor whose keys don't parse.
var ErrBadCursor = errors.New("invalid cursor")

func cursorArgs(c *Cursor) ([]any, error) {
	parseT := func(s string) (time.Time, error) { return time.Parse(timeKey, s) }
	var args []any
	switch c.Sort {
	case SortUsers:
		if len(c.Keys) != 2 {
			return nil, ErrBadCursor
		}
		n, err := strconv.Atoi(c.Keys[0])
		if err != nil {
			return nil, ErrBadCursor
		}
		t, err := parseT(c.Keys[1])
		if err != nil {
			return nil, ErrBadCursor
		}
		args = []any{n, t}
	case SortActive, SortNewest:
		if len(c.Keys) != 1 {
			return nil, ErrBadCursor
		}
		t, err := parseT(c.Keys[0])
		if err != nil {
			return nil, ErrBadCursor
		}
		args = []any{t}
	case SortName:
		if len(c.Keys) != 1 {
			return nil, ErrBadCursor
		}
		args = []any{c.Keys[0]}
	default:
		return nil, ErrBadCursor
	}
	return append(args, c.ID), nil
}

func (r *NetworkRepository) filtered(ctx context.Context, f ListFilter) *gorm.DB {
	q := r.db.Conn(ctx).Model(&Network{})
	switch {
	case f.AnyStatus:
	case len(f.Statuses) > 0:
		q = q.Where("status IN ?", f.Statuses)
	default:
		q = q.Where("status = ?", StatusVerified)
	}
	if !f.IncludeNSFW {
		q = q.Where("is_nsfw = false")
	}
	if f.Language != "" {
		q = q.Where("? = ANY(languages)", f.Language)
	}
	if f.Tag != "" {
		q = q.Where("? = ANY(tags)", f.Tag)
	}
	switch f.Source {
	case "", "all":
	default:
		q = q.Where("origin = ?", f.Source)
	}
	if f.OwnerAuthUserID != "" {
		q = q.Where("owner_auth_user_id = ?", f.OwnerAuthUserID)
	}
	if f.KeyHash != "" {
		q = q.Where("management_key_hash = ?", f.KeyHash)
	}
	if tsq := PrefixTSQuery(f.Query); tsq != "" {
		q = q.Where("search_vector @@ to_tsquery('simple', ?)", tsq)
	}
	return q
}

func (r *NetworkRepository) List(ctx context.Context, f ListFilter) ([]*Network, error) {
	if !ValidSort(f.Sort) {
		f.Sort = SortUsers
	}
	o := orders[f.Sort]
	q := r.filtered(ctx, f)
	if f.After != nil {
		if f.After.Sort != f.Sort {
			return nil, ErrBadCursor
		}
		args, err := cursorArgs(f.After)
		if err != nil {
			return nil, err
		}
		q = q.Where(o.cmp, args...)
	}
	if f.Limit <= 0 {
		f.Limit = 25
	}
	q = q.Order(o.order).Limit(f.Limit)
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	var out []*Network
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *NetworkRepository) Count(ctx context.Context, f ListFilter) (int64, error) {
	var n int64
	err := r.filtered(ctx, f).Count(&n).Error
	return n, err
}

func (r *NetworkRepository) Facets(ctx context.Context, includeNSFW bool, limit int) ([]Facet, []Facet, error) {
	if limit <= 0 {
		limit = 50
	}
	run := func(col string) ([]Facet, error) {
		out := []Facet{}
		err := r.db.Conn(ctx).Raw(fmt.Sprintf(`
			SELECT v AS value, count(*) AS count
			FROM networks, unnest(%s) AS v
			WHERE status = 'verified' AND (? OR NOT is_nsfw)
			GROUP BY v ORDER BY count(*) DESC, v ASC LIMIT ?`, col), includeNSFW, limit).
			Scan(&out).Error
		return out, err
	}
	tags, err := run("tags")
	if err != nil {
		return nil, nil, err
	}
	langs, err := run("languages")
	if err != nil {
		return nil, nil, err
	}
	return tags, langs, nil
}

func (r *NetworkRepository) OriginCounts(ctx context.Context, includeNSFW bool) (map[string]int64, error) {
	var rows []struct {
		Origin string
		Count  int64
	}
	err := r.db.Conn(ctx).Raw(`SELECT origin, count(*) AS count FROM networks
		WHERE status = 'verified' AND (? OR NOT is_nsfw) GROUP BY origin`, includeNSFW).Scan(&rows).Error
	out := map[string]int64{}
	for _, x := range rows {
		out[x.Origin] = x.Count
	}
	return out, err
}

func (r *NetworkRepository) Update(ctx context.Context, n *Network, columns ...string) error {
	if len(columns) == 0 {
		return nil
	}
	res := r.db.Conn(ctx).Model(&Network{ID: n.ID}).Select(columns).Updates(n)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return r.reload(ctx, n)
}

func (r *NetworkRepository) Delete(ctx context.Context, id string) error {
	res := r.db.Conn(ctx).Where("id = ?", id).Delete(&Network{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *NetworkRepository) DueForVerify(ctx context.Context, d VerifyDue) ([]*Network, error) {
	if d.Limit <= 0 {
		d.Limit = 200
	}
	var out []*Network
	err := r.db.Conn(ctx).
		Where(`(status = 'verified' AND (last_checked_at IS NULL OR last_checked_at < ?))
			OR (status = 'lapsed' AND (last_checked_at IS NULL OR last_checked_at < ?))
			OR (status = 'pending' AND (last_checked_at IS NULL OR last_checked_at < ?)
				AND (origin <> 'local' OR token_issued_at > ?))`,
			d.Now.Add(-d.VerifiedEvery), d.Now.Add(-d.LapsedEvery), d.Now.Add(-d.PendingEvery), d.Now.Add(-d.TokenTTL)).
		Order("last_checked_at ASC NULLS FIRST, id ASC").Limit(d.Limit).Find(&out).Error
	return out, err
}

func (r *NetworkRepository) RecordVerify(ctx context.Context, v VerifyResult) (bool, error) {
	upd := map[string]any{"status": v.Status, "last_checked_at": v.CheckedAt}
	if v.VerifiedAt != nil {
		upd["last_verified_at"] = *v.VerifiedAt
	}
	res := r.db.Conn(ctx).Model(&Network{}).
		Where("id = ? AND verification_token = ? AND status <> 'blocked'", v.ID, v.CheckedToken).
		Updates(upd)
	return res.RowsAffected > 0, res.Error
}

func (r *NetworkRepository) DueForHealth(ctx context.Context, before time.Time, limit int) ([]*Network, error) {
	if limit <= 0 {
		limit = 500
	}
	var out []*Network
	err := r.db.Conn(ctx).Where("status = 'verified' AND (health_checked_at IS NULL OR health_checked_at < ?)", before).
		Order("health_checked_at ASC NULLS FIRST, id ASC").Limit(limit).Find(&out).Error
	return out, err
}

func (r *NetworkRepository) RecordHealth(ctx context.Context, h HealthResult) error {
	upd := map[string]any{"health_status": h.Status, "health_checked_at": h.CheckedAt}
	if h.UserCount != nil {
		upd["user_count"] = *h.UserCount
		upd["user_count_at"] = h.CheckedAt
	}
	return r.db.Conn(ctx).Model(&Network{}).Where("id = ?", h.ID).Updates(upd).Error
}

// patternWhere is the SQL match for a block pattern.
func patternWhere(pattern string) (string, []any) {
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return "(hostname = ? OR hostname LIKE ?)", []any{suffix, "%." + suffix}
	}
	return "hostname = ?", []any{pattern}
}

func (r *NetworkRepository) BlockMatching(ctx context.Context, pattern string) ([]string, error) {
	where, args := patternWhere(pattern)
	var changed []string
	err := r.db.Conn(ctx).Raw("UPDATE networks SET status = 'blocked' WHERE status <> 'blocked' AND "+where+" RETURNING id", args...).
		Scan(&changed).Error
	return changed, err
}

func (r *NetworkRepository) UnblockMatching(ctx context.Context, pattern string, stillBlocked func(string) bool) ([]string, error) {
	where, args := patternWhere(pattern)
	var rows []*Network
	if err := r.db.Conn(ctx).Where("status = 'blocked' AND "+where, args...).Find(&rows).Error; err != nil {
		return nil, err
	}
	var changed []string
	for _, n := range rows {
		if stillBlocked != nil && stillBlocked(n.Hostname) {
			continue
		}
		status := StatusPending
		if n.LastVerifiedAt != nil {
			status = StatusLapsed
		}
		if err := r.db.Conn(ctx).Model(&Network{}).Where("id = ? AND status = 'blocked'", n.ID).
			Updates(map[string]any{"status": status, "last_checked_at": nil}).Error; err != nil {
			return changed, err
		}
		changed = append(changed, n.ID)
	}
	return changed, nil
}

// upsertColumns are the public fields a peer import may set.
var upsertColumns = []string{"hostname", "port", "tls", "name", "description", "tags", "languages", "is_nsfw",
	"website_url", "auth_url", "icon_key", "banner_key"}

func (r *NetworkRepository) UpsertByOrigin(ctx context.Context, n *Network) (UpsertResult, error) {
	if n.Origin == "" || n.Origin == OriginLocal || n.OriginID == nil || *n.OriginID == "" {
		return "", errors.New("network: UpsertByOrigin needs a peer origin and origin_id")
	}
	var result UpsertResult
	err := r.db.WithTx(ctx, func(ctx context.Context) error {
		// Serialise imports of the same hostname.
		if err := r.db.Conn(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network:"+n.Hostname).Error; err != nil {
			return err
		}
		var cur Network
		err := r.db.Conn(ctx).Where("origin = ? AND origin_id = ?", n.Origin, *n.OriginID).First(&cur).Error
		switch {
		case err == nil:
			if cur.Hostname != n.Hostname {
				ok, err := r.makeRoom(ctx, n.Hostname, n.Origin)
				if err != nil || !ok {
					result = UpsertConflict
					return err
				}
			}
			cols := slices.Clone(upsertColumns)
			n.ID = cur.ID
			if n.VerificationToken != cur.VerificationToken {
				cols = append(cols, "verification_token", "token_issued_at", "status", "last_verified_at")
				n.TokenIssuedAt = time.Now().UTC()
				n.LastVerifiedAt = nil
				n.Status = StatusPending
				if cur.Status == StatusBlocked {
					n.Status = StatusBlocked
				}
			}
			if n.Tags == nil {
				n.Tags = []string{}
			}
			if n.Languages == nil {
				n.Languages = []string{}
			}
			before := cur.Seq
			if err := r.db.Conn(ctx).Model(&Network{ID: cur.ID}).Select(cols).Updates(n).Error; err != nil {
				return mapErr(err)
			}
			got, err := r.Get(ctx, cur.ID)
			if err != nil {
				return err
			}
			*n = *got
			result = UpsertUpdated
			if got.Seq == before {
				result = UpsertUnchanged
			}
			return nil
		case errors.Is(err, gorm.ErrRecordNotFound):
			ok, err := r.makeRoom(ctx, n.Hostname, n.Origin)
			if err != nil || !ok {
				result = UpsertConflict
				return err
			}
			n.ID = ""
			if n.TokenIssuedAt.IsZero() {
				n.TokenIssuedAt = time.Now().UTC()
			}
			if n.Status == "" {
				n.Status = StatusPending
			}
			if err := r.Create(ctx, n); err != nil {
				return err
			}
			result = UpsertCreated
			return nil
		default:
			return err
		}
	})
	return result, err
}

// makeRoom reports whether origin may take hostname, deleting another
// peer's unverified listing that holds it.
func (r *NetworkRepository) makeRoom(ctx context.Context, hostname, origin string) (bool, error) {
	var holder Network
	err := r.db.Conn(ctx).Where("hostname = ?", hostname).First(&holder).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if holder.Origin == OriginLocal || holder.Origin == origin || holder.Status == StatusVerified || holder.Status == StatusBlocked {
		return false, nil
	}
	return true, r.db.Conn(ctx).Where("id = ?", holder.ID).Delete(&Network{}).Error
}

func (r *NetworkRepository) DeleteByOrigin(ctx context.Context, origin, originID string) (bool, error) {
	res := r.db.Conn(ctx).Where("origin = ? AND origin_id = ?", origin, originID).Delete(&Network{})
	return res.RowsAffected > 0, res.Error
}

func (r *NetworkRepository) DeleteAllByOrigin(ctx context.Context, origin string) (int64, error) {
	if origin == OriginLocal {
		return 0, errors.New("network: refusing to delete every local listing")
	}
	res := r.db.Conn(ctx).Where("origin = ?", origin).Delete(&Network{})
	return res.RowsAffected, res.Error
}

func (r *NetworkRepository) CountByOrigin(ctx context.Context, origin string) (int64, error) {
	var n int64
	err := r.db.Conn(ctx).Model(&Network{}).Where("origin = ?", origin).Count(&n).Error
	return n, err
}

func (r *NetworkRepository) Changes(ctx context.Context, since int64, limit int) ([]Change, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var nets []*Network
	if err := r.db.Conn(ctx).Where("seq > ?", since).Order("seq ASC").Limit(limit).Find(&nets).Error; err != nil {
		return nil, err
	}
	var tombs []*Tombstone
	if err := r.db.Conn(ctx).Where("seq > ?", since).Order("seq ASC").Limit(limit).Find(&tombs).Error; err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(nets)+len(tombs))
	i, j := 0, 0
	for len(out) < limit && (i < len(nets) || j < len(tombs)) {
		if j >= len(tombs) || (i < len(nets) && nets[i].Seq < tombs[j].Seq) {
			out = append(out, Change{Seq: nets[i].Seq, Network: nets[i]})
			i++
		} else {
			out = append(out, Change{Seq: tombs[j].Seq, Deleted: tombs[j]})
			j++
		}
	}
	return out, nil
}

// PrefixTSQuery turns user input into a to_tsquery('simple', …) string:
// every word (dots split words, so "libera.chat" finds irc.libera.chat)
// becomes a prefix match, ANDed. Everything that to_tsquery would parse as
// an operator is dropped.
func PrefixTSQuery(input string) string {
	words := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return r == ' ' || r == '.' || r == '\t' || r == '\n'
	})
	terms := make([]string, 0, len(words))
	for _, w := range words {
		clean := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
				return r
			case r > 127 && (r < 0x2000 || r > 0x206f):
				return r // letters of other scripts
			}
			return -1
		}, w)
		clean = strings.Trim(clean, "-_")
		if clean == "" {
			continue
		}
		terms = append(terms, clean+":*")
		if len(terms) == 8 {
			break
		}
	}
	return strings.Join(terms, " & ")
}
