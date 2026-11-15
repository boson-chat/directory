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
//   - variants: at most one, for the base that isn't Base (one equal to
//     Base is dropped), with the same colour, nick and pane rules;
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

// Variant is the theme for the base that isn't the document's: its own
// colours (missing tokens come from Boson's built-in of that base), nick
// colours and per-pane overrides laid over the primary panes (a pane's
// image is the primary's unless the variant gives one). An empty variant
// is meaningful: "Boson's own look for that base".
type Variant struct {
	Colors map[string]string `json:"colors,omitempty"`
	Nick   *Nick             `json:"nick,omitempty"`
	Panes  *Panes            `json:"panes,omitempty"`
}

// Variants holds at most one variant: the base that isn't Theme.Base.
type Variants struct {
	Dark  *Variant `json:"dark,omitempty"`
	Light *Variant `json:"light,omitempty"`
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
	Variants        *Variants         `json:"variants,omitempty"`
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
	var err error
	if t.Colors, err = normalizeColors(t.Colors, ""); err != nil {
		return err
	}
	if t.Nick, err = normalizeNick(t.Nick, ""); err != nil {
		return err
	}
	if err := rangeCheck(t.Radius, 0, 12, "radius"); err != nil {
		return err
	}
	if t.Font != "" && t.Font != "boson" && t.Font != "system" {
		return invalid("theme font must be boson or system")
	}
	if t.Panes, err = normalizePanes(t.Panes, "", o); err != nil {
		return err
	}
	if err := t.normalizeVariants(o); err != nil {
		return err
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

// normalizeVariants keeps the variant for the base that isn't t.Base (the
// one equal to it is dropped) and validates it like the primary.
func (t *Theme) normalizeVariants(o Options) error {
	if t.Variants == nil {
		return nil
	}
	if t.Base == "dark" {
		t.Variants.Dark = nil
	} else {
		t.Variants.Light = nil
	}
	for _, v := range []struct {
		p    **Variant
		name string
	}{{&t.Variants.Dark, "dark"}, {&t.Variants.Light, "light"}} {
		vr := *v.p
		if vr == nil {
			continue
		}
		prefix := "variants." + v.name + "."
		var err error
		if vr.Colors, err = normalizeColors(vr.Colors, prefix); err != nil {
			return err
		}
		if vr.Nick, err = normalizeNick(vr.Nick, prefix); err != nil {
			return err
		}
		if vr.Panes, err = normalizePanes(vr.Panes, prefix, o); err != nil {
			return err
		}
	}
	if t.Variants.Dark == nil && t.Variants.Light == nil {
		t.Variants = nil
	}
	return nil
}

// Variant returns the variant for base, or nil (also for t.Base itself).
func (t *Theme) Variant(base string) *Variant {
	if t.Variants == nil || base == t.Base {
		return nil
	}
	switch base {
	case "dark":
		return t.Variants.Dark
	case "light":
		return t.Variants.Light
	}
	return nil
}

// normalizeColors drops non-token keys and lowercases; nil when empty.
func normalizeColors(in map[string]string, prefix string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if !isToken(k) {
			continue
		}
		if !colorRe.MatchString(v) {
			return nil, invalid("theme %scolour %s must be #rrggbb", prefix, k)
		}
		out[k] = strings.ToLower(v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func normalizeNick(n *Nick, prefix string) (*Nick, error) {
	if n == nil {
		return nil, nil
	}
	if err := rangeCheck(n.Saturation, 0, 100, prefix+"nick saturation"); err != nil {
		return nil, err
	}
	if err := rangeCheck(n.Lightness, 0, 100, prefix+"nick lightness"); err != nil {
		return nil, err
	}
	if n.Saturation == nil && n.Lightness == nil {
		return nil, nil
	}
	return n, nil
}

// normalizePanes validates each pane and drops empty ones; nil when none.
func normalizePanes(ps *Panes, prefix string, o Options) (*Panes, error) {
	if ps == nil {
		return nil, nil
	}
	for _, name := range PaneNames {
		pp := ps.pane(name)
		p := *pp
		if p == nil {
			continue
		}
		field := prefix + "panes." + name
		if p.Bg != "" {
			if !colorRe.MatchString(p.Bg) {
				return nil, invalid("%s.bg must be #rrggbb", field)
			}
			p.Bg = strings.ToLower(p.Bg)
		}
		if err := rangeCheck(p.Dim, 0, 1, field+".dim"); err != nil {
			return nil, err
		}
		if err := rangeCheck(p.Blur, 0, 24, field+".blur"); err != nil {
			return nil, err
		}
		if p.Position != "" && !oneOf(p.Position, Positions) {
			return nil, invalid("%s.position must be center, top, bottom, left or right", field)
		}
		if err := checkImage(&p.Image, field+".image", o); err != nil {
			return nil, err
		}
		if p.empty() {
			*pp = nil
		}
	}
	if ps.Servers == nil && ps.Channels == nil && ps.Messages == nil && ps.Members == nil {
		return nil, nil
	}
	return ps, nil
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
	if c.Variants != nil {
		for _, vr := range []*Variant{c.Variants.Dark, c.Variants.Light} {
			if vr == nil || vr.Panes == nil {
				continue
			}
			for _, name := range PaneNames {
				if p := *vr.Panes.pane(name); p != nil && p.Image != nil {
					u := urlFor(*p.Image)
					p.Image = &u
				}
			}
		}
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
