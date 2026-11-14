package settings

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/configfile"
	"gopkg.in/yaml.v3"
)

// The directory's part of the stack config file (BOSON_CONFIG, see
// internal/configfile), the `directory:` section:
//
//	directory:
//	  name: Boson directory          # DIRECTORY_NAME
//	  operator: Boson                # DIRECTORY_OPERATOR
//	  contact: hi@example.com        # DIRECTORY_CONTACT
//	  include_main: true             # DIRECTORY_INCLUDE_MAIN
//	  main_peer_url: https://directory.boson.chat   # DIRECTORY_MAIN_PEER ("" = none)
//	  sync_every: 10m                # DIRECTORY_SYNC_EVERY
//	  peers: [https://dir.example.net]               # DIRECTORY_PEERS
//
// While the file exists it is the source of truth: file > env > default,
// directory_settings is ignored and the settings API is read-only.

// Sources (Snapshot.Source).
const (
	SourceFile = "file"
	SourceDB   = "db"
)

// FieldPeers is the file-only peers key.
const FieldPeers = "peers"

// ErrManagedByConfigFile: read-only because the stack config file manages it.
var ErrManagedByConfigFile = apperr.New(http.StatusConflict, "managed_by_config_file",
	"managed by the stack config file (BOSON_CONFIG); edit the file or the admin panel instead")

// FileValues are the keys set in the file (nil / !PeersSet = not set).
type FileValues struct {
	Name        *string
	Operator    *string
	Contact     *string
	IncludeMain *bool
	// MainPeerURL is normalised ("" = no main directory).
	MainPeerURL *string
	SyncEvery   *time.Duration
	// Peers are normalised peer URLs the file wants enabled.
	Peers    []string
	PeersSet bool
	// Unknown lists keys of the section this version doesn't know (logged).
	Unknown []string
}

// Has reports whether field (a Field* name or FieldPeers) is set.
func (f *FileValues) Has(field string) bool {
	if f == nil {
		return false
	}
	switch field {
	case FieldName:
		return f.Name != nil
	case FieldOperator:
		return f.Operator != nil
	case FieldContact:
		return f.Contact != nil
	case FieldIncludeMain:
		return f.IncludeMain != nil
	case FieldMainPeerURL:
		return f.MainPeerURL != nil
	case FieldSyncEvery:
		return f.SyncEvery != nil
	case FieldPeers:
		return f.PeersSet
	}
	return false
}

