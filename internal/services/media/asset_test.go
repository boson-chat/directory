package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func encode(t *testing.T, format string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&buf, img)
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPrepareAssetRaster(t *testing.T) {
	for format, ext := range map[string]string{"png": "png", "jpeg": "jpg", "gif": "gif"} {
		a, err := PrepareAsset(encode(t, format, 64, 32))
		if err != nil || a.Ext != ext || a.ContentType != AssetTypes[ext] {
			t.Fatalf("%s: %+v %v", format, a, err)
		}
	}
	// Not cropped: a 4096-wide image stays 4096 wide.
	a, err := PrepareAsset(encode(t, "png", 4096, 10))
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(a.Data)); cfg.Width != 4096 || cfg.Height != 10 {
		t.Fatalf("%dx%d", cfg.Width, cfg.Height)
	}
	if _, err := PrepareAsset(encode(t, "png", 4097, 10)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("4097 px: %v", err)
	}
	if _, err := PrepareAsset(bytes.Repeat([]byte{1}, MaxAssetBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size: %v", err)
	}
	if _, err := PrepareAsset([]byte("hello")); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("junk: %v", err)
	}
	if _, err := PrepareAsset(nil); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatal("empty")
	}
}

const okSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="64" height="64" viewBox="0 0 64 64">
  <defs><linearGradient id="g"><stop offset="0" stop-color="#7c3aed"/></linearGradient></defs>
  <style>.a{fill:url(#g)}</style>
  <rect class="a" width="64" height="64" rx="8" style="fill:url(#g)"/>
  <use xlink:href="#g"/>
  <image href="data:image/png;base64,iVBORw0KGgo=" width="1" height="1"/>
</svg>`

func TestPrepareAssetSVG(t *testing.T) {
	a, err := PrepareAsset([]byte(okSVG))
	if err != nil || a.Ext != "svg" || a.ContentType != "image/svg+xml" || string(a.Data) != okSVG {
		t.Fatalf("%+v %v", a, err)
	}
	unsafe := map[string]string{
		"script":        `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"SCRIPT":        `<svg xmlns="http://www.w3.org/2000/svg"><SCRIPT>alert(1)</SCRIPT></svg>`,
		"foreignObject": `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject></svg>`,
		"onload":        `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"js href":       `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="java&#x09;script:alert(1)"><rect/></a></svg>`,
		"external use":  `<svg xmlns="http://www.w3.org/2000/svg"><use href="https://evil.example/x.svg#a"/></svg>`,
		"external url":  `<svg xmlns="http://www.w3.org/2000/svg"><rect style="fill:url(https://evil.example/x)"/></svg>`,
		"svg data":      `<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/svg+xml;base64,PHN2Zz4="/></svg>`,
		"animate js":    `<svg xmlns="http://www.w3.org/2000/svg"><a><animate attributeName="href" values="javascript:alert(1)"/></a></svg>`,
		"entity":        `<!DOCTYPE svg [<!ENTITY x "y">]><svg xmlns="http://www.w3.org/2000/svg">&x;</svg>`,
		"import":        `<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(https://evil.example/a.css);</style></svg>`,
		"pi":            `<?xml-stylesheet href="https://evil.example/a.css"?><svg xmlns="http://www.w3.org/2000/svg"/>`,
	}
	for name, svg := range unsafe {
		if _, err := PrepareAsset([]byte(svg)); !errors.Is(err, ErrUnsafeSVG) {
			t.Errorf("%s: %v", name, err)
		}
	}
	invalid := map[string]string{
		"html root": `<html><svg xmlns="http://www.w3.org/2000/svg"/></html>`,
		"broken":    `<svg xmlns="http://www.w3.org/2000/svg"><rect></svg>`,
		"two roots": `<svg xmlns="http://www.w3.org/2000/svg"/><svg/>`,
	}
	for name, svg := range invalid {
		if _, err := PrepareAsset([]byte(svg)); !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := PrepareAsset([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="5000px" height="10"/>`)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("wide svg: %v", err)
	}
}

func TestStoreAsset(t *testing.T) {
	store := NewMemStorage()
	s := NewMediaService(store, "https://m.example")
	a, _ := PrepareAsset([]byte(okSVG))
	key, err := s.StoreAsset(context.Background(), "network-themes", "net_1-logo", a)
	if err != nil || !strings.HasPrefix(key, "network-themes/net_1-logo-") || !strings.HasSuffix(key, ".svg") {
		t.Fatalf("%q %v", key, err)
	}
	if !IsKeyIn(key, "network-themes") || IsKeyIn("https://x/network-themes/a.png", "network-themes") || IsKeyIn("network-icons/a.png", "network-themes") {
		t.Fatal("IsKeyIn")
	}
}
