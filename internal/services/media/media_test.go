package media

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestProcess(t *testing.T) {
	store := NewMemStorage()
	s := NewMediaService(store, "https://m.example/")
	var src bytes.Buffer
	_ = png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 90, 30)))
	key, err := s.Process(context.Background(), "network-banners", "net_1", src.Bytes(), "", BannerWidth, BannerHeight)
	if err != nil || !strings.HasPrefix(key, "network-banners/net_1-") {
		t.Fatalf("%q %v", key, err)
	}
	img, err := png.Decode(bytes.NewReader(store.Objects[key]))
	if err != nil || img.Bounds().Dx() != BannerWidth || img.Bounds().Dy() != BannerHeight {
		t.Fatalf("stored %v %v", img.Bounds(), err)
	}
	if s.URLFor(key) != "https://m.example/"+key || s.URLFor("https://cdn/x.png") != "https://cdn/x.png" || s.URLFor("") != "" {
		t.Fatal("URLFor")
	}
	if _, err := s.Process(context.Background(), "n", "i", []byte("junk"), "", 1, 1); err == nil {
		t.Fatal("junk accepted")
	}
	// A huge declared size is refused before decoding.
	var bomb bytes.Buffer
	_ = png.Encode(&bomb, image.NewGray(image.Rect(0, 0, 6000, 5000)))
	if _, err := s.Process(context.Background(), "n", "i", bomb.Bytes(), "", 1, 1); err != ErrTooLarge {
		t.Fatalf("bomb: %v", err)
	}
}

func TestLocalStorageKeys(t *testing.T) {
	l, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x.png", "/abs.png", "a/../../b.png", `a\b.png`} {
		if err := l.Put(context.Background(), bad, []byte("x"), "image/png"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := l.Put(context.Background(), "network-icons/a.png", []byte("x"), "image/png"); err != nil {
		t.Fatal(err)
	}
}
