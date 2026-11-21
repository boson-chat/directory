package federation_test

import (
	"context"
	"slices"
	"testing"

	"github.com/boson-chat/directory/internal/db/repositories/network"
)

// A peer imports channels and autojoin_channels and follows edits; a
// listing without them (an old origin) imports with empty lists.
func TestImportCarriesChannels(t *testing.T) {
	a := newInstance(t)
	b := newInstance(t)
	one := a.local("irc.chan.test", network.StatusVerified)
	a.local("irc.plain.test", network.StatusVerified)
	one.Channels, one.AutojoinChannels = []string{"#Lobby", "#help"}, []string{"#Lobby"}
	if err := a.App.Deps.NetworkRepo.Update(context.Background(), one, "channels", "autojoin_channels"); err != nil {
		t.Fatal(err)
	}

	id := b.addPeer(a.URL)
	if res := b.sync(id); res.Created != 2 {
		t.Fatalf("sync: %+v", res)
	}
	got := b.byHost("irc.chan.test")
	if !slices.Equal(got.Channels, []string{"#Lobby", "#help"}) || !slices.Equal(got.AutojoinChannels, []string{"#Lobby"}) {
		t.Fatalf("imported channels %q autojoin %q", got.Channels, got.AutojoinChannels)
	}
	if plain := b.byHost("irc.plain.test"); plain.Channels == nil || len(plain.Channels) != 0 || len(plain.AutojoinChannels) != 0 {
		t.Fatalf("listing without channels: %q %q", plain.Channels, plain.AutojoinChannels)
	}

	// Public JSON on B.
	var view struct {
		Network struct {
			Channels         []string `json:"channels"`
			AutojoinChannels []string `json:"autojoin_channels"`
		} `json:"network"`
	}
	b.do("GET", "/v1/networks/"+got.ID, "", nil, &view)
	if !slices.Equal(view.Network.AutojoinChannels, []string{"#Lobby"}) || len(view.Network.Channels) != 2 {
		t.Fatalf("public view on the peer: %+v", view)
	}

	// Clearing at the origin propagates.
	one.Channels, one.AutojoinChannels = []string{}, []string{}
	if err := a.App.Deps.NetworkRepo.Update(context.Background(), one, "channels", "autojoin_channels"); err != nil {
		t.Fatal(err)
	}
	if res := b.sync(id); res.Updated != 1 {
		t.Fatalf("incremental sync: %+v", res)
	}
	if got := b.byHost("irc.chan.test"); len(got.Channels)+len(got.AutojoinChannels) != 0 {
		t.Fatalf("after clearing: %q %q", got.Channels, got.AutojoinChannels)
	}
}
