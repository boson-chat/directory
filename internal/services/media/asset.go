package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Theme images (logo, sign-in and pane backgrounds) keep their format and
// size: they are validated, not cropped.
const (
	// MaxAssetBytes caps one theme image.
	MaxAssetBytes = 2 << 20
	// MaxAssetSide caps each side in pixels.
	MaxAssetSide = 4096
)

// ErrUnsafeSVG: the SVG has scripts, foreignObject, event handlers or
// external references.
var ErrUnsafeSVG = errors.New("unsafe SVG")

// AssetTypes maps a stored extension to its content type.
var AssetTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"svg":  "image/svg+xml",
}

// Asset is a validated theme image ready to store.
type Asset struct {
	Data        []byte
	Ext         string
	ContentType string
}

// PrepareAsset checks raw is a PNG, JPEG, GIF, WebP or a safe SVG within
// MaxAssetBytes and MaxAssetSide. PNG and JPEG are re-encoded (drops
// metadata such as EXIF location); GIF (animation) and WebP are kept as
// sent after a full decode.
func PrepareAsset(raw []byte) (*Asset, error) {
	if len(raw) == 0 {
		return nil, ErrUnsupportedImage
	}
	if len(raw) > MaxAssetBytes {
		return nil, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		if looksLikeSVG(raw) {
			if err := CheckSVG(raw); err != nil {
				return nil, err
			}
			return &Asset{Data: raw, Ext: "svg", ContentType: AssetTypes["svg"]}, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, ErrUnsupportedImage
	}
	if cfg.Width > MaxAssetSide || cfg.Height > MaxAssetSide {
		return nil, ErrTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
			return nil, err
		}
		return keepSmaller(raw, buf.Bytes(), "png"), nil
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
		return &Asset{Data: buf.Bytes(), Ext: "jpg", ContentType: AssetTypes["jpg"]}, nil
	case "gif", "webp":
		return &Asset{Data: raw, Ext: format, ContentType: AssetTypes[format]}, nil
	}
	return nil, ErrUnsupportedImage
}

// keepSmaller re-encodes PNG but never grows past the size cap.
func keepSmaller(raw, enc []byte, ext string) *Asset {
	if len(enc) > MaxAssetBytes {
		enc = raw
	}
	return &Asset{Data: enc, Ext: ext, ContentType: AssetTypes[ext]}
}

func looksLikeSVG(raw []byte) bool {
	head := raw
	if len(head) > 4096 {
		head = head[:4096]
	}
	return bytes.Contains(bytes.ToLower(head), []byte("<svg"))
}

// forbidden SVG elements (lowercase local names).
var svgForbidden = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "embed": true, "object": true,
	"handler": true, "listener": true, "audio": true, "video": true,
}

// CheckSVG rejects SVG that isn't plain vector art: scripts,
// foreignObject, on* handlers, javascript: URLs, references to anything
// but fragments or embedded raster data, DTDs/entities, and sizes over
// MaxAssetSide. SVG is only ever shown as an image (no scripts run there),
// so this is defence in depth for anyone opening the file directly.
func CheckSVG(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("%w: not UTF-8", ErrUnsafeSVG)
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	d.Strict = true
	d.Entity = map[string]string{}
	depth := 0
	sawRoot := false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsupportedImage, err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return fmt.Errorf("%w: DOCTYPE or entity declarations", ErrUnsafeSVG)
		case xml.ProcInst:
			if !strings.EqualFold(t.Target, "xml") {
				return fmt.Errorf("%w: processing instruction %s", ErrUnsafeSVG, t.Target)
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if depth == 0 {
				if sawRoot || name != "svg" {
					return fmt.Errorf("%w: the root element must be <svg>", ErrUnsupportedImage)
				}
				sawRoot = true
				if err := checkSVGSize(t.Attr); err != nil {
					return err
				}
			}
			if svgForbidden[name] {
				return fmt.Errorf("%w: <%s>", ErrUnsafeSVG, t.Name.Local)
			}
			for _, a := range t.Attr {
				if err := checkSVGAttr(a); err != nil {
					return err
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if bytes.Contains(bytes.ToLower(t), []byte("@import")) {
				return fmt.Errorf("%w: @import", ErrUnsafeSVG)
			}
		}
	}
	if !sawRoot {
		return fmt.Errorf("%w: no <svg> element", ErrUnsupportedImage)
	}
	return nil
}

func compact(v string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == 0 {
			return -1
		}
		return r
	}, v))
}

func checkSVGAttr(a xml.Attr) error {
	local := strings.ToLower(a.Name.Local)
	if strings.HasPrefix(local, "on") {
		return fmt.Errorf("%w: event handler %s", ErrUnsafeSVG, a.Name.Local)
	}
	v := compact(a.Value)
	if strings.Contains(v, "javascript:") || strings.Contains(v, "vbscript:") {
		return fmt.Errorf("%w: script URL in %s", ErrUnsafeSVG, a.Name.Local)
	}
	if local == "href" || local == "src" {
		if !strings.HasPrefix(v, "#") && !isRasterData(v) {
			return fmt.Errorf("%w: external reference in %s", ErrUnsafeSVG, a.Name.Local)
		}
	}
	if strings.Contains(v, "url(") && !strings.Contains(v, "url(#") && !strings.Contains(v, "url('#") && !strings.Contains(v, `url("#`) {
		return fmt.Errorf("%w: external url() in %s", ErrUnsafeSVG, a.Name.Local)
	}
	return nil
}

func isRasterData(v string) bool {
	for _, t := range []string{"png", "jpeg", "jpg", "gif", "webp"} {
		if strings.HasPrefix(v, "data:image/"+t+";") || strings.HasPrefix(v, "data:image/"+t+",") {
			return true
		}
	}
	return false
}

func checkSVGSize(attrs []xml.Attr) error {
	for _, a := range attrs {
		if n := strings.ToLower(a.Name.Local); n != "width" && n != "height" {
			continue
		}
		v := strings.TrimSuffix(strings.TrimSpace(a.Value), "px")
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > MaxAssetSide {
			return ErrTooLarge
		}
	}
	return nil
}

// StoreAsset stores a prepared asset under <namespace>/<name>-<hash>.<ext>.
func (s *MediaService) StoreAsset(ctx context.Context, namespace, name string, a *Asset) (string, error) {
	sum := sha256.Sum256(a.Data)
	key := fmt.Sprintf("%s/%s-%s.%s", namespace, name, hex.EncodeToString(sum[:6]), a.Ext)
	if err := s.storage.Put(ctx, key, a.Data, a.ContentType); err != nil {
		return "", err
	}
	return key, nil
}

// IsKeyIn reports whether v is a storage key under namespace (not a URL).
func IsKeyIn(v, namespace string) bool {
	return !IsAbsolute(v) && strings.HasPrefix(v, namespace+"/") && !strings.Contains(v, "..")
}
