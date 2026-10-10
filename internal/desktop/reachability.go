package desktop

import (
	"time"

	"linklan/internal/model"
)

type echoState struct {
	Measured, Reachable bool
	RTT                 time.Duration
}

// Image 0: not measured; 1: no echo reply; 2..5: one to four green bars.
func signalImage(state echoState) int32 {
	if !state.Measured {
		return 0
	}
	if !state.Reachable {
		return 1
	}
	switch {
	case state.RTT <= 50*time.Millisecond:
		return 5
	case state.RTT <= 150*time.Millisecond:
		return 4
	case state.RTT <= 300*time.Millisecond:
		return 3
	default:
		return 2
	}
}
func visiblePeers(peers []model.Peer, ownID string) []model.Peer {
	out := make([]model.Peer, 0, len(peers))
	for _, peer := range peers {
		if peer.ID != ownID {
			out = append(out, peer)
		}
	}
	return out
}
