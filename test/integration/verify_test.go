package integration

import (
	"bytes"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"
	"time"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const day = 24 * time.Hour

func TestVerifyStateMachine(t *testing.T) {
	k := netkey("stack-admin-key-eeeeeeeeeeeeeeeeeeeeeee")

	t.Run("first verify is strict 3/3", func(t *testing.T) {
		e := newEnv(t)
		id, _ := e.register(k, "irc.strict.example", nil)
		e.dns.Set("irc.strict.example", 2)
		e.runWorker("verify")
		if e.status(id) != "pending" {
			t.Fatalf("2/3 verified a pending listing: %s", e.status(id))
		}
		e.dns.Set("irc.strict.example", 3)
		e.clock.Advance(61 * time.Minute)
		e.runWorker("verify")
		if e.status(id) != "verified" {
			t.Fatalf("3/3 didn't verify: %s", e.status(id))
		}
	})

	t.Run("expired pending stays pending (bug fix)", func(t *testing.T) {
		e := newEnv(t)
		id, _ := e.register(k, "irc.expired.example", nil)
		e.dns.Set("irc.expired.example", 0)
		e.runWorker("verify")
		e.clock.Advance(73 * time.Hour)
		e.dns.Set("irc.expired.example", 3) // the record shows up too late
		before := len(e.dns.Calls())
		e.runWorker("verify")
		if len(e.dns.Calls()) != before {
			t.Fatal("worker checked an expired pending token")
		}
		if e.status(id) != "pending" {
			t.Fatalf("expired pending became %s", e.status(id))
		}
		if e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 410).m(t)["error"] != "token_expired" {
			t.Fatal("want token_expired")
		}
		// Regenerate → a fresh 72h window.
		e.expect(e.do("POST", "/v1/networks/"+id+"/regenerate-token", k, nil), 200)
		e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 200)
		if e.status(id) != "verified" {
			t.Fatal("regenerated token didn't verify")
		}
	})

	t.Run("an expired claim doesn't hold the hostname", func(t *testing.T) {
		e := newEnv(t)
		e.register(k, "irc.squat.example", nil)
		other := netkey("stack-admin-key-fffffffffffffffffffffff")
		e.expect(e.do("POST", "/v1/networks", other, map[string]any{"hostname": "irc.squat.example", "port": 6697, "name": "x"}), 409)
		e.clock.Advance(73 * time.Hour)
		e.expect(e.do("POST", "/v1/networks", other, map[string]any{"hostname": "irc.squat.example", "port": 6697, "name": "x"}), 201)
	})

	t.Run("lapse after 14 days from last_verified_at (bug fix)", func(t *testing.T) {
		e := newEnv(t)
		id := e.registerVerified(k, "irc.lapse.example", nil)
		e.dns.Set("irc.lapse.example", 1) // re-checks fail from now on
		// Daily checks keep failing; last_checked_at moves every day but
		// the listing must still lapse 14 days after the last success.
		for i := 1; i <= 13; i++ {
			e.clock.Advance(day + time.Minute)
			e.runWorker("verify")
			if s := e.status(id); s != "verified" {
				t.Fatalf("day %d: %s, want verified within the 14 days", i, s)
			}
		}
		var checked, verified time.Time
		_ = e.sqlDB.QueryRow("SELECT last_checked_at, last_verified_at FROM networks WHERE id=$1", id).Scan(&checked, &verified)
		if !checked.After(verified.Add(12 * day)) {
			t.Fatalf("daily checks didn't run: checked %v verified %v", checked, verified)
		}
		e.clock.Advance(day + time.Minute)
		e.runWorker("verify")
		if s := e.status(id); s != "lapsed" {
			t.Fatalf("after 14 days: %s, want lapsed", s)
		}
		if len(e.list("q=lapse").Networks) != 0 {
			t.Fatal("lapsed listing still public")
		}
	})

	t.Run("re-check is lenient 2/3 and refreshes last_verified_at", func(t *testing.T) {
		e := newEnv(t)
		id := e.registerVerified(k, "irc.lenient.example", nil)
		e.dns.Set("irc.lenient.example", 2)
		for i := 0; i < 20; i++ {
			e.clock.Advance(day + time.Minute)
			e.runWorker("verify")
		}
		if s := e.status(id); s != "verified" {
			t.Fatalf("2/3 re-checks lapsed the listing: %s", s)
		}
	})

	t.Run("lapsed recovers on the weekly re-check (bug fix)", func(t *testing.T) {
		e := newEnv(t)
		id := e.registerVerified(k, "irc.recover.example", nil)
		e.exec("UPDATE networks SET status = 'lapsed', last_checked_at = $1 WHERE id = $2", e.clock.Now(), id)
		e.dns.Set("irc.recover.example", 2)
		e.clock.Advance(3 * day)
		before := len(e.dns.Calls())
		e.runWorker("verify")
		if len(e.dns.Calls()) != before {
			t.Fatal("lapsed listing re-checked before a week")
		}
		e.clock.Advance(5 * day)
		e.runWorker("verify")
		if s := e.status(id); s != "verified" {
			t.Fatalf("lapsed didn't recover: %s", s)
		}
		// The owner can trigger it too (lenient for lapsed).
		e.exec("UPDATE networks SET status = 'lapsed' WHERE id = $1", id)
		e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 200)
	})

	t.Run("a regenerate mid-check isn't overwritten", func(t *testing.T) {
		e := newEnv(t)
		id := e.registerVerified(k, "irc.race.example", nil)
		var token string
		_ = e.sqlDB.QueryRow("SELECT verification_token FROM networks WHERE id=$1", id).Scan(&token)
		ok, err := e.app.Deps.NetworkRepo.RecordVerify(t.Context(), recordStale(id))
		if err != nil || ok {
			t.Fatalf("stale token write applied: %v %v", ok, err)
		}
	})
}

