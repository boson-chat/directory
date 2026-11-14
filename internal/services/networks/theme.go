package networks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/internal/principal"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/theme"
)

// ThemeNamespace is the media prefix of uploaded theme images.
const ThemeNamespace = "network-themes"

var (
	ErrUnknownSlot = apperr.New(http.StatusNotFound, "unknown_slot",
		"unknown image slot; use logo, logo_wide, login_background or pane-servers|channels|messages|members")
	errThemeImageTooLarge = apperr.New(http.StatusRequestEntityTooLarge, "image_too_large",
		"the image is too large (2 MB and 4096 px a side at most)")
	errThemeImageType = apperr.New(http.StatusBadRequest, "unsupported_image",
		"unsupported or invalid image (PNG, JPEG, GIF, WebP or SVG)")
	errUnsafeSVG = apperr.New(http.StatusBadRequest, "unsafe_svg",
		"the SVG has scripts, foreignObject, event handlers or external references")
)

func invalidTheme(err error) error {
	if theme.IsInvalid(err) {
		return apperr.New(http.StatusBadRequest, "invalid_theme", err.Error())
	}
	return err
}

func isThemeKey(v string) bool { return media.IsKeyIn(v, ThemeNamespace) }

// currentTheme decodes n's stored theme (nil when none).
func currentTheme(n *network.Network) *theme.Theme { return theme.Decode(n.Theme) }

// imageProbe tells "absent or null" (keep) from "" (clear) for each image
// field of a PUT body, which theme.Parse can't.
type imageProbe struct {
	Logo            *string `json:"logo"`
	LogoWide        *string `json:"logo_wide"`
	LoginBackground *string `json:"login_background"`
	Panes           map[string]struct {
		Image *string `json:"image"`
	} `json:"panes"`
}

func (p *imageProbe) get(slot theme.Slot) *string {
	switch slot {
	case theme.SlotLogo:
		return p.Logo
	case theme.SlotLogoWide:
		return p.LogoWide
	case theme.SlotLoginBackground:
		return p.LoginBackground
	}
	if name := string(slot)[len("pane-"):]; p.Panes != nil {
		return p.Panes[name].Image
	}
	return nil
}

// fits checks the rendered document (keys as URLs) against the size cap.
func (s *NetworkService) fits(t *theme.Theme) error {
	if len(t.Render(s.media.URLFor).Encode()) > theme.MaxBytes {
		return apperr.New(http.StatusBadRequest, "invalid_theme", "the theme is too large (16 KB at most)")
	}
	return nil
}

// dropThemeKeys removes uploaded theme images of old that next no longer uses.
func (s *NetworkService) dropThemeKeys(ctx context.Context, old, next *theme.Theme) {
	if old == nil {
		return
	}
	keep := map[string]bool{}
	if next != nil {
		for _, v := range next.Values() {
			keep[v] = true
		}
	}
	for _, v := range old.Values() {
		if isThemeKey(v) && !keep[v] {
			if err := s.media.Remove(ctx, v); err != nil {
				logger.FromCtx(ctx).Warn().Err(err).Str("key", v).Msg("removing theme image")
			}
		}
	}
}

func (s *NetworkService) saveTheme(ctx context.Context, n *network.Network, t *theme.Theme) error {
	if t == nil {
		n.Theme = nil
	} else {
		n.Theme = t.Encode()
	}
	return mapRepo(s.repo.Update(ctx, n, "theme"))
}

func (s *NetworkService) SetTheme(ctx context.Context, p *principal.Principal, id string, raw []byte) (*network.Network, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	// Image values must be URLs here: this directory's own (as GET returns
	// them, kept) or another host's (hot-linked, e.g. a stack's branding).
	next, err := theme.Parse(raw, theme.Options{AllowHTTP: s.o.AllowHTTPImages})
	if err != nil {
		return nil, invalidTheme(err)
	}
	var probe imageProbe
	_ = json.Unmarshal(raw, &probe)
	cur := currentTheme(n)
	for _, slot := range theme.Slots {
		curVal := ""
		if cur != nil {
			curVal = cur.Get(slot)
		}
		sent := probe.get(slot)
		switch {
		case sent == nil:
			// Omitted or null: images are managed through theme-images.
			next.Set(slot, curVal)
		case curVal != "" && (*sent == curVal || *sent == s.media.URLFor(curVal)):
			next.Set(slot, curVal)
		}
	}
	if err := s.fits(next); err != nil {
		return nil, err
	}
	if err := s.saveTheme(ctx, n, next); err != nil {
		return nil, err
	}
	s.dropThemeKeys(ctx, cur, next)
	s.audit.Record(ctx, p, "network.set_theme", target(n), nil)
	return n, nil
}

func (s *NetworkService) ClearTheme(ctx context.Context, p *principal.Principal, id string) (*network.Network, error) {
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	cur := currentTheme(n)
	if cur == nil {
		return n, nil
	}
	if err := s.saveTheme(ctx, n, nil); err != nil {
		return nil, err
	}
	s.dropThemeKeys(ctx, cur, nil)
	s.audit.Record(ctx, p, "network.clear_theme", target(n), nil)
	return n, nil
}

func (s *NetworkService) SetThemeImage(ctx context.Context, p *principal.Principal, id string, slot theme.Slot, raw []byte) (*network.Network, error) {
	if _, ok := theme.ParseSlot(string(slot)); !ok {
		return nil, ErrUnknownSlot
	}
	n, err := s.manageable(ctx, p, id, true)
	if err != nil {
		return nil, err
	}
	cur := currentTheme(n)
	next := theme.New()
	if cur != nil {
		next = cur.Clone()
	}
	key := ""
	if raw != nil {
		a, err := media.PrepareAsset(raw)
		switch {
		case errors.Is(err, media.ErrTooLarge):
			return nil, errThemeImageTooLarge
		case errors.Is(err, media.ErrUnsafeSVG):
			return nil, errUnsafeSVG
		case errors.Is(err, media.ErrUnsupportedImage):
			return nil, errThemeImageType
		case err != nil:
			return nil, err
		}
		if key, err = s.media.StoreAsset(ctx, ThemeNamespace, n.ID+"-"+string(slot), a); err != nil {
			return nil, err
		}
	} else if cur == nil || cur.Get(slot) == "" {
		return n, nil
	}
	next.Set(slot, key)
	if err := s.fits(next); err != nil {
		_ = s.media.Remove(ctx, key)
		return nil, err
	}
	if err := s.saveTheme(ctx, n, next); err != nil {
		if key != "" {
			_ = s.media.Remove(ctx, key)
		}
		return nil, err
	}
	s.dropThemeKeys(ctx, cur, next)
	action := "network.set_theme_image"
	if raw == nil {
		action = "network.clear_theme_image"
	}
	s.audit.Record(ctx, p, action, target(n), map[string]any{"slot": string(slot)})
	return n, nil
}

// themeKeys are n's uploaded theme images.
func themeKeys(n *network.Network) []string {
	t := currentTheme(n)
	if t == nil {
		return nil
	}
	var out []string
	for _, v := range t.Values() {
		if isThemeKey(v) {
			out = append(out, v)
		}
	}
	return out
}
