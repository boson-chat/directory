// Package config holds the service configuration, loaded from env vars.
//
// Durations use Go syntax (15m, 720h); days are not accepted, so 14 days is 336h.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/jinzhu/configor"
)

type Config struct {
	AppConfig        AppConfig
	HTTPConfig       HTTPConfig
	DBConfig         DBConfig
	AuthConfig       AuthConfig
	DirectoryConfig  DirectoryConfig
	VerifyConfig     VerifyConfig
	HealthConfig     HealthConfig
	MediaConfig      MediaConfig
	FederationConfig FederationConfig
	TelemetryConfig  TelemetryConfig
}

type AppConfig struct {
	APPName  string `default:"directory"`
	Env      string `default:"development" env:"ENV"`
	Version  string `default:"dev" env:"VERSION"`
	LogLevel string `default:"info" env:"LOG_LEVEL"`
}

// IsProduction reports ENV=production (the image default).
func (a AppConfig) IsProduction() bool { return strings.EqualFold(a.Env, "production") }

type HTTPConfig struct {
	// Public API + web UI listener (behind the reverse proxy).
	Addr string `default:":8080" env:"HTTP_ADDR"`
	// Internal listener (/metrics). Never expose publicly.
	InternalAddr string `default:":8081" env:"INTERNAL_ADDR"`
	// Externally visible base URL, e.g. https://directory.boson.chat.
	PublicURL string `default:"http://localhost:8080" env:"PUBLIC_URL"`
	// Comma-separated CIDRs (or IPs) whose X-Forwarded-For is trusted.
	TrustedProxies string `env:"TRUSTED_PROXIES"`
	// Comma-separated origins allowed to make credentialed (cookie) calls.
	// GETs on the public API are CORS * regardless.
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS"`
}

type DBConfig struct {
	// Full connection URL; when set it wins over the DB_* parts.
	URL      string `env:"DATABASE_URL"`
	Host     string `default:"localhost" env:"DB_HOST"`
	Port     uint   `default:"5432" env:"DB_PORT"`
	User     string `default:"directory" env:"DB_USER"`
	Password string `env:"DB_PASSWORD"`
	Name     string `default:"directory" env:"DB_NAME"`
	SSLMode  string `default:"disable" env:"DB_SSLMODE"`
	// Run migrations up on `serve` start.
	AutoMigrate bool `default:"true" env:"AUTO_MIGRATE"`
	MaxConns    int  `default:"20" env:"DB_MAX_CONNS"`
}

// DSN returns the connection URL.
func (c DBConfig) DSN() string {
	if c.URL != "" {
		return c.URL
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:     "/" + c.Name,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
	return u.String()
}

// AuthConfig points at the boson auth service whose access tokens sign
// owners and operators in. Empty AUTH_ISSUER turns account sign-in off
// (only Network keys work then).
type AuthConfig struct {
	Issuer string `env:"AUTH_ISSUER"`
	// Defaults to AUTH_ISSUER + /.well-known/jwks.json.
	JWKSURL  string `env:"AUTH_JWKS_URL"`
	Audience string `default:"boson" env:"AUTH_AUDIENCE"`
	// Cookie the auth service sets with the access token (browser flow).
	AccessCookie string `default:"boson_at" env:"AUTH_ACCESS_COOKIE"`
}

// Enabled reports whether account (bearer) auth is configured.
func (a AuthConfig) Enabled() bool { return a.Issuer != "" }

type DirectoryConfig struct {
	// Shown in /.well-known/boson-directory.json.
	Name     string `default:"boson directory" env:"DIRECTORY_NAME"`
	Operator string `env:"DIRECTORY_OPERATOR"`
	Contact  string `env:"DIRECTORY_CONTACT"`
	// Owner write requests per client IP per minute (create, edit, upload).
	WritesPerMinute float64 `default:"30" env:"RATE_LIMIT_WRITES_PER_MINUTE"`
	WriteBurst      int     `default:"10" env:"RATE_LIMIT_WRITE_BURST"`
}

type VerifyConfig struct {
	// real (default) | fake (always succeeds; refused when ENV=production).
	Verifier string `default:"real" env:"DNS_VERIFIER"`
	// Comma-separated provider=host:port list replacing the default
	// Cloudflare/Google/Quad9 triple, e.g. "a=127.0.0.1:5353,b=...".
	Resolvers string `env:"DNS_RESOLVERS"`
	// Per-resolver query budget.
	ResolverTimeout time.Duration `default:"5s" env:"DNS_RESOLVER_TIMEOUT"`
	// A first verification must succeed within this long of token issue.
	TokenTTL time.Duration `default:"72h" env:"VERIFY_TOKEN_TTL"`
	// A verified listing lapses after this long without a successful check
	// (measured from last_verified_at).
	LapseAfter time.Duration `default:"336h" env:"VERIFY_LAPSE_AFTER"`
	// Re-check cadence per status.
	VerifiedEvery time.Duration `default:"24h" env:"VERIFY_VERIFIED_EVERY"`
	PendingEvery  time.Duration `default:"1h" env:"VERIFY_PENDING_EVERY"`
	LapsedEvery   time.Duration `default:"168h" env:"VERIFY_LAPSED_EVERY"`
	// How often the verify worker looks for due listings.
	WorkerTick time.Duration `default:"5m" env:"VERIFY_WORKER_TICK"`
	// Minimum gap between owner-triggered verifies of one network.
	OwnerRateLimit time.Duration `default:"30s" env:"VERIFY_RATE_LIMIT"`
	// Run the background verify worker.
	WorkerEnabled bool `default:"true" env:"VERIFY_WORKER_ENABLED"`
}

type HealthConfig struct {
	WorkerEnabled bool          `default:"true" env:"HEALTH_WORKER_ENABLED"`
	Every         time.Duration `default:"15m" env:"HEALTH_EVERY"`
	DialTimeout   time.Duration `default:"5s" env:"HEALTH_DIAL_TIMEOUT"`
	// Register on the IRC server and read LUSERS for user_count.
	UserCount bool `default:"true" env:"HEALTH_USER_COUNT"`
	// Minimum gap between user-count reads of one network.
	UserCountEvery time.Duration `default:"1h" env:"HEALTH_USER_COUNT_EVERY"`
	// Parallel probes.
	Concurrency int `default:"8" env:"HEALTH_CONCURRENCY"`
}

// MediaConfig: icons and banners go to a local directory (served at
// /media/) unless an S3/R2 bucket is configured.
type MediaConfig struct {
	Dir string `default:"./data/media" env:"MEDIA_DIR"`
	// Base URL images are served from. Defaults to PUBLIC_URL/media (local)
	// and is required for S3 (the bucket's public/CDN URL).
	PublicURL   string `env:"MEDIA_PUBLIC_URL"`
	S3Endpoint  string `env:"MEDIA_S3_ENDPOINT"`
	S3Bucket    string `env:"MEDIA_S3_BUCKET"`
	S3Region    string `default:"auto" env:"MEDIA_S3_REGION"`
	S3AccessKey string `env:"MEDIA_S3_ACCESS_KEY"`
	S3SecretKey string `env:"MEDIA_S3_SECRET_KEY"`
}

// UseS3 reports whether the S3 backend is configured.
func (m MediaConfig) UseS3() bool { return m.S3Bucket != "" }

// FederationConfig is read by internal/services/federation.
type FederationConfig struct {
	// Built-in main peer; skipped when it is this directory itself.
	MainPeer    string `default:"https://directory.boson.chat" env:"DIRECTORY_MAIN_PEER"`
	IncludeMain bool   `default:"true" env:"DIRECTORY_INCLUDE_MAIN"`
	// Comma-separated extra peer URLs seeded on start.
	Peers     string        `env:"DIRECTORY_PEERS"`
	SyncEvery time.Duration `default:"10m" env:"DIRECTORY_SYNC_EVERY"`
}

// TelemetryConfig controls metrics export. Prometheus on the internal
// listener's /metrics is always on; OTLP push is on when an endpoint is set.
type TelemetryConfig struct {
	OTLPEndpoint        string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTLPMetricsEndpoint string `env:"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"`
	OTLPProtocol        string `env:"OTEL_EXPORTER_OTLP_PROTOCOL"`
	OTLPMetricsProtocol string `env:"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"`
	MetricsExporter     string `env:"OTEL_METRICS_EXPORTER"`
}

// LoadConfig reads the configuration from the environment and fills derived defaults.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := configor.New(&configor.Config{ENVPrefix: "-", Silent: true}).Load(&cfg); err != nil {
		return cfg, err
	}
	cfg.ApplyDefaults()
	return cfg, cfg.Validate()
}

