// Package media stores listing icons and banners: decode, cover-crop to a
// fixed size, re-encode PNG, store under a content-addressed key. Storage
// is a local directory (served at /media/) or an S3/R2 bucket.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"

	_ "image/gif"  // GIF decoder
	_ "image/jpeg" // JPEG decoder

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // WebP decoder
)

const (
	// MaxUploadBytes caps the raw upload.
	MaxUploadBytes = 5 << 20
	// MaxImagePixels caps decoded dimensions (decompression bombs).
	MaxImagePixels = 24 * 1000 * 1000

	IconSize     = 256
	BannerWidth  = 1200
	BannerHeight = 400
)

var (
	ErrTooLarge         = errors.New("image too large")
	ErrUnsupportedImage = errors.New("unsupported or invalid image")
)

// Storage is the object store.
type Storage interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, key string) error
}

type MediaServiceImpl interface {
	// Process normalises raw to width×height and stores it under
	// <namespace>/<id>-<hash>.png. Best-effort deletes prevKey.
	Process(ctx context.Context, namespace, id string, raw []byte, prevKey string, width, height int) (string, error)
	// Store saves already-processed bytes under key (imports).
	Store(ctx context.Context, key string, data []byte, contentType string) error
	Remove(ctx context.Context, key string) error
	// URLFor is the public URL of key. Keys that are absolute URLs
	// (imported listings) are returned as they are.
	URLFor(key string) string
}

type MediaService struct {
	storage Storage
	baseURL string
}

func NewMediaService(storage Storage, publicBaseURL string) *MediaService {
	return &MediaService{storage: storage, baseURL: strings.TrimRight(publicBaseURL, "/")}
}

// IsAbsolute reports whether key is a full URL rather than a storage key.
func IsAbsolute(key string) bool {
	return strings.HasPrefix(key, "https://") || strings.HasPrefix(key, "http://")
}

func (s *MediaService) URLFor(key string) string {
	if key == "" {
		return ""
	}
	if IsAbsolute(key) {
		return key
	}
	return s.baseURL + "/" + key
}

func (s *MediaService) Store(ctx context.Context, key string, data []byte, contentType string) error {
	return s.storage.Put(ctx, key, data, contentType)
}

func (s *MediaService) Remove(ctx context.Context, key string) error {
	if key == "" || IsAbsolute(key) {
		return nil
	}
	return s.storage.Delete(ctx, key)
}

func (s *MediaService) Process(ctx context.Context, namespace, id string, raw []byte, prevKey string, width, height int) (string, error) {
	if len(raw) == 0 {
		return "", ErrUnsupportedImage
	}
	if len(raw) > MaxUploadBytes {
		return "", ErrTooLarge
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxImagePixels/cfg.Height {
		return "", ErrTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, cover(src, width, height)); err != nil {
		return "", err
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	key := fmt.Sprintf("%s/%s-%s.png", namespace, id, hex.EncodeToString(sum[:6]))
	if err := s.storage.Put(ctx, key, data, "image/png"); err != nil {
		return "", err
	}
	if prevKey != "" && prevKey != key {
		_ = s.Remove(ctx, prevKey)
	}
	return key, nil
}

// cover centre-crops src to the target aspect ratio and scales it.
func cover(src image.Image, width, height int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if sw <= 0 || sh <= 0 {
		return dst
	}
	targetAR := float64(width) / float64(height)
	cw, ch := sw, sh
	if float64(sw)/float64(sh) > targetAR {
		cw = max(int(float64(sh)*targetAR), 1)
	} else {
		ch = max(int(float64(sw)/targetAR), 1)
	}
	ox, oy := b.Min.X+(sw-cw)/2, b.Min.Y+(sh-ch)/2
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, image.Rect(ox, oy, ox+cw, oy+ch), draw.Over, nil)
	return dst
}
