// Package repositories holds helpers shared by the per-entity repository
// packages. Repositories are only ever used by services.
package repositories

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a lookup matches no row. Repositories never
// return a zero-value entity instead.
var ErrNotFound = errors.New("not found")

// NotFound maps gorm.ErrRecordNotFound to ErrNotFound.
func NotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// UniqueViolation returns the violated constraint name, or "".
func UniqueViolation(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return pg.ConstraintName
	}
	return ""
}

// JSONB is a jsonb column value. nil marshals as {}.
type JSONB map[string]any

func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONB) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*j = nil
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("JSONB: cannot scan %T", src)
	}
	return json.Unmarshal(b, j)
}

// StringList is a jsonb array of strings. nil marshals as [].
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
		return fmt.Errorf("StringList: cannot scan %T", src)
	}
	return json.Unmarshal(b, (*[]string)(l))
}
