package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/configfile"
	"github.com/boson-chat/directory/internal/principal"
)

func strictURL(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if !strings.HasPrefix(s, "https://") {
		return "", errors.New("must be an https URL")
	}
	return strings.ToLower(s), nil
}

func loadFile(t *testing.T, src string) (FileValues, error) {
	t.Helper()
	doc, err := configfile.Parse([]byte(src))
	if err != nil {
		return FileValues{}, err
	}
	return LoadFile(doc, strictURL)
}

func TestLoadFileFull(t *testing.T) {
	f, err := loadFile(t, `version: 1
auth: { registration: closed }   # another service: ignored
directory:
  name: " File Dir "
  operator: Ops
  contact: ops@example.com
  include_main: false
  main_peer_url: https://Main.example.com/
  sync_every: 1h30m
  peers: [https://a.example.com/, https://B.example.com, https://a.example.com]
`)
	if err != nil {
		t.Fatal(err)
	}
	if *f.Name != "File Dir" || *f.Operator != "Ops" || *f.Contact != "ops@example.com" || *f.IncludeMain ||
		*f.MainPeerURL != "https://main.example.com" || *f.SyncEvery != 90*time.Minute {
		t.Fatalf("%+v", f)
	}
	if !f.PeersSet || strings.Join(f.Peers, ",") != "https://a.example.com,https://b.example.com" {
		t.Fatal("peers are normalised and de-duplicated", f.Peers)
	}
	if len(f.Unknown) != 0 {
		t.Fatal(f.Unknown)
	}
}

func TestLoadFileAbsentAndEmpty(t *testing.T) {
	for _, src := range []string{"", "version: 1\n", "directory: ~\n", "directory: {}\n"} {
		f, err := loadFile(t, src)
		if err != nil || len(f.Flatten()) != 0 {
			t.Fatalf("%q: %v %v", src, f.Flatten(), err)
		}
	}
	f, err := loadFile(t, "directory:\n  peers: []\n  main_peer_url: \"\"\n  operator: \"\"\n")
	if err != nil || !f.PeersSet || len(f.Peers) != 0 || *f.MainPeerURL != "" || *f.Operator != "" {
		t.Fatalf("explicit empty values are values: %+v %v", f, err)
	}
}

func TestLoadFileValidation(t *testing.T) {
	cases := []struct {
		src       string
		line, col int
		msg       string
	}{
		{"directory:\n  name: \"  \"\n", 2, 9, "can't be empty (remove the key"},
		{"directory:\n  contact: " + strings.Repeat("x", 201) + "\n", 2, 12, "at most 200"},
		{"directory:\n  include_main: maybe\n", 2, 17, "true or false"},
		{"directory:\n  main_peer_url: ftp://x\n", 2, 18, "https"},
		{"directory:\n  sync_every: 5s\n", 2, 15, "between 1m and 168h"},
		{"directory:\n  sync_every: soon\n", 2, 15, "duration"},
		{"directory:\n  peers:\n    - https://ok.example.com\n    - http://plain.example.com\n", 4, 7, "plain.example.com"},
		{"directory:\n  peers: { a: b }\n", 2, 10, "list of strings"},
		{"directory: [a]\n", 1, 12, "must be a mapping"},
	}
	for _, c := range cases {
		_, err := loadFile(t, c.src)
		e, ok := configfile.AsError(err)
		if !ok {
			t.Fatalf("%q: want positioned error, got %v", c.src, err)
		}
		if e.Line != c.line || e.Column != c.col || !strings.Contains(e.Msg, c.msg) {
			t.Errorf("%q: got %d:%d %q, want %d:%d …%s…", c.src, e.Line, e.Column, e.Msg, c.line, c.col, c.msg)
		}
	}
}

func TestLoadFileUnknownKeys(t *testing.T) {
	f, err := loadFile(t, "directory:\n  nmae: x\n  peers: []\n")
	if err != nil || strings.Join(f.Unknown, ",") != "directory.nmae" {
		t.Fatal(f.Unknown, err)
	}
}

func TestFileModePrecedence(t *testing.T) {
	s, m := newSvc()
	ctx := context.Background()
	pr := &principal.Principal{Kind: principal.KindInternal, Username: "alice"}
	// A database override from before the file.
	if _, _, err := s.Update(ctx, pr, patch(t, `{"name":"db name","contact":"db@example.com"}`)); err != nil {
		t.Fatal(err)
	}
	f, err := loadFile(t, "directory:\n  name: File Dir\n  include_main: false\n  peers: [https://a.example.com]\n")
	if err != nil {
		t.Fatal(err)
	}
	s.SetFile(&f)
	cur := s.Current()
	// File beats database and env; absent keys use the env default (the
	// database override of contact is ignored in file mode).
	if cur.Name != "File Dir" || cur.IncludeMain || cur.Contact != "" || cur.Operator != "env op" || cur.SyncEvery != 10*time.Minute {
		t.Fatalf("%+v", cur)
	}
	snap, err := s.Get(ctx)
	if err != nil || snap.Source != SourceFile || strings.Join(snap.Overridden, ",") != "name,include_main,peers" || snap.Values != cur {
		t.Fatalf("%+v %v", snap, err)
	}
	_, _, err = s.Update(ctx, pr, patch(t, `{"name":"x"}`))
	if e, ok := apperr.As(err); !ok || e.Status != 409 || e.Code != "managed_by_config_file" {
		t.Fatalf("want 409, got %v", err)
	}
	if *m.Row.Name != "db name" {
		t.Fatal("the row must stay untouched")
	}
	// File gone: database mode again, at once.
	s.SetFile(nil)
	if c := s.Current(); c.Name != "db name" || c.Contact != "db@example.com" {
		t.Fatalf("%+v", c)
	}
	if snap, _ := s.Get(ctx); snap.Source != SourceDB {
		t.Fatal(snap.Source)
	}
}

func TestExportFileRoundTrip(t *testing.T) {
	v := Values{Name: "Dir", Operator: "", Contact: "c@example.com", IncludeMain: true, MainPeerURL: "https://directory.boson.chat", SyncEvery: 15 * time.Minute}
	out, err := ExportFile(v, []string{"https://a.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := loadFile(t, "version: 1\n"+string(out))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := f.apply(Values{}); got != v {
		t.Fatalf("round trip: %+v\n%s", got, out)
	}
	if strings.Join(f.Peers, ",") != "https://a.example.com" {
		t.Fatal(f.Peers)
	}
	if out, _ := ExportFile(v, nil); !strings.Contains(string(out), "peers: []") {
		t.Fatalf("%s", out)
	}
}
