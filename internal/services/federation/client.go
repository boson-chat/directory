package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Errors for peer URLs.
var (
	errBadURL     = errors.New("a peer is a directory base URL like https://directory.example.org")
	errNeedHTTPS  = errors.New("peer URLs must use https://")
	errPrivateDst = errors.New("peer address is not a public IP")
)

// NormalizeURL canonicalises a peer base URL: scheme and host lowercased,
// default port and trailing slash dropped, no credentials, query or
// fragment. With requireHTTPS only https is accepted, and loopback /
// private IP literals and "localhost" are refused up front (the dialer
// refuses them again after DNS resolution).
func NormalizeURL(raw string, requireHTTPS bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 512 {
		return "", errBadURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errBadURL
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "https":
	case "http":
		if requireHTTPS {
			return "", errNeedHTTPS
		}
	default:
		return "", errBadURL
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", errBadURL
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errBadURL
		}
		if (u.Scheme == "https" && n == 443) || (u.Scheme == "http" && n == 80) {
			port = ""
		}
	}
	if requireHTTPS {
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return "", errPrivateDst
		}
		if a, err := netip.ParseAddr(host); err == nil && !PublicAddr(a) {
			return "", errPrivateDst
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	return u.Scheme + "://" + host + path, nil
}

// nonPublic are ranges a peer may never resolve to in production.
var nonPublic = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// PublicAddr reports whether a is a globally routable unicast address.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// ClientOptions bound every peer call.
type ClientOptions struct {
	Timeout  time.Duration
	MaxBytes int64
	// Production: https only, public IPs only (checked after DNS resolution,
	// so DNS rebinding can't reach internal services).
	Strict bool
	// Transport replaces the guarded transport (tests only).
	Transport nethttp.RoundTripper
}

// Client fetches a peer's .well-known document and feed.
type Client struct {
	http     *nethttp.Client
	maxBytes int64
	strict   bool
}

// NewClient returns a peer client with the SSRF guard installed.
func NewClient(o ClientOptions) *Client {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = 8 << 20
	}
	rt := o.Transport
	if rt == nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		if o.Strict {
			dialer.Control = func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				a, err := netip.ParseAddr(host)
				if err != nil || !PublicAddr(a) {
					return fmt.Errorf("%w: %s", errPrivateDst, host)
				}
				return nil
			}
		}
		rt = &nethttp.Transport{
			// Never an env proxy: it would bypass the dial guard.
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: o.Timeout,
		}
	}
	strict := o.Strict
	return &Client{
		maxBytes: o.MaxBytes,
		strict:   strict,
		http: &nethttp.Client{
			Timeout:   o.Timeout,
			Transport: rt,
			CheckRedirect: func(req *nethttp.Request, via []*nethttp.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				if strict && req.URL.Scheme != "https" {
					return errNeedHTTPS
				}
				return nil
			},
		},
	}
}

// WellKnown is /.well-known/boson-directory.json.
type WellKnown struct {
	Name       string `json:"name"`
	API        string `json:"api"`
	Version    string `json:"version"`
	Operator   string `json:"operator"`
	Contact    string `json:"contact"`
	Auth       string `json:"auth,omitempty"`
	AuthIssuer string `json:"auth_issuer,omitempty"`
	Federation bool   `json:"federation"`
}

// PeerError is a failure talking to a peer (stored as last_error).
type PeerError struct{ Msg string }

func (e *PeerError) Error() string { return e.Msg }

func peerErr(format string, a ...any) error { return &PeerError{Msg: fmt.Sprintf(format, a...)} }

func (c *Client) getJSON(ctx context.Context, u string, dst any) error {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, u, nil)
	if err != nil {
		return peerErr("bad peer URL")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "boson-directory-federation/1")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, errPrivateDst) || errors.Is(err, errNeedHTTPS) {
			return peerErr("%s: %v", u, unwrapPeer(err))
		}
		return peerErr("%s: request failed: %v", u, unwrapPeer(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return peerErr("%s: HTTP %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return peerErr("%s: reading response: %v", u, err)
	}
	if int64(len(body)) > c.maxBytes {
		return peerErr("%s: response larger than %d bytes", u, c.maxBytes)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return peerErr("%s: invalid JSON", u)
	}
	return nil
}

// unwrapPeer strips url.Error's method/URL prefix (we add the URL ourselves).
func unwrapPeer(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// FetchWellKnown reads and validates base's .well-known document.
func (c *Client) FetchWellKnown(ctx context.Context, base string) (*WellKnown, error) {
	var wk WellKnown
	if err := c.getJSON(ctx, base+"/.well-known/boson-directory.json", &wk); err != nil {
		return nil, err
	}
	if !wk.Federation {
		return nil, peerErr("%s does not offer federation (.well-known federation is not true)", base)
	}
	if wk.API == "" {
		return nil, peerErr("%s: .well-known has no api URL", base)
	}
	if len(wk.Name) > 200 {
		wk.Name = string([]rune(wk.Name)[:100])
	}
	return &wk, nil
}

// FetchFeed reads one page of base's change feed.
func (c *Client) FetchFeed(ctx context.Context, base string, since int64, limit int) (*FeedPage, error) {
	q := url.Values{"since": {strconv.FormatInt(since, 10)}, "limit": {strconv.Itoa(limit)}}
	var page FeedPage
	if err := c.getJSON(ctx, base+"/v1/feed?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	if page.NextSince < since {
		// The peer's sequence went backwards (restored or rebuilt
		// database). The caller restarts from zero.
		return &page, errCursorRegressed
	}
	if len(page.Changes) > limit {
		return nil, peerErr("%s: feed returned %d changes for limit %d", base, len(page.Changes), limit)
	}
	return &page, nil
}

var errCursorRegressed = errors.New("peer feed cursor went backwards")
