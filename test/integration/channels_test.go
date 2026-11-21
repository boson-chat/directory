package integration

import (
	"slices"
	"strings"
	"testing"
)

type channelsView struct {
	Network struct {
		Channels         []string `json:"channels"`
		AutojoinChannels []string `json:"autojoin_channels"`
	} `json:"network"`
}

func (e *env) channelsOf(path, auth string) channelsView {
	e.t.Helper()
	var v channelsView
	e.expect(e.do("GET", path, auth, nil), 200).json(e.t, &v)
	return v
}

func TestChannelsRoundTrip(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-channels-000000000000")
	id := e.registerVerified(k, "irc.chan.example", map[string]any{
		"channels": []string{"#Lobby", "#lobby", "#help"}, "autojoin_channels": []string{"#Lobby"},
	})
	base := "/v1/networks/" + id
	v := e.channelsOf(base, "")
	if !slices.Equal(v.Network.Channels, []string{"#Lobby", "#help"}) || !slices.Equal(v.Network.AutojoinChannels, []string{"#Lobby"}) {
		t.Fatalf("after create: %+v", v)
	}

	// PATCH with the Network key (the stack admin push) replaces each list.
	e.expect(e.do("PATCH", base, k, map[string]any{
		"channels": []string{"#lobby", "#Dev", "#DEV"}, "autojoin_channels": []string{"#lobby", "#dev"},
	}), 200)
	v = e.channelsOf(base, "")
	if !slices.Equal(v.Network.Channels, []string{"#lobby", "#Dev"}) || !slices.Equal(v.Network.AutojoinChannels, []string{"#lobby", "#dev"}) {
		t.Fatalf("after patch: %+v", v)
	}
	// Other edits (and null) leave them alone.
	e.expect(e.do("PATCH", base, k, map[string]any{"description": "hi", "channels": nil}), 200)
	if v = e.channelsOf(base, ""); len(v.Network.Channels) != 2 {
		t.Fatalf("unrelated patch changed channels: %+v", v)
	}

	// Lists, lookup and the feed carry them.
	var list struct {
		Networks []struct {
			ID               string   `json:"id"`
			AutojoinChannels []string `json:"autojoin_channels"`
		} `json:"networks"`
	}
	e.expect(e.do("GET", "/v1/networks", "", nil), 200).json(t, &list)
	if len(list.Networks) != 1 || !slices.Equal(list.Networks[0].AutojoinChannels, []string{"#lobby", "#dev"}) {
		t.Fatalf("list: %+v", list)
	}
	if v = e.channelsOf("/v1/networks/lookup?hostname=irc.chan.example", ""); len(v.Network.AutojoinChannels) != 2 {
		t.Fatalf("lookup: %+v", v)
	}
	feed := string(e.expect(e.do("GET", "/v1/feed", "", nil), 200).Body)
	if !strings.Contains(feed, `"channels":["#lobby","#Dev"],"autojoin_channels":["#lobby","#dev"]`) {
		t.Fatalf("feed: %s", feed)
	}

	// Invalid names and too many are refused; [] clears.
	for name, body := range map[string]map[string]any{
		"space":    {"channels": []string{"#a b"}},
		"no hash":  {"autojoin_channels": []string{"lobby"}},
		"comma":    {"channels": []string{"#a,#b"}},
		"too long": {"channels": []string{"#" + strings.Repeat("x", 64)}},
		"too many": {"channels": many(51)},
	} {
		r := e.do("PATCH", base, k, body)
		if r.Status != 400 || !strings.Contains(string(r.Body), `"error":"invalid_`) {
			t.Errorf("%s: %d %s", name, r.Status, r.Body)
		}
	}
	e.expect(e.do("PATCH", base, k, map[string]any{"channels": many(50)}), 200)
	e.expect(e.do("PATCH", base, k, map[string]any{"channels": []string{}, "autojoin_channels": []string{}}), 200)
	v = e.channelsOf(base, "")
	if v.Network.Channels == nil || len(v.Network.Channels) != 0 || v.Network.AutojoinChannels == nil || len(v.Network.AutojoinChannels) != 0 {
		t.Fatalf("after clearing (must be [] not null): %+v", v)
	}
}

// Rows from before the migration (inserted without the columns) read as [].
func TestChannelsDefaultEmpty(t *testing.T) {
	e := newEnv(t)
	e.seed("old", 1)
	var nullCount int
	if err := e.sqlDB.QueryRow(`SELECT count(*) FROM networks WHERE channels IS NULL OR autojoin_channels IS NULL
		OR channels <> '{}' OR autojoin_channels <> '{}'`).Scan(&nullCount); err != nil {
		t.Fatal(err)
	}
	if nullCount != 0 {
		t.Fatalf("%d rows without the '{}' default", nullCount)
	}
	r := string(e.expect(e.do("GET", "/v1/networks/net_old000", "", nil), 200).Body)
	if !strings.Contains(r, `"channels":[]`) || !strings.Contains(r, `"autojoin_channels":[]`) {
		t.Fatalf("old row: %s", r)
	}
}

func many(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "#c" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	return out
}
