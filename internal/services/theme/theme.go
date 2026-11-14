// Package theme is the boson-theme/1 document a listing carries: colours
// for the client's design-system tokens, per-pane backgrounds, a logo and
// a sign-in background. The same rules apply in the stack, the directory
// and the client:
//
//   - colours are #rrggbb (stored lowercase); colour keys that aren't
//     client tokens (THEME_TOKENS in the client's theme.types.ts) are dropped;
//   - nick saturation/lightness 0–100, radius 0–12, font boson|system;
//   - pane dim 0–1, blur 0–24 px, position center|top|bottom|left|right;
//   - image URLs are https (http too when AllowHTTP, for dev);
//   - unknown keys are dropped;
//   - the whole document is at most MaxBytes.
//
// On a listing the image fields hold either a media storage key (an image
// uploaded to this directory) or an absolute URL (imported or hot-linked);
// Render turns keys into URLs.
package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Version is the only document version.
const Version = 1

// MaxBytes caps the encoded document.
const MaxBytes = 16 << 10

// Tokens are the themeable colour keys, in the client's editor order.
var Tokens = []string{
	"bg", "surface", "surface-raised", "surface-hover", "border", "border-soft",
	"fg", "fg-2", "fg-3", "muted", "meta",
	"accent", "accent-on",
	"success", "warn", "danger", "info",
}

// PaneNames are the client panes, left to right.
var PaneNames = []string{"servers", "channels", "messages", "members"}

// Positions are the allowed pane image positions.
var Positions = []string{"center", "top", "bottom", "left", "right"}

// Slot names an image field of the document (the theme-images API path).
type Slot string

const (
	SlotLogo            Slot = "logo"
	SlotLogoWide        Slot = "logo_wide"
	SlotLoginBackground Slot = "login_background"
	SlotPaneServers     Slot = "pane-servers"
	SlotPaneChannels    Slot = "pane-channels"
	SlotPaneMessages    Slot = "pane-messages"
	SlotPaneMembers     Slot = "pane-members"
)

// Slots lists every image slot.
var Slots = []Slot{SlotLogo, SlotLogoWide, SlotLoginBackground, SlotPaneServers, SlotPaneChannels, SlotPaneMessages, SlotPaneMembers}

// ParseSlot validates a slot name.
func ParseSlot(s string) (Slot, bool) {
	for _, v := range Slots {
		if string(v) == s {
			return v, true
		}
	}
	return "", false
}

// Nick tunes the per-nick colours (percent).
type Nick struct {
	Saturation *float64 `json:"saturation,omitempty"`
	Lightness  *float64 `json:"lightness,omitempty"`
}

// Pane is one pane's background.
type Pane struct {
	Bg       string   `json:"bg,omitempty"`
	Image    *string  `json:"image,omitempty"`
	Dim      *float64 `json:"dim,omitempty"`
	Blur     *float64 `json:"blur,omitempty"`
	Position string   `json:"position,omitempty"`
}

func (p *Pane) empty() bool {
	return p == nil || (p.Bg == "" && p.Image == nil && p.Dim == nil && p.Blur == nil && p.Position == "")
}

// Panes are the four panes.
type Panes struct {
	Servers  *Pane `json:"servers,omitempty"`
	Channels *Pane `json:"channels,omitempty"`
	Messages *Pane `json:"messages,omitempty"`
	Members  *Pane `json:"members,omitempty"`
}

func (p *Panes) pane(name string) **Pane {
	switch name {
	case "servers":
		return &p.Servers
	case "channels":
		return &p.Channels
	case "messages":
		return &p.Messages
	case "members":
		return &p.Members
	}
	return nil
}

// Theme is a boson-theme/1 document. Image fields are nil when unset.
type Theme struct {
	Version         int               `json:"version"`
	Base            string            `json:"base"`
	Colors          map[string]string `json:"colors,omitempty"`
	Nick            *Nick             `json:"nick,omitempty"`
	Radius          *float64          `json:"radius,omitempty"`
	Font            string            `json:"font,omitempty"`
	Panes           *Panes            `json:"panes,omitempty"`
	Logo            *string           `json:"logo,omitempty"`
	LogoWide        *string           `json:"logo_wide,omitempty"`
	LoginBackground *string           `json:"login_background,omitempty"`
}

