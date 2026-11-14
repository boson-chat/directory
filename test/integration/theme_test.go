package integration

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const safeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="#7c3aed"/></svg>`

func themeOf(t *testing.T, r resp) map[string]any {
	t.Helper()
	th, _ := r.m(t)["network"].(map[string]any)["theme"].(map[string]any)
	return th
}

func (e *env) themeObjects() int {
	n := 0
	for k := range e.media.Objects {
		if strings.HasPrefix(k, "network-themes/") {
			n++
		}
	}
	return n
}

func TestThemeNetworkKey(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-theme-0000000000000")
	id := e.registerVerified(k, "irc.theme.example", nil)
	base := "/v1/networks/" + id

	// No theme yet: theme is null on every public shape.
	if r := e.expect(e.do("GET", base, "", nil), 200).m(t); r["network"].(map[string]any)["theme"] != nil {
		t.Fatalf("theme before any: %v", r["network"])
	}

	doc := map[string]any{
		"version": 1, "base": "dark", "colors": map[string]any{"accent": "#7C3AED", "bg": "#0b0b10", "nope": "#000000"},
		"nick": map[string]any{"saturation": 60, "lightness": 64}, "radius": 4, "font": "system", "raw_css": "x",
		"panes": map[string]any{"channels": map[string]any{"bg": "#111118", "dim": 0.6, "blur": 0}},
	}
	th := themeOf(t, e.expect(e.do("PUT", base+"/theme", k, doc), 200))
	if th["colors"].(map[string]any)["accent"] != "#7c3aed" || th["raw_css"] != nil || th["colors"].(map[string]any)["nope"] != nil || th["font"] != "system" {
		t.Fatalf("stored theme %v", th)
	}
	for name, bad := range map[string]any{
		"colour":  map[string]any{"colors": map[string]any{"accent": "purple"}},
		"dim":     map[string]any{"panes": map[string]any{"messages": map[string]any{"dim": 2}}},
		"blur":    map[string]any{"panes": map[string]any{"messages": map[string]any{"blur": 30}}},
		"base":    map[string]any{"base": "sepia"},
		"version": map[string]any{"version": 3},
		"http":    map[string]any{"logo": "ftp://cdn.example/x.png"},
	} {
		if r := e.expect(e.do("PUT", base+"/theme", k, bad), 400).m(t); r["error"] != "invalid_theme" {
			t.Errorf("%s: %v", name, r)
		}
	}
	if r := e.do("PUT", base+"/theme", k, map[string]any{"logo": "https://cdn.example/" + strings.Repeat("x", 17000)}); r.Status != 400 {
		t.Fatalf("over 16 KB: %d", r.Status)
	}

	// Image slots.
	r := e.expect(e.do("PUT", base+"/theme-images/logo", k, []byte(safeSVG)), 200)
	th = themeOf(t, r)
	logo, _ := th["logo"].(string)
	if !strings.HasPrefix(logo, "https://media.test/network-themes/"+id+"-logo-") || !strings.HasSuffix(logo, ".svg") {
		t.Fatalf("logo %q", logo)
	}
	if th["colors"] == nil || th["panes"].(map[string]any)["channels"].(map[string]any)["bg"] != "#111118" {
		t.Fatalf("upload must keep the rest of the theme: %v", th)
	}
	th = themeOf(t, e.expect(e.do("PUT", base+"/theme-images/pane-channels", k, pngBytes(t, 800, 600)), 200))
	ch := th["panes"].(map[string]any)["channels"].(map[string]any)
	if img, _ := ch["image"].(string); !strings.HasPrefix(img, "https://media.test/network-themes/"+id+"-pane-channels-") || ch["dim"] != 0.6 {
		t.Fatalf("pane %v", ch)
	}
	for _, slot := range []string{"logo_wide", "login_background", "pane-servers", "pane-messages", "pane-members"} {
		e.expect(e.do("PUT", base+"/theme-images/"+slot, k, pngBytes(t, 40, 40)), 200)
	}
	if e.themeObjects() != 7 {
		t.Fatalf("objects %d", e.themeObjects())
	}
	e.expect(e.do("PUT", base+"/theme-images/pane-sidebar", k, pngBytes(t, 4, 4)), 404)
	if r := e.expect(e.do("PUT", base+"/theme-images/logo", k, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)), 400).m(t); r["error"] != "unsafe_svg" {
		t.Fatal(r)
	}
	if r := e.expect(e.do("PUT", base+"/theme-images/logo", k, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject/></svg>`)), 400).m(t); r["error"] != "unsafe_svg" {
		t.Fatal(r)
	}
	e.expect(e.do("PUT", base+"/theme-images/logo", k, []byte("not an image")), 400)
	e.expect(e.do("PUT", base+"/theme-images/logo", k, pngBytes(t, 4097, 2)), 413)
	e.expect(e.do("PUT", base+"/theme-images/logo", k, append(pngBytes(t, 2, 2), bytes.Repeat([]byte{0}, 2<<20)...)), 413)
	if e.themeObjects() != 7 {
		t.Fatalf("rejected uploads stored: %d", e.themeObjects())
	}

	// Replacing an upload removes the old file.
	th = themeOf(t, e.expect(e.do("PUT", base+"/theme-images/logo", k, pngBytes(t, 64, 64)), 200))
	if th["logo"] == logo || e.themeObjects() != 7 {
		t.Fatalf("replace: %v %d", th["logo"], e.themeObjects())
	}

	// PUT the document GET returned: images stay. "" removes one; another
	// https URL is hot-linked; omitted keeps.
	got := themeOf(t, e.expect(e.do("GET", base, k, nil), 200))
	got["colors"].(map[string]any)["accent"] = "#22c55e"
	th = themeOf(t, e.expect(e.do("PUT", base+"/theme", k, got), 200))
	if th["logo"] != got["logo"] || e.themeObjects() != 7 || th["colors"].(map[string]any)["accent"] != "#22c55e" {
		t.Fatalf("round trip: %v %d", th, e.themeObjects())
	}
	delete(got, "logo")
	got["logo_wide"] = ""
	got["login_background"] = "https://stack.example/v1/branding/assets/login.webp?v=abc"
	th = themeOf(t, e.expect(e.do("PUT", base+"/theme", k, got), 200))
	if th["logo"] == nil || th["logo_wide"] != nil || th["login_background"] != "https://stack.example/v1/branding/assets/login.webp?v=abc" || e.themeObjects() != 5 {
		t.Fatalf("keep/clear/hot-link: %v %d", th, e.themeObjects())
	}
	e.expect(e.do("DELETE", base+"/theme-images/pane-members", k, nil), 200)
	if e.themeObjects() != 4 {
		t.Fatalf("slot delete: %d", e.themeObjects())
	}

	// Public shapes and the feed carry it.
	var list struct {
		Networks []struct {
			ID    string         `json:"id"`
			Theme map[string]any `json:"theme"`
		} `json:"networks"`
	}
	e.expect(e.do("GET", "/v1/networks", "", nil), 200).json(t, &list)
	if len(list.Networks) != 1 || list.Networks[0].Theme["colors"] == nil {
		t.Fatalf("list %+v", list)
	}
	if th := themeOf(t, e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.theme.example", "", nil), 200)); th["logo"] == nil {
		t.Fatal("lookup")
	}
	var feed struct {
		Changes []struct {
			Network struct {
				ID    string          `json:"id"`
				Theme json.RawMessage `json:"theme"`
			} `json:"network"`
		} `json:"changes"`
	}
	e.expect(e.do("GET", "/v1/feed", "", nil), 200).json(t, &feed)
	last := feed.Changes[len(feed.Changes)-1].Network
	if last.ID != id || !strings.Contains(string(last.Theme), `"logo":"https://media.test/network-themes/`+id+`-logo-`) ||
		!strings.Contains(string(last.Theme), "stack.example") {
		t.Fatalf("feed theme %s", last.Theme)
	}

	// Others can't touch it.
	other := netkey("someone-else-key-0000000000000000")
	e.expect(e.do("PUT", base+"/theme", other, doc), 403)
	e.expect(e.do("PUT", base+"/theme-images/logo", other, pngBytes(t, 4, 4)), 403)
	e.expect(e.do("PUT", base+"/theme", "", doc), 401)
	e.expect(e.do("DELETE", base+"/theme-images/logo", "", nil), 401)

	// DELETE the theme: null, uploads removed. Deleting the network removes the rest.
	e.expect(e.do("PUT", base+"/theme-images/logo", k, pngBytes(t, 8, 8)), 200)
	if th := e.expect(e.do("DELETE", base+"/theme", k, nil), 200).m(t)["network"].(map[string]any)["theme"]; th != nil || e.themeObjects() != 0 {
		t.Fatalf("clear: %v %d", th, e.themeObjects())
	}
	e.expect(e.do("PUT", base+"/theme-images/login_background", k, pngBytes(t, 8, 8)), 200)
	e.expect(e.do("DELETE", base, k, nil), 204)
	if e.themeObjects() != 0 {
		t.Fatalf("after network delete %d", e.themeObjects())
	}
}