// ApplyDefaults fills values derived from other settings.
func (c *Config) ApplyDefaults() {
	c.HTTPConfig.PublicURL = strings.TrimRight(c.HTTPConfig.PublicURL, "/")
	c.AuthConfig.Issuer = strings.TrimRight(c.AuthConfig.Issuer, "/")
	if c.AuthConfig.JWKSURL == "" && c.AuthConfig.Issuer != "" {
		c.AuthConfig.JWKSURL = c.AuthConfig.Issuer + "/.well-known/jwks.json"
	}
	if c.MediaConfig.PublicURL == "" && !c.MediaConfig.UseS3() {
		c.MediaConfig.PublicURL = c.HTTPConfig.PublicURL + "/media"
	}
	c.MediaConfig.PublicURL = strings.TrimRight(c.MediaConfig.PublicURL, "/")
	c.FederationConfig.MainPeer = strings.TrimRight(c.FederationConfig.MainPeer, "/")
}

// Validate checks settings that would otherwise fail confusingly later.
func (c Config) Validate() error {
	switch strings.ToLower(c.VerifyConfig.Verifier) {
	case "real", "":
	case "fake":
		if c.AppConfig.IsProduction() {
			return errors.New("DNS_VERIFIER=fake is not allowed with ENV=production")
		}
	default:
		return fmt.Errorf("DNS_VERIFIER must be real or fake, got %q", c.VerifyConfig.Verifier)
	}
	v := c.VerifyConfig
	if v.TokenTTL <= 0 || v.LapseAfter <= 0 || v.VerifiedEvery <= 0 || v.PendingEvery <= 0 || v.LapsedEvery <= 0 || v.WorkerTick <= 0 {
		return errors.New("VERIFY_* durations must be positive")
	}
	if c.HealthConfig.Every <= 0 || c.HealthConfig.DialTimeout <= 0 {
		return errors.New("HEALTH_EVERY and HEALTH_DIAL_TIMEOUT must be positive")
	}
	if c.MediaConfig.UseS3() && c.MediaConfig.PublicURL == "" {
		return errors.New("MEDIA_PUBLIC_URL is required with MEDIA_S3_BUCKET")
	}
	if _, err := c.HTTPConfig.TrustedProxyPrefixes(); err != nil {
		return err
	}
	return nil
}

// TrustedProxyPrefixes parses TRUSTED_PROXIES.
func (c HTTPConfig) TrustedProxyPrefixes() ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range SplitList(c.TrustedProxies) {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an IP or CIDR", s)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

// SplitList splits a comma/space-separated env value.
func SplitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
}