// New is an empty dark theme.
func New() *Theme { return &Theme{Version: Version, Base: "dark"} }

// Options tune validation.
type Options struct {
	// AllowHTTP accepts http:// image URLs (dev; production needs https,
	// except on loopback).
	AllowHTTP bool
	// IsKey reports whether v is a media storage key of this directory
	// (accepted in place of a URL in stored documents). nil: no keys.
	IsKey func(v string) bool
}

// Error is a validation failure (a 400 for the caller).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

// IsInvalid reports whether err is a validation failure.
func IsInvalid(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func isToken(k string) bool {
	for _, t := range Tokens {
		if t == k {
			return true
		}
	}
	return false
}

func oneOf(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Parse decodes and validates a document. Unknown keys are dropped; a
// missing version means 1 and a missing base means dark.
func Parse(raw []byte, o Options) (*Theme, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > MaxBytes {
		return nil, invalid("the theme is too large (16 KB at most)")
	}
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return nil, invalid("theme: %s has the wrong type", te.Field)
		}
		return nil, invalid("the theme must be a JSON object")
	}
	if err := t.Normalize(o); err != nil {
		return nil, err
	}
	return &t, nil
}

// Normalize validates t in place: drops unknown colour keys and empty
// panes, lowercases colours, fills version and base.
func (t *Theme) Normalize(o Options) error {
	switch t.Version {
	case 0:
		t.Version = Version
	case Version:
	default:
		return invalid("theme version must be 1")
	}
	switch t.Base {
	case "":
		t.Base = "dark"
	case "dark", "light":
	default:
		return invalid("theme base must be dark or light")
	}
	if len(t.Colors) > 0 {
		out := make(map[string]string, len(t.Colors))
		for k, v := range t.Colors {
			if !isToken(k) {
				continue
			}
			if !colorRe.MatchString(v) {
				return invalid("theme colour %s must be #rrggbb", k)
			}
			out[k] = strings.ToLower(v)
		}
		t.Colors = out
	}
	if len(t.Colors) == 0 {
		t.Colors = nil
	}
	if t.Nick != nil {
		if err := rangeCheck(t.Nick.Saturation, 0, 100, "nick saturation"); err != nil {
			return err
		}
		if err := rangeCheck(t.Nick.Lightness, 0, 100, "nick lightness"); err != nil {
			return err
		}
		if t.Nick.Saturation == nil && t.Nick.Lightness == nil {
			t.Nick = nil
		}
	}
	if err := rangeCheck(t.Radius, 0, 12, "radius"); err != nil {
		return err
	}
	if t.Font != "" && t.Font != "boson" && t.Font != "system" {
		return invalid("theme font must be boson or system")
	}
	if t.Panes != nil {
		for _, name := range PaneNames {
			pp := t.Panes.pane(name)
			p := *pp
			if p == nil {
				continue
			}
			if p.Bg != "" {
				if !colorRe.MatchString(p.Bg) {
					return invalid("panes.%s.bg must be #rrggbb", name)
				}
				p.Bg = strings.ToLower(p.Bg)
			}
			if err := rangeCheck(p.Dim, 0, 1, "panes."+name+".dim"); err != nil {
				return err
			}
			if err := rangeCheck(p.Blur, 0, 24, "panes."+name+".blur"); err != nil {
				return err
			}
			if p.Position != "" && !oneOf(p.Position, Positions) {
				return invalid("panes.%s.position must be center, top, bottom, left or right", name)
			}
			if err := checkImage(&p.Image, "panes."+name+".image", o); err != nil {
				return err
			}
			if p.empty() {
				*pp = nil
			}
		}
		if t.Panes.Servers == nil && t.Panes.Channels == nil && t.Panes.Messages == nil && t.Panes.Members == nil {
			t.Panes = nil
		}
	}
	for _, f := range []struct {
		v    **string
		name string
	}{{&t.Logo, "logo"}, {&t.LogoWide, "logo_wide"}, {&t.LoginBackground, "login_background"}} {
		if err := checkImage(f.v, f.name, o); err != nil {
			return err
		}
	}
	if len(t.Encode()) > MaxBytes {
		return invalid("the theme is too large (16 KB at most)")
	}
	return nil
}