func TestFeedSeqAndUpsertByOrigin(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-ggggggggggggggggggggggg")
	repo := e.app.Deps.NetworkRepo
	ctx := t.Context()
	local := e.registerVerified(k, "irc.local.example", nil)

	// Peer listing: conflicts with the local hostname.
	n := peerNet("peer:p1", "o1", "irc.local.example", "tokA")
	if res, err := repo.UpsertByOrigin(ctx, n); err != nil || res != "conflict" {
		t.Fatalf("local should win: %v %v", res, err)
	}
	n = peerNet("peer:p1", "o2", "irc.peer.example", "tokA")
	if res, err := repo.UpsertByOrigin(ctx, n); err != nil || res != "created" || n.Status != "pending" {
		t.Fatalf("create: %v %v %s", res, err, n.Status)
	}
	if res, _ := repo.UpsertByOrigin(ctx, peerNet("peer:p1", "o2", "irc.peer.example", "tokA")); res != "unchanged" {
		t.Fatalf("unchanged: %v", res)
	}
	// The local verifier checks the peer's token; imported rows aren't bound by the 72h TTL.
	e.clock.Advance(100 * time.Hour)
	e.dns.SetToken("irc.peer.example", "tokA")
	e.runWorker("verify")
	if e.status(n.ID) != "verified" {
		t.Fatalf("peer listing not verified locally: %s", e.status(n.ID))
	}
	// Another peer can't take a verified hostname.
	if res, _ := repo.UpsertByOrigin(ctx, peerNet("peer:p2", "x", "irc.peer.example", "tokB")); res != "conflict" {
		t.Fatalf("first verified peer should win: %v", res)
	}
	// Token change → back to pending.
	m := peerNet("peer:p1", "o2", "irc.peer.example", "tokC")
	if res, _ := repo.UpsertByOrigin(ctx, m); res != "updated" || e.status(n.ID) != "pending" {
		t.Fatalf("token change: %v %s", res, e.status(n.ID))
	}
	// A local registration replaces the peer listing.
	e.expect(e.do("POST", "/v1/networks", k, map[string]any{"hostname": "irc.peer.example", "port": 6697, "name": "mine now"}), 201)

	changes, err := repo.Changes(ctx, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	sawTomb := false
	for _, c := range changes {
		seqs = append(seqs, c.Seq)
		if c.Deleted != nil && c.Deleted.NetworkID == n.ID {
			sawTomb = true
		}
	}
	if !slices.IsSorted(seqs) || !sawTomb {
		t.Fatalf("feed seqs %v tombstone %v", seqs, sawTomb)
	}
	// Probe bookkeeping doesn't bump seq; real edits do.
	var s1, s2, s3 int64
	_ = e.sqlDB.QueryRow("SELECT seq FROM networks WHERE id=$1", local).Scan(&s1)
	e.exec("UPDATE networks SET last_checked_at = now(), health_checked_at = now() WHERE id=$1", local)
	_ = e.sqlDB.QueryRow("SELECT seq FROM networks WHERE id=$1", local).Scan(&s2)
	e.exec("UPDATE networks SET description = 'changed' WHERE id=$1", local)
	_ = e.sqlDB.QueryRow("SELECT seq FROM networks WHERE id=$1", local).Scan(&s3)
	if s1 != s2 || s3 <= s2 {
		t.Fatalf("seq %d %d %d", s1, s2, s3)
	}
	if c, _ := repo.DeleteAllByOrigin(ctx, "peer:p1"); c != 0 {
		t.Fatalf("deleted %d", c)
	}
}

func recordStale(id string) network.VerifyResult {
	now := time.Now()
	return network.VerifyResult{ID: id, CheckedToken: "an-old-token", Status: "lapsed", CheckedAt: now}
}

func peerNet(origin, originID, host, token string) *network.Network {
	oid := originID
	return &network.Network{
		Hostname: host, Port: 6697, TLS: true, Name: "peer " + host, Origin: origin, OriginID: &oid,
		VerificationToken: token, Tags: []string{}, Languages: []string{},
	}
}
