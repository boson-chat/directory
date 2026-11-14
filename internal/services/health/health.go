// Package health probes listed IRC servers: a TLS/TCP dial plus the first
// byte of the welcome banner (up | down | unknown), and optionally a full
// IRC registration to read the LUSERS numerics for user_count.
package health

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/logger"
	"github.com/boson-chat/directory/metrics"
)

// Result of one probe.
type Result struct {
	Status    string
	UserCount *int
}

// Prober probes one listing.
type Prober interface {
	Probe(ctx context.Context, n *network.Network, wantUsers bool) Result
}

// IRCProber is the real network prober.
type IRCProber struct {
	DialTimeout time.Duration
	// ReadTimeout bounds the whole LUSERS exchange.
	ReadTimeout time.Duration
	// InsecureSkipVerify is for tests against self-signed servers.
	InsecureSkipVerify bool
}

func (p IRCProber) Probe(ctx context.Context, n *network.Network, wantUsers bool) Result {
	dt := p.DialTimeout
	if dt <= 0 {
		dt = 5 * time.Second
	}
	addr := net.JoinHostPort(n.Hostname, strconv.Itoa(n.Port))
	dctx, cancel := context.WithTimeout(ctx, dt)
	defer cancel()
	d := &net.Dialer{Timeout: dt}
	var conn net.Conn
	var err error
	if n.TLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{
			ServerName: n.Hostname, MinVersion: tls.VersionTLS12, InsecureSkipVerify: p.InsecureSkipVerify, //nolint:gosec // tests only
		}}).DialContext(dctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(dctx, "tcp", addr)
	}
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return Result{Status: network.HealthUnknown}
		}
		return Result{Status: network.HealthDown}
	}
	defer conn.Close()
	if !wantUsers {
		_ = conn.SetReadDeadline(time.Now().Add(dt))
		if _, err := conn.Read(make([]byte, 1)); err != nil {
			return Result{Status: network.HealthUnknown}
		}
		return Result{Status: network.HealthUp}
	}
	rt := p.ReadTimeout
	if rt <= 0 {
		rt = 15 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(rt))
	count, gotAny := readLusers(conn)
	if !gotAny {
		return Result{Status: network.HealthUnknown}
	}
	return Result{Status: network.HealthUp, UserCount: count}
}

var digits = regexp.MustCompile(`\d+`)

// readLusers registers with a throwaway nick and reads RPL_GLOBALUSERS
// (266), falling back to RPL_LUSERCLIENT (251). gotAny is true when the
// server sent anything at all.
func readLusers(conn net.Conn) (count *int, gotAny bool) {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	nick := "bdir" + hex.EncodeToString(b)
	w := bufio.NewWriter(conn)
	_, _ = w.WriteString("NICK " + nick + "\r\nUSER directory 0 * :boson directory health check\r\n")
	_ = w.Flush()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), 16384)
	var fallback *int
	quit := func() { _, _ = conn.Write([]byte("QUIT :boson directory health check\r\n")) }
	for sc.Scan() {
		gotAny = true
		line := sc.Text()
		cmd, params, trailing := parse(line)
		switch cmd {
		case "PING":
			_, _ = conn.Write([]byte("PONG :" + trailing + "\r\n"))
		case "266":
			// :srv 266 nick [cur max] :Current global users: 12, max 30
			if len(params) >= 3 {
				if v, err := strconv.Atoi(params[1]); err == nil {
					quit()
					return &v, true
				}
			}
			if m := digits.FindString(trailing); m != "" {
				v, _ := strconv.Atoi(m)
				quit()
				return &v, true
			}
		case "251":
			// :There are 5 users and 7 invisible on 2 servers
			nums := digits.FindAllString(trailing, -1)
			if len(nums) >= 2 {
				a, _ := strconv.Atoi(nums[0])
				b, _ := strconv.Atoi(nums[1])
				v := a + b
				fallback = &v
			}
		case "376", "422", "ERROR":
			quit()
			return fallback, true
		case "433":
			quit()
			return fallback, true
		}
	}
	return fallback, gotAny
}

// parse splits an IRC line into command, middle params and trailing.
func parse(line string) (string, []string, string) {
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, "@") {
		if _, rest, ok := strings.Cut(line, " "); ok {
			line = rest
		}
	}
	if strings.HasPrefix(line, ":") {
		_, rest, _ := strings.Cut(line, " ")
		line = rest
	}
	head, trailing, _ := strings.Cut(line, " :")
	f := strings.Fields(head)
	if len(f) == 0 {
		return "", nil, trailing
	}
	return strings.ToUpper(f[0]), f[1:], trailing
}

// Checker runs probes over every verified listing that is due.
type Checker struct {
	Repo           network.NetworkRepositoryImpl
	Prober         Prober
	Every          time.Duration
	UserCount      bool
	UserCountEvery time.Duration
	Concurrency    int
	Metrics        *metrics.Metrics
	Now            func() time.Time
}

// RunDue probes listings not probed within Every; returns how many.
func (c *Checker) RunDue(ctx context.Context) (int, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	m := c.Metrics
	if m == nil {
		m = metrics.Noop()
	}
	// A little slack so a 15m ticker doesn't miss rows probed 14m59s ago.
	due, err := c.Repo.DueForHealth(ctx, now().Add(-c.Every+time.Minute), 1000)
	if err != nil {
		return 0, err
	}
	conc := max(c.Concurrency, 1)
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, n := range due {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(n *network.Network) {
			defer func() { <-sem; wg.Done() }()
			want := c.UserCount && (n.UserCountAt == nil || now().Sub(*n.UserCountAt) >= c.UserCountEvery)
			r := c.Prober.Probe(ctx, n, want)
			m.HealthProbe(ctx, r.Status)
			if err := c.Repo.RecordHealth(ctx, network.HealthResult{ID: n.ID, Status: r.Status, CheckedAt: now().UTC(), UserCount: r.UserCount}); err != nil {
				logger.FromCtx(ctx).Warn().Err(err).Str("hostname", n.Hostname).Msg("recording health")
			}
		}(n)
	}
	wg.Wait()
	return len(due), nil
}