// Flatten maps every key set to its value, keyed "directory.<field>"
// (audit diffs, restart checks).
func (f *FileValues) Flatten() map[string]any {
	m := map[string]any{}
	if f == nil {
		return m
	}
	put := func(field string, v any) {
		if f.Has(field) {
			m["directory."+field] = v
		}
	}
	put(FieldName, deref(f.Name))
	put(FieldOperator, deref(f.Operator))
	put(FieldContact, deref(f.Contact))
	put(FieldIncludeMain, deref(f.IncludeMain))
	put(FieldMainPeerURL, deref(f.MainPeerURL))
	put(FieldSyncEvery, FormatDuration(deref(f.SyncEvery)))
	put(FieldPeers, f.Peers)
	return m
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// apply returns defaults with the file's values on top.
func (f *FileValues) apply(v Values) Values {
	if f.Name != nil {
		v.Name = *f.Name
	}
	if f.Operator != nil {
		v.Operator = *f.Operator
	}
	if f.Contact != nil {
		v.Contact = *f.Contact
	}
	if f.IncludeMain != nil {
		v.IncludeMain = *f.IncludeMain
	}
	if f.MainPeerURL != nil {
		v.MainPeerURL = *f.MainPeerURL
	}
	if f.SyncEvery != nil {
		v.SyncEvery = *f.SyncEvery
	}
	return v
}

func (f *FileValues) fields() []string {
	out := []string{}
	for _, field := range append(append([]string{}, Fields...), FieldPeers) {
		if f.Has(field) {
			out = append(out, field)
		}
	}
	return out
}

// LoadFile decodes and validates the directory section with the same
// rules as PATCH /v1/admin/settings. normalize validates peer URLs
// (federation.NormalizeURL); nil only trims.
func LoadFile(doc *configfile.Document, normalize func(string) (string, error)) (FileValues, error) {
	if normalize == nil {
		normalize = func(s string) (string, error) { return strings.TrimRight(strings.TrimSpace(s), "/"), nil }
	}
	var f FileValues
	s, err := doc.Section("directory")
	if err != nil {
		return f, err
	}
	text := func(field string, maxLen int, required bool, dst **string) error {
		v, err := s.String(field)
		if err != nil || v == nil {
			return err
		}
		c, err := cleanText(field, *v, maxLen, required)
		if err != nil {
			msg := err.Error()
			if e, ok := apperr.As(err); ok {
				msg = strings.TrimPrefix(e.Message, field+" ")
				msg = strings.Replace(msg, "(send null to use the default)", "(remove the key to use the default)", 1)
			}
			return s.Errorf(field, "%s", msg)
		}
		*dst = &c
		return nil
	}
	if err := text(FieldName, MaxNameLen, true, &f.Name); err != nil {
		return FileValues{}, err
	}
	if err := text(FieldOperator, MaxOperatorLen, false, &f.Operator); err != nil {
		return FileValues{}, err
	}
	if err := text(FieldContact, MaxContactLen, false, &f.Contact); err != nil {
		return FileValues{}, err
	}
	if f.IncludeMain, err = s.Bool(FieldIncludeMain); err != nil {
		return FileValues{}, err
	}
	if v, err := s.String(FieldMainPeerURL); err != nil {
		return FileValues{}, err
	} else if v != nil {
		u := strings.TrimSpace(*v)
		if u != "" {
			if u, err = normalize(u); err != nil {
				return FileValues{}, s.Errorf(FieldMainPeerURL, "%v", err)
			}
		}
		f.MainPeerURL = &u
	}
	if v, err := s.String(FieldSyncEvery); err != nil {
		return FileValues{}, err
	} else if v != nil {
		d, err := ParseSyncEvery(*v)
		if err != nil {
			return FileValues{}, s.Errorf(FieldSyncEvery, "must be a duration between %s and %s, such as 10m or 1h",
				FormatDuration(MinSyncEvery), FormatDuration(MaxSyncEvery))
		}
		f.SyncEvery = &d
	}
	list, set, err := s.Strings(FieldPeers)
	if err != nil {
		return FileValues{}, err
	}
	if set {
		seen := map[string]bool{}
		f.Peers, f.PeersSet = []string{}, true
		for i, raw := range list {
			u, err := normalize(raw)
			if err != nil {
				return FileValues{}, configfile.At(s.ItemNode(FieldPeers, i), "directory.peers: %q: %v", raw, err)
			}
			if !seen[u] {
				seen[u] = true
				f.Peers = append(f.Peers, u)
			}
		}
	}
	f.Unknown = s.Unknown(append(append([]string{}, Fields...), FieldPeers)...)
	return f, nil
}

// ExportFile renders the directory: section for the stack config file from
// v (env defaults with the database overrides applied) and peers (the
// enabled, non-builtin peers' URLs).
func ExportFile(v Values, peers []string) ([]byte, error) {
	if peers == nil {
		peers = []string{}
	}
	m := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, val any) {
		n := &yaml.Node{}
		_ = n.Encode(val)
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, n)
	}
	add(FieldName, v.Name)
	add(FieldOperator, v.Operator)
	add(FieldContact, v.Contact)
	add(FieldIncludeMain, v.IncludeMain)
	add(FieldMainPeerURL, v.MainPeerURL)
	add(FieldSyncEvery, FormatDuration(v.SyncEvery))
	add(FieldPeers, peers)
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "directory",
		HeadComment: "Exported by `directory export-config` from the env and the database.\nMerge into config/boson.yaml."}, m}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}
