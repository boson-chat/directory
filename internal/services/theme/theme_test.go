package theme

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseNormalises(t *testing.T) {
	raw := `{
		"version": 1, "base": "light", "name": "dropped",
		"colors": {"accent": "#7C3AED", "bg": "#0b0b10", "not-a-token": "#ffffff"},
		"nick": {"saturation": 62, "lightness": 64},
		"radius": 2, "font": "system", "css": "body{}",
		"panes": {
			"servers": {"bg": "#0B0B10"},
			"messages": {"image": "https://cdn.example/m.webp", "dim": 0.7, "blur": 8, "position": "center", "x": 1},
			"members": {},
			"sidebar": {"bg": "#000000"}
		},
		"logo": "https://cdn.example/logo.svg",
		"login_background": ""
	}`
	th, err := Parse([]byte(raw), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if th.Base != "light" || th.Font != "system" || *th.Radius != 2 {
		t.Fatalf("%+v", th)
	}
	if len(th.Colors) != 2 || th.Colors["accent"] != "#7c3aed" {
		t.Fatalf("colors %v", th.Colors)
	}
	if th.Panes.Servers.Bg != "#0b0b10" || th.Panes.Members != nil || *th.Panes.Messages.Image != "https://cdn.example/m.webp" {
		t.Fatalf("panes %+v", th.Panes)
	}
	if th.LoginBackground != nil || *th.Logo != "https://cdn.example/logo.svg" {
		t.Fatal("images")
	}
	enc := string(th.Encode())
	for _, gone := range []string{"name", "css", "not-a-token", "sidebar", `"x"`, "members"} {
		if strings.Contains(enc, gone) {
			t.Errorf("%s kept: %s", gone, enc)
		}
	}
	// Round trip is stable.
	again, err := Parse(th.Encode(), Options{})
	if err != nil || string(again.Encode()) != enc {
		t.Fatalf("round trip: %v\n%s\n%s", err, again.Encode(), enc)
	}
}

func TestParseDefaults(t *testing.T) {
	th, err := Parse([]byte(`{}`), Options{})
	if err != nil || th.Version != 1 || th.Base != "dark" || th.Colors != nil || th.Panes != nil {
		t.Fatalf("%+v %v", th, err)
	}
	if string(th.Encode()) != `{"version":1,"base":"dark"}` {
		t.Fatal(string(th.Encode()))
	}
}

func TestParseRejects(t *testing.T) {
	bad := map[string]string{
		"version":      `{"version": 2}`,
		"base":         `{"base": "blue"}`,
		"short colour": `{"colors": {"accent": "#fff"}}`,
		"named colour": `{"colors": {"accent": "red"}}`,
		"rgba":         `{"colors": {"bg": "#00000080"}}`,
		"colour type":  `{"colors": {"bg": 5}}`,
		"saturation":   `{"nick": {"saturation": 101}}`,
		"lightness":    `{"nick": {"lightness": -1}}`,
		"radius":       `{"radius": 13}`,
		"font":         `{"font": "comic"}`,
		"pane bg":      `{"panes": {"channels": {"bg": "blue"}}}`,
		"dim":          `{"panes": {"channels": {"dim": 1.5}}}`,
		"blur":         `{"panes": {"channels": {"blur": 25}}}`,
		"position":     `{"panes": {"channels": {"position": "middle"}}}`,
		"http image":   `{"logo": "http://cdn.example/x.png"}`,
		"js image":     `{"logo": "javascript:alert(1)"}`,
		"relative":     `{"logo": "/x.png"}`,
		"userinfo":     `{"logo": "https://u:p@cdn.example/x.png"}`,
		"pane image":   `{"panes": {"messages": {"image": "ftp://x/y.png"}}}`,
		"not object":   `[1,2]`,
		"garbage":      `{`,
		"too large":    `{"logo": "https://cdn.example/` + strings.Repeat("a", MaxBytes) + `"}`,
	}
	for name, raw := range bad {
		if _, err := Parse([]byte(raw), Options{}); err == nil || !IsInvalid(err) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}

func TestImageURLs(t *testing.T) {
	ok := []string{"https://cdn.example/x.png", "http://127.0.0.1:8080/x.png", "http://localhost/x.png", "http://[::1]/x.svg"}
	for _, u := range ok {
		if !ValidImageURL(u, false) {
			t.Errorf("%s rejected", u)
		}
	}
	if ValidImageURL("http://cdn.example/x.png", false) || !ValidImageURL("http://cdn.example/x.png", true) {
		t.Fatal("AllowHTTP")
	}
	// Keys are accepted only where the caller says so.
	isKey := func(v string) bool { return strings.HasPrefix(v, "network-themes/") }
	th, err := Parse([]byte(`{"logo": "network-themes/net_1-logo-abc.png"}`), Options{IsKey: isKey})
	if err != nil || th.Get(SlotLogo) != "network-themes/net_1-logo-abc.png" {
		t.Fatalf("%v", err)
	}
	if _, err := Parse([]byte(`{"logo": "network-themes/net_1-logo-abc.png"}`), Options{}); err == nil {
		t.Fatal("key accepted without IsKey")
	}
}

func TestSlotsAndRender(t *testing.T) {
	th := New()
	for _, s := range Slots {
		th.Set(s, "k/"+string(s)+".png")
	}
	if len(th.Values()) != len(Slots) || th.Panes.Members == nil || *th.Panes.Channels.Image != "k/pane-channels.png" {
		t.Fatalf("%s", th.Encode())
	}
	r := th.Render(func(k string) string { return "https://m.example/" + k })
	if r.Get(SlotPaneMessages) != "https://m.example/k/pane-messages.png" || th.Get(SlotPaneMessages) != "k/pane-messages.png" {
		t.Fatal("render must copy")
	}
	for _, s := range Slots {
		th.Set(s, "")
	}
	if th.Panes != nil || th.Logo != nil {
		t.Fatalf("clear: %s", th.Encode())
	}
	// A pane keeps its other fields when its image goes.
	th2, _ := Parse([]byte(`{"panes": {"channels": {"bg": "#111118", "image": "https://x.example/a.png"}}}`), Options{})
	th2.Set(SlotPaneChannels, "")
	if th2.Panes == nil || th2.Panes.Channels.Bg != "#111118" || th2.Panes.Channels.Image != nil {
		t.Fatalf("%s", th2.Encode())
	}
	if _, ok := ParseSlot("pane-sidebar"); ok {
		t.Fatal("bad slot")
	}
	if _, ok := ParseSlot("logo_wide"); !ok {
		t.Fatal("logo_wide")
	}
	var m map[string]any
	if err := json.Unmarshal(RenderJSON([]byte(`{"version":1,"base":"dark","logo":"a.png"}`), func(k string) string { return "https://m/" + k }), &m); err != nil || m["logo"] != "https://m/a.png" {
		t.Fatalf("RenderJSON %v %v", m, err)
	}
	if RenderJSON(nil, nil) != nil || RenderJSON([]byte("null"), nil) != nil {
		t.Fatal("empty theme must render null")
	}
}

// IRC Forever's light palette, as the second variant of its dark theme.
const lightVariant = `{
	"colors": {"bg": "#F5F9F6", "surface": "#ffffff", "surface-raised": "#ffffff", "surface-hover": "#e7efea",
		"border": "#cfdcd4", "border-soft": "#e1eae4", "fg": "#0f1a14", "fg-2": "#2c3e34", "fg-3": "#46594e",
		"muted": "#5a6c62", "meta": "#74857b", "accent": "#0d7a45", "accent-on": "#ffffff", "success": "#11733f",
		"warn": "#8a5a00", "danger": "#b42318", "info": "#0b62a3", "glow": "#ffffff"},
	"nick": {"saturation": 60, "lightness": 36},
	"panes": {"servers": {"bg": "#E7EFEA", "dim": 0.4, "x": 1}, "members": {}},
	"radius": 9, "logo": "https://cdn.example/dropped.png"
}`

func TestVariants(t *testing.T) {
	raw := `{"base": "dark", "colors": {"accent": "#70d199"}, "panes": {"servers": {"bg": "#090c0e"}},
		"variants": {"light": ` + lightVariant + `, "sepia": {"colors": {"bg": "#000000"}}}}`
	th, err := Parse([]byte(raw), Options{})
	if err != nil {
		t.Fatal(err)
	}
	v := th.Variant("light")
	if v == nil || th.Variants.Dark != nil || th.Variant("dark") != nil {
		t.Fatalf("variants %+v", th.Variants)
	}
	if len(v.Colors) != 17 || v.Colors["bg"] != "#f5f9f6" || v.Colors["glow"] != "" {
		t.Fatalf("variant colours %v", v.Colors)
	}
	if *v.Nick.Saturation != 60 || *v.Nick.Lightness != 36 {
		t.Fatalf("variant nick %+v", v.Nick)
	}
	if v.Panes.Servers.Bg != "#e7efea" || *v.Panes.Servers.Dim != 0.4 || v.Panes.Members != nil {
		t.Fatalf("variant panes %+v", v.Panes)
	}
	enc := string(th.Encode())
	for _, gone := range []string{"sepia", "glow", "radius", "dropped.png", `"x"`, "members"} {
		if strings.Contains(enc, gone) {
			t.Errorf("%s kept: %s", gone, enc)
		}
	}
	// Shared fields stay top-level; the primary is untouched.
	if th.Colors["accent"] != "#70d199" || th.Panes.Servers.Bg != "#090c0e" || th.Radius != nil {
		t.Fatalf("primary %s", enc)
	}
	again, err := Parse(th.Encode(), Options{})
	if err != nil || string(again.Encode()) != enc {
		t.Fatalf("round trip: %v\n%s\n%s", err, again.Encode(), enc)
	}
}

func TestVariantEqualToBaseDropped(t *testing.T) {
	th, err := Parse([]byte(`{"base": "light", "variants": {"light": {"colors": {"bg": "#ffffff"}}}}`), Options{})
	if err != nil || th.Variants != nil {
		t.Fatalf("%s %v", th.Encode(), err)
	}
	if string(th.Encode()) != `{"version":1,"base":"light"}` {
		t.Fatal(string(th.Encode()))
	}
	// The default base is dark, so a dark variant without base is dropped too.
	th, err = Parse([]byte(`{"variants": {"dark": {}, "light": {}}}`), Options{})
	if err != nil || string(th.Encode()) != `{"version":1,"base":"dark","variants":{"light":{}}}` {
		t.Fatalf("%s %v", th.Encode(), err)
	}
	// Even an invalid variant equal to base is dropped, not rejected.
	if _, err := Parse([]byte(`{"base": "dark", "variants": {"dark": {"colors": {"bg": "red"}}}}`), Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestVariantEmptyIsKept(t *testing.T) {
	// {} means "Boson's own light look", which differs from no variant.
	th, err := Parse([]byte(`{"variants": {"light": {"colors": {"nope": "#000000"}, "panes": {"messages": {}}}}}`), Options{})
	if err != nil || string(th.Encode()) != `{"version":1,"base":"dark","variants":{"light":{}}}` {
		t.Fatalf("%s %v", th.Encode(), err)
	}
	th, err = Parse([]byte(`{"variants": {}}`), Options{})
	if err != nil || th.Variants != nil {
		t.Fatalf("%s %v", th.Encode(), err)
	}
}

func TestVariantRejects(t *testing.T) {
	bad := map[string]string{
		"short colour": `{"variants": {"light": {"colors": {"accent": "#fff"}}}}`,
		"named colour": `{"variants": {"light": {"colors": {"bg": "white"}}}}`,
		"colour type":  `{"variants": {"light": {"colors": {"bg": 5}}}}`,
		"saturation":   `{"variants": {"light": {"nick": {"saturation": 101}}}}`,
		"lightness":    `{"variants": {"light": {"nick": {"lightness": -1}}}}`,
		"pane bg":      `{"variants": {"light": {"panes": {"channels": {"bg": "blue"}}}}}`,
		"dim":          `{"variants": {"light": {"panes": {"channels": {"dim": 1.5}}}}}`,
		"blur":         `{"variants": {"light": {"panes": {"channels": {"blur": 25}}}}}`,
		"position":     `{"variants": {"light": {"panes": {"channels": {"position": "middle"}}}}}`,
		"pane image":   `{"variants": {"light": {"panes": {"messages": {"image": "http://cdn.example/x.png"}}}}}`,
		"not object":   `{"variants": {"light": [1]}}`,
		"variants":     `{"variants": "light"}`,
		"dark variant": `{"base": "light", "variants": {"dark": {"colors": {"fg": "#12345"}}}}`,
		"too large": `{"variants": {"light": {"panes": {"messages": {"image": "https://cdn.example/` +
			strings.Repeat("a", 2000) + `"}}}}, "logo": "https://cdn.example/` + strings.Repeat("b", MaxBytes-2100) + `"}`,
	}
	for name, raw := range bad {
		_, err := Parse([]byte(raw), Options{})
		if err == nil || !IsInvalid(err) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
		if name != "too large" && name != "not object" && name != "variants" && err != nil && !strings.Contains(err.Error(), "variants.") && !strings.Contains(err.Error(), "wrong type") {
			t.Errorf("%s: error doesn't name the variant: %v", name, err)
		}
	}
}

func TestVariantImages(t *testing.T) {
	th, err := Parse([]byte(`{"panes": {"messages": {"image": "https://cdn.example/dark.webp"}},
		"variants": {"light": {"panes": {"messages": {"image": "https://cdn.example/light.webp", "dim": 0.3}}}}}`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Image slots are the primary's; the variant's image is its own URL.
	if th.Get(SlotPaneMessages) != "https://cdn.example/dark.webp" || len(th.Values()) != 1 {
		t.Fatalf("%v", th.Values())
	}
	r := th.Render(func(k string) string { return "https://m.example/" + k })
	if got := *r.Variant("light").Panes.Messages.Image; got != "https://m.example/https://cdn.example/light.webp" {
		t.Fatalf("render %s", got)
	}
	if *th.Variant("light").Panes.Messages.Image != "https://cdn.example/light.webp" {
		t.Fatal("render must copy")
	}
	th.Set(SlotPaneMessages, "")
	if th.Variant("light").Panes.Messages.Image == nil {
		t.Fatal("clearing the primary slot must not touch the variant")
	}
}
