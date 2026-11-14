// Package settings is the directory_settings repository: the single row of
// runtime settings an operator overrides from the admin panel. A nil field
// means "use the env default". Only internal/services/settings uses it.
package settings

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/boson-chat/directory/internal/db"
)

// Row is directory_settings (id is always 1).
type Row struct {
	ID               int16 `gorm:"primaryKey"`
	Name             *string
	Operator         *string
	Contact          *string
	IncludeMain      *bool
	MainPeerURL      *string `gorm:"column:main_peer_url"`
	SyncEverySeconds *int
	SeededPeers      StringList
	UpdatedAt        time.Time
	UpdatedBy        *string
}

func (Row) TableName() string { return "directory_settings" }

// StringList is a jsonb array of strings.
type StringList []string

func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(l))
	return string(b), err
}

func (l *StringList) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*l = nil
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("settings: cannot scan %T into StringList", src)
	}
	return json.Unmarshal(b, (*[]string)(l))
}

// Columns a Save may write.
var Columns = []string{"name", "operator", "contact", "include_main", "main_peer_url", "sync_every_seconds"}

type SettingsRepositoryImpl interface {
	// Get returns the row (created by migration 000003).
	Get(ctx context.Context) (*Row, error)
	// Save writes the named columns plus updated_at/updated_by.
	Save(ctx context.Context, r *Row, columns ...string) error
	// SetSeededPeers replaces seeded_peers.
	SetSeededPeers(ctx context.Context, urls []string) error
}

type SettingsRepository struct{ db *db.DB }

func NewSettingsRepository(d *db.DB) SettingsRepositoryImpl { return &SettingsRepository{db: d} }

func (r *SettingsRepository) Get(ctx context.Context) (*Row, error) {
	var row Row
	res := r.db.Conn(ctx).Where("id = 1").Limit(1).Find(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// The migration inserts it; recreate it if someone deleted it.
		row = Row{ID: 1, UpdatedAt: time.Now().UTC()}
		if err := r.db.Conn(ctx).Exec("INSERT INTO directory_settings (id) VALUES (1) ON CONFLICT DO NOTHING").Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}

func (r *SettingsRepository) Save(ctx context.Context, row *Row, columns ...string) error {
	if len(columns) == 0 {
		return errors.New("settings: nothing to save")
	}
	row.ID = 1
	row.UpdatedAt = time.Now().UTC()
	columns = append(columns, "updated_at", "updated_by")
	return r.db.Conn(ctx).Model(&Row{ID: 1}).Select(columns).Updates(row).Error
}

func (r *SettingsRepository) SetSeededPeers(ctx context.Context, urls []string) error {
	return r.db.Conn(ctx).Model(&Row{ID: 1}).Update("seeded_peers", StringList(urls)).Error
}

// Memory is an in-memory SettingsRepositoryImpl for unit tests.
type Memory struct {
	Row Row
}

func (m *Memory) Get(context.Context) (*Row, error) {
	c := m.Row
	c.SeededPeers = append(StringList(nil), m.Row.SeededPeers...)
	return &c, nil
}

func (m *Memory) Save(_ context.Context, r *Row, columns ...string) error {
	for _, c := range columns {
		switch c {
		case "name":
			m.Row.Name = r.Name
		case "operator":
			m.Row.Operator = r.Operator
		case "contact":
			m.Row.Contact = r.Contact
		case "include_main":
			m.Row.IncludeMain = r.IncludeMain
		case "main_peer_url":
			m.Row.MainPeerURL = r.MainPeerURL
		case "sync_every_seconds":
			m.Row.SyncEverySeconds = r.SyncEverySeconds
		}
	}
	m.Row.UpdatedAt, m.Row.UpdatedBy = time.Now().UTC(), r.UpdatedBy
	return nil
}

func (m *Memory) SetSeededPeers(_ context.Context, urls []string) error {
	m.Row.SeededPeers = append(StringList(nil), urls...)
	return nil
}
