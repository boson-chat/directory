// Package hostname normalises IRC hostnames the way the directory stores
// them: trimmed, no trailing dot, IDNA (punycode) lowercased, and never an
// IP address or a single label.
package hostname

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// ErrInvalid is returned for anything that is not a public DNS name.
var ErrInvalid = errors.New("the hostname must be a public DNS name like irc.example.org (not an IP address)")

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

var profile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.Transitional(false))

// Normalize returns the canonical form of h.
func Normalize(h string) (string, error) {
	h = strings.TrimSuffix(strings.TrimSpace(h), ".")
	if h == "" || len(h) > 253 {
		return "", ErrInvalid
	}
	if _, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return "", ErrInvalid
	}
	a, err := profile.ToASCII(h)
	if err != nil {
		return "", ErrInvalid
	}
	a = strings.ToLower(a)
	labels := strings.Split(a, ".")
	if len(labels) < 2 {
		return "", ErrInvalid
	}
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return "", ErrInvalid
		}
	}
	// The TLD can't be all digits (that would be an IPv4-ish name).
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "", ErrInvalid
	}
	return a, nil
}
