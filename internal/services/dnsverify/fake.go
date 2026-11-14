package dnsverify

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Fake is a Verifier for tests and CI (DNS_VERIFIER=fake). By default every
// check succeeds on every resolver. Set(hostname, matches) makes a hostname
// match on only the first `matches` resolvers (0 = record missing), and
// SetToken publishes a specific token, so tests can drive every state
// transition without a DNS server.
type Fake struct {
	mu      sync.Mutex
	matches map[string]int
	tokens  map[string]string
	calls   []string
}

// NewFake returns a Fake that always succeeds.
func NewFake() *Fake { return &Fake{matches: map[string]int{}, tokens: map[string]string{}} }

// Set makes hostname match on n of the 3 resolvers.
func (f *Fake) Set(hostname string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.matches[strings.ToLower(hostname)] = n
}

// SetToken makes only token match for hostname (any other token misses).
func (f *Fake) SetToken(hostname, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[strings.ToLower(hostname)] = token
}

// Calls returns the hostnames checked so far.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *Fake) Verify(_ context.Context, hostname, token string, mode Mode) (Report, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" || strings.TrimSpace(token) == "" {
		return Report{}, ErrInvalidInput
	}
	f.mu.Lock()
	f.calls = append(f.calls, hostname)
	n, ok := f.matches[hostname]
	if !ok {
		n = len(DefaultResolvers)
	}
	if want, ok := f.tokens[hostname]; ok && want != token {
		n = 0
	}
	f.mu.Unlock()
	results := map[string]Result{}
	matched := 0
	for i, r := range DefaultResolvers {
		if i < n {
			results[r.Provider] = Result{Outcome: OutcomeMatch, Records: []string{"boson-verify=" + token}}
			matched++
		} else {
			results[r.Provider] = Result{Outcome: OutcomeMissingRecord, Detail: "no TXT record contained the expected token"}
		}
	}
	threshold := len(DefaultResolvers)
	if mode == ModeLenient {
		threshold = len(DefaultResolvers)/2 + 1
	}
	return Report{Success: matched >= threshold, Mode: mode, Token: token, Results: results}, nil
}

// ParseResolvers parses DNS_RESOLVERS: "provider=host:port,…" (a bare
// host:port gets the provider name r1, r2, …).
func ParseResolvers(list []string) ([]Resolver, error) {
	out := make([]Resolver, 0, len(list))
	for i, item := range list {
		name, addr, ok := strings.Cut(item, "=")
		if !ok {
			name, addr = fmt.Sprintf("r%d", i+1), item
		}
		if name == "" || !strings.Contains(addr, ":") {
			return nil, fmt.Errorf("DNS_RESOLVERS: %q is not provider=host:port", item)
		}
		out = append(out, Resolver{Provider: name, Addr: addr})
	}
	return out, nil
}

// RecordName is the TXT record name for hostname.
func RecordName(hostname string) string { return "_boson." + hostname }

// RecordValue is the TXT record value for token.
func RecordValue(token string) string { return "boson-verify=" + token }
