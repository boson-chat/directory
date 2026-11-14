package handlers

import (
	nethttp "net/http"
	"sync"
	"time"

	"github.com/boson-chat/directory/http/router"
	"github.com/boson-chat/directory/internal/apperr"
	"github.com/boson-chat/directory/internal/reqmeta"
	"golang.org/x/time/rate"
)

var errTooMany = apperr.New(nethttp.StatusTooManyRequests, "rate_limited", "too many requests; slow down")

// ipLimiter is a token bucket per client IP.
type ipLimiter struct {
	mu      sync.Mutex
	perSec  rate.Limit
	burst   int
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	l    *rate.Limiter
	seen time.Time
}

func newIPLimiter(perMinute float64, burst int) *ipLimiter {
	return &ipLimiter{perSec: rate.Limit(perMinute / 60), burst: max(burst, 1), buckets: map[string]*bucket{}}
}

func (l *ipLimiter) allow(ip string) bool {
	if l == nil || l.perSec <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.seen) > 30*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{l: rate.NewLimiter(l.perSec, l.burst)}
		l.buckets[ip] = b
	}
	b.seen = now
	return b.l.Allow()
}

// writes rate-limits owner write requests per client IP.
func (h *Handlers) writes(next nethttp.HandlerFunc) nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !h.writeLimit.allow(reqmeta.From(r.Context()).IP) {
			h.d.Metrics.RateLimited(r.Context(), "writes")
			w.Header().Set("Retry-After", "10")
			router.WriteError(w, r, errTooMany)
			return
		}
		next(w, r)
	}
}