func rangeCheck(v *float64, lo, hi float64, name string) error {
	if v != nil && !(*v >= lo && *v <= hi) {
		return invalid("theme %s must be between %g and %g", name, lo, hi)
	}
	return nil
}

// checkImage validates an image field; "" clears it.
func checkImage(v **string, name string, o Options) error {
	if *v == nil {
		return nil
	}
	s := strings.TrimSpace(**v)
	if s == "" {
		*v = nil
		return nil
	}
	if o.IsKey != nil && o.IsKey(s) {
		*v = &s
		return nil
	}
	if !ValidImageURL(s, o.AllowHTTP) {
		return invalid("theme %s must be an https:// URL", name)
	}
	*v = &s
	return nil
}

// ValidImageURL: https, or http on loopback, or any http with allowHTTP.
func ValidImageURL(s string, allowHTTP bool) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || len(s) > 2048 {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		if allowHTTP {
			return true
		}
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// Encode is the canonical JSON (sorted colour keys, no unset fields).
func (t *Theme) Encode() []byte {
	b, _ := json.Marshal(t)
	return b
}

// Image returns a pointer to slot's field, creating the pane if needed.
func (t *Theme) Image(slot Slot) **string {
	switch slot {
	case SlotLogo:
		return &t.Logo
	case SlotLogoWide:
		return &t.LogoWide
	case SlotLoginBackground:
		return &t.LoginBackground
	}
	name, ok := strings.CutPrefix(string(slot), "pane-")
	if !ok {
		return nil
	}
	if t.Panes == nil {
		t.Panes = &Panes{}
	}
	pp := t.Panes.pane(name)
	if pp == nil {
		return nil
	}
	if *pp == nil {
		*pp = &Pane{}
	}
	return &(*pp).Image
}

// Get returns slot's value ("" when unset) without creating anything.
func (t *Theme) Get(slot Slot) string {
	var v *string
	switch slot {
	case SlotLogo:
		v = t.Logo
	case SlotLogoWide:
		v = t.LogoWide
	case SlotLoginBackground:
		v = t.LoginBackground
	default:
		name, _ := strings.CutPrefix(string(slot), "pane-")
		if t.Panes != nil {
			if pp := t.Panes.pane(name); pp != nil && *pp != nil {
				v = (*pp).Image
			}
		}
	}
	if v == nil {
		return ""
	}
	return *v
}

// Set sets (v != "") or clears slot, dropping a pane left empty.
func (t *Theme) Set(slot Slot, v string) {
	f := t.Image(slot)
	if f == nil {
		return
	}
	if v == "" {
		*f = nil
	} else {
		*f = &v
	}
	if t.Panes != nil {
		for _, name := range PaneNames {
			if pp := t.Panes.pane(name); (*pp).empty() {
				*pp = nil
			}
		}
		if t.Panes.Servers == nil && t.Panes.Channels == nil && t.Panes.Messages == nil && t.Panes.Members == nil {
			t.Panes = nil
		}
	}
}

// Values lists the set image values by slot.
func (t *Theme) Values() map[Slot]string {
	out := map[Slot]string{}
	for _, s := range Slots {
		if v := t.Get(s); v != "" {
			out[s] = v
		}
	}
	return out
}

// Clone deep-copies t.
func (t *Theme) Clone() *Theme {
	var c Theme
	_ = json.Unmarshal(t.Encode(), &c)
	return &c
}

// Decode reads a stored document without validating (it was validated on
// write). nil for empty input or garbage.
func Decode(raw []byte) *Theme {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil
	}
	return &t
}

// Render maps every image value through urlFor (keys become URLs).
func (t *Theme) Render(urlFor func(string) string) *Theme {
	c := t.Clone()
	for slot, v := range c.Values() {
		c.Set(slot, urlFor(v))
	}
	return c
}

// RenderJSON is Decode + Render + Encode; nil when raw holds no theme.
func RenderJSON(raw []byte, urlFor func(string) string) json.RawMessage {
	t := Decode(raw)
	if t == nil {
		return nil
	}
	return t.Render(urlFor).Encode()
}
