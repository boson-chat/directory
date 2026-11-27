// Package views renders networks as API JSON. Shared by the core handlers
// and other modules (the federation feed uses Network for its public
// fields).
package views

import (
	"encoding/json"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/services/dnsverify"
	"github.com/boson-chat/directory/internal/services/media"
	"github.com/boson-chat/directory/internal/services/theme"
)

// Network is the public JSON of a listing.
type Network struct {
	ID          string   `json:"id"`
	Hostname    string   `json:"hostname"`
	Port        int      `json:"port"`
	TLS         bool     `json:"tls"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Languages   []string `json:"languages"`
	IsNSFW      bool     `json:"is_nsfw"`
	WebsiteURL  *string  `json:"website_url,omitempty"`
	AuthURL     *string  `json:"auth_url,omitempty"`
	IconURL     string   `json:"icon_url,omitempty"`
	BannerURL   string   `json:"banner_url,omitempty"`
	// Theme is the listing's boson-theme/1 document with absolute image
	// URLs; null when it has none.
	Theme json.RawMessage `json:"theme"`
	// Channels are the network's fixed channels; AutojoinChannels the ones
	// new users are joined to. Names as written; [] when none.
	Channels         []string `json:"channels"`
	AutojoinChannels []string `json:"autojoin_channels"`
	// PreviousHostnames are the hostnames the listing was moved away from,
	// most recent first; [] when it never moved. GET
	// /v1/networks/lookup?hostname=<old> still finds it.
	PreviousHostnames []string `json:"previous_hostnames"`
	// LegacyID is the listing's id on the legacy boson API directory (a
	// uuid), for rows imported from it: clients match servers saved under it.
	LegacyID *string `json:"legacy_id,omitempty"`
	// Status: pending | verified | lapsed | blocked. VerificationStatus
	// repeats it for clients written against the old /servers shape.
	Status             string     `json:"status"`
	VerificationStatus string     `json:"verification_status"`
	LastVerifiedAt     *time.Time `json:"last_verified_at,omitempty"`
	LastCheckedAt      *time.Time `json:"last_checked_at,omitempty"`
	HealthStatus       string     `json:"health_status"`
	HealthCheckedAt    *time.Time `json:"health_checked_at,omitempty"`
	UserCount          *int       `json:"user_count,omitempty"`
	UserCountAt        *time.Time `json:"user_count_at,omitempty"`
	// Origin: local, or peer:<peer id> for listings imported from another directory.
	Origin string `json:"origin"`
	// OriginName labels a peer origin (the peer's name or URL); null for local.
	OriginName *string   `json:"origin_name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// RegisteredAt repeats CreatedAt (old /servers name).
	RegisteredAt time.Time `json:"registered_at"`
	// Owner and the token fields are only shown to whoever may manage the listing.
	Owner             *Owner     `json:"owner,omitempty"`
	VerificationToken string     `json:"verification_token,omitempty"`
	TokenIssuedAt     *time.Time `json:"token_issued_at,omitempty"`
	TokenExpiresAt    *time.Time `json:"token_expires_at,omitempty"`
}

// Owner says who manages a listing.
type Owner struct {
	// Kind: account | network_key | none (imported).
	Kind       string `json:"kind"`
	AuthUserID string `json:"auth_user_id,omitempty"`
}

// Verification is the DNS record that proves control of the hostname.
type Verification struct {
	RecordName  string    `json:"record_name"`  // _boson.<hostname>
	RecordValue string    `json:"record_value"` // boson-verify=<token>
	Token       string    `json:"token"`
	IssuedAt    time.Time `json:"issued_at"`
	// ExpiresAt: a pending listing must verify before this (then regenerate).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Listing is a network plus, for its managers, the verification record
// and the pending move.
type Listing struct {
	Network      Network       `json:"network"`
	Verification *Verification `json:"verification"`
	// Move is the pending move to a new hostname (managers of local
	// listings only; omitted when there is none).
	Move *Move `json:"move,omitempty"`
	// MovedFrom is set by a lookup by a hostname the listing was moved
	// away from: the hostname asked for.
	MovedFrom string `json:"moved_from,omitempty"`
}

// Address is a hostname, port and tls.
type Address struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	TLS      bool   `json:"tls"`
}

// Move is a request to move a listing to a new address. While pending, the
// listing keeps its current address; the TXT record below, at the new
// hostname, completes the move.
type Move struct {
	ID string `json:"id"`
	// Status: pending | completed | cancelled | expired.
	Status string  `json:"status"`
	From   Address `json:"from"`
	To     Address `json:"to"`
	// Hostname, Port and TLS repeat To.
	Hostname    string     `json:"hostname"`
	Port        int        `json:"port"`
	TLS         bool       `json:"tls"`
	RequestedAt time.Time  `json:"requested_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	// The record to publish (pending moves only).
	RecordName    string     `json:"record_name,omitempty"`
	RecordValue   string     `json:"record_value,omitempty"`
	Token         string     `json:"token,omitempty"`
	IssuedAt      *time.Time `json:"issued_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	// Expired: the token is past its TTL; it is no longer checked. Request
	// the move again for a new token, or cancel it.
	Expired bool `json:"expired"`
}

