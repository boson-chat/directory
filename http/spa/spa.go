// Package spa serves the embedded web UI (ui/build).
package spa

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// SPA serves the built UI: real files as they are, anything else as
// index.html so client-side routes (/users/usr_…) work on reload.
type SPA struct {
	fsys  fs.FS
	index []byte
	// ScriptHashes are CSP sources for index.html's inline bootstrap scripts.
	ScriptHashes []string
	// ImgOrigins are extra img-src origins (the boson CDN for avatars).
	ImgOrigins []string
}

var inlineScript = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

// NewSPA loads index.html from fsys. A missing index.html is not fatal (a
// Go-only build without `make ui`): a stub page explains what to do.
func NewSPA(fsys fs.FS) (*SPA, error) {
	index, err := fs.ReadFile(fsys, "index.html")
	if errors.Is(err, fs.ErrNotExist) {
		index = []byte("<!doctype html><title>boson directory</title><p>The web UI is not built. Run <code>make ui</code> and rebuild. The API is at <a href=\"/v1/networks\">/v1/networks</a>.</p>")
	} else if err != nil {
		return nil, err
	}
	s := &SPA{fsys: fsys, index: index}
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		if bytes.Contains(m[1], []byte("src=")) {
			continue
		}
		sum := sha256.Sum256(m[2])
		s.ScriptHashes = append(s.ScriptHashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return s, nil
}

// CSP returns the Content-Security-Policy for the app. connect lists extra
// origins the browser may contact (the public latency probes), exactly.
func (s *SPA) CSP(connect ...string) string {
	script := "'self'"
	if len(s.ScriptHashes) > 0 {
		script += " " + strings.Join(s.ScriptHashes, " ")
	}
	conn := "'self'" + origins(connect)
	img := "'self' data:" + origins(s.ImgOrigins)
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + script,
		// Svelte transitions write inline styles.
		"style-src 'self' 'unsafe-inline'",
		"img-src " + img,
		"font-src 'self'",
		"connect-src " + conn,
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"object-src 'none'",
	}, "; ")
}

// origins renders a CSP source list, dropping duplicates and anything
// that could widen the policy.
func origins(list []string) string {
	out := ""
	seen := map[string]bool{}
	for _, o := range list {
		if o == "" || seen[o] || strings.ContainsAny(o, " ;,'*") {
			continue
		}
		seen[o] = true
		out += " " + o
	}
	return out
}

var startTime = time.Now()

func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if st, err := fs.Stat(s.fsys, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=300")
			}
			http.ServeFileFS(w, r, s.fsys, name)
			return
		}
		// A missing asset is a 404, not the app shell.
		if strings.HasPrefix(name, "_app/") || path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", startTime, bytes.NewReader(s.index))
}

// Origin returns scheme://host of u, or "" when u isn't an absolute
// http(s) URL (so it never widens the CSP).
func Origin(u string) string {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") || p.Host == "" {
		return ""
	}
	return p.Scheme + "://" + p.Host
}
