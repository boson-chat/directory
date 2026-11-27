package federation_test

import (
	"context"
	"slices"
	"testing"

	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/services/federation"
)

// A listing that moves to a new hostname reaches peers as an update of the
// same listing (same origin id, same row on the peer), never a delete +
// create; the peer re-verifies the new hostname itself and follows the old
// hostname to it. A pending move never reaches the feed.
func TestMoveReachesPeersAsAnUpdate(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	one := a.local("irc.ircforever.test", network.StatusVerified)
	id := b.addPeer(a.URL)
	if res := b.sync(id); res.Created != 1 || res.Verified != 1 {
		t.Fatalf("first sync: %+v", res)
	}
	imported := b.byHost("irc.ircforever.test")

	// Request the move on A (an operator manages any local listing).
	a.Fake.Set("irc.boson.ircforever.test", 0)
	var l struct {
		Move *struct {
			Token string `json:"token"`
		} `json:"move"`
	}
	if code := a.do("POST", "/v1/networks/"+one.ID+"/move", "op", map[string]any{"hostname": "irc.boson.ircforever.test"}, &l); code != 201 || l.Move == nil {
		t.Fatalf("request: %d %+v", code, l)
	}
	var feed federation.FeedPage
	a.do("GET", "/v1/feed?since="+itoa(one.Seq), "", nil, &feed)
	if len(feed.Changes) != 0 {
		t.Fatalf("a pending move reached the feed: %+v", feed.Changes)
	}
	if res := b.sync(id); res.Updated+res.Created+res.Deleted != 0 {
		t.Fatalf("sync during the pending move: %+v", res)
	}

	// Complete it on A.
	a.Fake.Set("irc.boson.ircforever.test", 3)
	if code := a.do("POST", "/v1/networks/"+one.ID+"/move/verify", "op", nil, nil); code != 200 {
		t.Fatalf("verify move: %d", code)
	}
	a.do("GET", "/v1/feed?since="+itoa(one.Seq), "", nil, &feed)
	if len(feed.Changes) != 1 {
		t.Fatalf("feed after the move: %+v", feed.Changes)
	}
	c := feed.Changes[0]
	if c.Deleted || c.Network.ID != one.ID || c.Network.Hostname != "irc.boson.ircforever.test" || c.VerificationToken != l.Move.Token ||
		!slices.Equal(c.Network.PreviousHostnames, []string{"irc.ircforever.test"}) {
		t.Fatalf("feed change: %+v", c)
	}

	// B: an update of the same row, re-verified by B's own DNS check.
	res := b.sync(id)
	if res.Updated != 1 || res.Created != 0 || res.Deleted != 0 || res.Verified != 1 {
		t.Fatalf("sync after the move: %+v", res)
	}
	if b.byHost("irc.ircforever.test") != nil {
		t.Fatal("the old hostname is still imported")
	}
	got := b.byHost("irc.boson.ircforever.test")
	if got == nil || got.ID != imported.ID || *got.OriginID != one.ID || got.Status != network.StatusVerified ||
		!slices.Equal(got.PreviousHostnames, []string{"irc.ircforever.test"}) {
		t.Fatalf("imported after the move: %+v", got)
	}
	// B resolves the old hostname to the moved listing.
	var lk struct {
		Network struct {
			ID       string `json:"id"`
			Hostname string `json:"hostname"`
		} `json:"network"`
		MovedFrom string `json:"moved_from"`
	}
	if code := b.do("GET", "/v1/networks/lookup?hostname=irc.ircforever.test", "", nil, &lk); code != 200 ||
		lk.Network.ID != imported.ID || lk.MovedFrom != "irc.ircforever.test" || lk.Network.Hostname != "irc.boson.ircforever.test" {
		t.Fatalf("lookup of the old hostname on the peer: %d %+v", code, lk)
	}

	// B can't see the new record yet: the import waits as pending (hidden),
	// it is not deleted.
	a.Fake.Set("irc.third.test", 3)
	a.do("POST", "/v1/networks/"+one.ID+"/move", "op", map[string]any{"hostname": "irc.third.test"}, nil)
	// (The owner's check is rate limited; A's verify worker completes it.)
	if err := a.App.RunWorkerOnce(context.Background(), "verify"); err != nil {
		t.Fatal(err)
	}
	b.Fake.Set("irc.third.test", 0)
	if res := b.sync(id); res.Updated != 1 || res.Deleted != 0 || res.Verified != 0 {
		t.Fatalf("sync of the second move: %+v", res)
	}
	if got := b.byHost("irc.third.test"); got == nil || got.ID != imported.ID || got.Status != network.StatusPending ||
		!slices.Equal(got.PreviousHostnames, []string{"irc.boson.ircforever.test", "irc.ircforever.test"}) {
		t.Fatalf("pending re-verification on the peer: %+v", got)
	}
}