// NewMove renders m. Only pending moves carry their record.
func NewMove(m *network.Move, tokenTTL time.Duration, now time.Time) *Move {
	if m == nil {
		return nil
	}
	v := &Move{
		ID: m.ID, Status: m.Status,
		From:     Address{Hostname: m.FromHostname, Port: m.FromPort, TLS: m.FromTLS},
		To:       Address{Hostname: m.ToHostname, Port: m.ToPort, TLS: m.ToTLS},
		Hostname: m.ToHostname, Port: m.ToPort, TLS: m.ToTLS,
		RequestedAt: m.RequestedAt, EndedAt: m.EndedAt, LastCheckedAt: m.LastCheckedAt,
	}
	if m.Status == network.MovePending {
		issued, expires := m.TokenIssuedAt, m.TokenIssuedAt.Add(tokenTTL)
		v.RecordName, v.RecordValue = dnsverify.RecordName(m.ToHostname), dnsverify.RecordValue(m.Token)
		v.Token, v.IssuedAt, v.ExpiresAt = m.Token, &issued, &expires
		v.Expired = now.Sub(m.TokenIssuedAt) > tokenTTL
	}
	return v
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// NewNetwork renders n's public fields.
func NewNetwork(n *network.Network, m media.MediaServiceImpl) Network {
	out := Network{
		ID: n.ID, Hostname: n.Hostname, Port: n.Port, TLS: n.TLS, Name: n.Name, Description: n.Description,
		Tags: nonNil(n.Tags), Languages: nonNil(n.Languages), IsNSFW: n.IsNSFW,
		Channels: nonNil(n.Channels), AutojoinChannels: nonNil(n.AutojoinChannels),
		PreviousHostnames: nonNil(n.PreviousHostnames),
		WebsiteURL:        n.WebsiteURL, AuthURL: n.AuthURL,
		Status: n.Status, VerificationStatus: n.Status, LastVerifiedAt: n.LastVerifiedAt, LastCheckedAt: n.LastCheckedAt,
		HealthStatus: n.HealthStatus, HealthCheckedAt: n.HealthCheckedAt, UserCount: n.UserCount, UserCountAt: n.UserCountAt,
		Origin: n.Origin, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt, RegisteredAt: n.CreatedAt,
	}
	if n.LegacyID != nil {
		id := n.LegacyID.String()
		out.LegacyID = &id
	}
	if m != nil {
		out.IconURL, out.BannerURL = m.URLFor(deref(n.IconKey)), m.URLFor(deref(n.BannerKey))
		out.Theme = theme.RenderJSON(n.Theme, m.URLFor)
	}
	return out
}

// OwnerOf describes n's owner.
func OwnerOf(n *network.Network) *Owner {
	switch {
	case n.OwnerAuthUserID != nil:
		return &Owner{Kind: "account", AuthUserID: *n.OwnerAuthUserID}
	case n.ManagementKeyHash != nil:
		return &Owner{Kind: "network_key"}
	}
	return &Owner{Kind: "none"}
}

// NewVerification renders the TXT record for n. tokenTTL sets expires_at
// on local pending listings.
func NewVerification(n *network.Network, tokenTTL time.Duration) *Verification {
	v := &Verification{
		RecordName: dnsverify.RecordName(n.Hostname), RecordValue: dnsverify.RecordValue(n.VerificationToken),
		Token: n.VerificationToken, IssuedAt: n.TokenIssuedAt,
	}
	if n.Status == network.StatusPending && n.IsLocal() {
		e := n.TokenIssuedAt.Add(tokenTTL)
		v.ExpiresAt = &e
	}
	return v
}

// NewListing renders n; manager adds the owner and verification record.
func NewListing(n *network.Network, m media.MediaServiceImpl, manager bool, tokenTTL time.Duration) Listing {
	l := Listing{Network: NewNetwork(n, m)}
	if manager {
		l.Network.Owner = OwnerOf(n)
		l.Verification = NewVerification(n, tokenTTL)
		issued, expires := n.TokenIssuedAt, n.TokenIssuedAt.Add(tokenTTL)
		l.Network.VerificationToken, l.Network.TokenIssuedAt, l.Network.TokenExpiresAt = n.VerificationToken, &issued, &expires
	}
	return l
}

// PeerNames resolves a peer origin ("peer:<id>") to a label.
type PeerNames func(origin string) *string

// WithOriginName sets OriginName using names (nil-safe).
func (v *Network) WithOriginName(names PeerNames) {
	if v.Origin != network.OriginLocal && names != nil {
		v.OriginName = names(v.Origin)
	}
}