func TestThemeAccountAndOperator(t *testing.T) {
	e := newEnv(t)
	owner := bearer(e.token("usr_owner"))
	id := e.registerVerified(owner, "irc.acct-theme.example", nil)
	base := "/v1/networks/" + id
	th := themeOf(t, e.expect(e.do("PUT", base+"/theme", owner, map[string]any{"base": "light", "colors": map[string]any{"accent": "#b45309"}}), 200))
	if th["base"] != "light" || th["version"] != float64(1) {
		t.Fatal(th)
	}
	e.expect(e.do("PUT", base+"/theme-images/logo", owner, pngBytes(t, 16, 16)), 200)
	e.expect(e.do("PUT", base+"/theme", bearer(e.token("usr_stranger")), map[string]any{}), 403)
	// Operators may manage any local listing.
	op := bearer(e.token("usr_op", "directory.manage"))
	e.expect(e.do("DELETE", base+"/theme-images/logo", op, nil), 200)

	// Imported listings can't be themed here (their theme comes from their directory).
	e.exec(`INSERT INTO peers (id, url, name, enabled) VALUES ('per_01THEME0000000000000000000', 'https://peer.example', 'peer', true)`)
	e.exec(`INSERT INTO networks (id, hostname, port, name, verification_token, status, origin, origin_id, theme)
		VALUES ('net_01THEMEIMPORTED0000000000', 'irc.imported-theme.example', 6697, 'Imp', 'tok-imported-xx', 'verified',
		        'peer:per_01THEME0000000000000000000', 'net_X', '{"version":1,"base":"dark","logo":"https://peer.example/media/l.png"}')`)
	if th := themeOf(t, e.expect(e.do("GET", "/v1/networks/net_01THEMEIMPORTED0000000000", "", nil), 200)); th["logo"] != "https://peer.example/media/l.png" {
		t.Fatalf("imported theme %v", th)
	}
	e.expect(e.do("PUT", "/v1/networks/net_01THEMEIMPORTED0000000000/theme", op, map[string]any{}), 409)
}
