package http

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMediaHandlerTypes(t *testing.T) {
	h := mediaHandler(fstest.MapFS{
		"network-themes/a.svg":  {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		"network-themes/b.webp": {Data: []byte("RIFF")},
		"network-icons/c.png":   {Data: []byte("png")},
		"x/d.html":              {Data: []byte("<script>")},
	})
	for path, ct := range map[string]string{
		"/media/network-themes/a.svg": "image/svg+xml", "/media/network-themes/b.webp": "image/webp", "/media/network-icons/c.png": "image/png",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != ct || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %v", path, w.Code, w.Header())
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || (strings.HasSuffix(path, ".svg") != strings.Contains(csp, "style-src 'unsafe-inline'")) {
			t.Fatalf("%s csp %q", path, csp)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/media/x/d.html", nil))
	if w.Code != 404 {
		t.Fatalf("html served: %d", w.Code)
	}
}
