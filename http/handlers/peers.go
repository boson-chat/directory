package handlers

import (
	"context"
	"sync"
	"time"

	"github.com/boson-chat/directory/http/views"
	"github.com/boson-chat/directory/internal/db/repositories/network"
	"github.com/boson-chat/directory/internal/db/repositories/peer"
	"github.com/boson-chat/directory/internal/logger"
)

// peerCache labels peer origins (origin_name, facet sources). Peers change
// rarely; a short cache keeps lists from querying them per row.
type peerCache struct {
	mu    sync.Mutex
	at    time.Time
	peers []peer.Peer
}

func (h *Handlers) peerList(ctx context.Context) []peer.Peer {
	c := &h.peers
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peers != nil && time.Since(c.at) < 30*time.Second {
		return c.peers
	}
	var list []peer.Peer
	if err := h.d.DB.Conn(ctx).Order("name, url").Find(&list).Error; err != nil {
		logger.FromCtx(ctx).Warn().Err(err).Msg("loading peers")
		return c.peers
	}
	if list == nil {
		list = []peer.Peer{}
	}
	c.peers, c.at = list, time.Now()
	return list
}

func peerLabel(p peer.Peer) string {
	if p.Name != "" {
		return p.Name
	}
	return p.URL
}

func (h *Handlers) peerNames(ctx context.Context) views.PeerNames {
	byOrigin := map[string]string{}
	for _, p := range h.peerList(ctx) {
		byOrigin[network.PeerOrigin(p.ID)] = peerLabel(p)
	}
	return func(origin string) *string {
		if s, ok := byOrigin[origin]; ok {
			return &s
		}
		return nil
	}
}
