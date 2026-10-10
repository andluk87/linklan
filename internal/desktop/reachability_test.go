package desktop

import (
	"linklan/internal/model"
	"testing"
	"time"
)

func TestOwnComputerIsHiddenWithoutChangingMembership(t *testing.T) {
	peers := []model.Peer{{ID: "own", Name: "This PC"}, {ID: "friend", Name: "Friend"}, {ID: "pending", Status: "pending"}}
	shown := visiblePeers(peers, "own")
	if len(shown) != 2 || shown[0].ID != "friend" || shown[1].ID != "pending" {
		t.Fatal(shown)
	}
	if len(peers) != 3 || peers[0].ID != "own" {
		t.Fatal("membership was mutated")
	}
}
func TestICMPBarsDistinguishUnmeasuredUnreachableAndLatency(t *testing.T) {
	cases := []struct {
		state echoState
		image int32
	}{
		{echoState{}, 0},
		{echoState{Measured: true}, 1},
		{echoState{Measured: true, Reachable: true, RTT: 0}, 5},
		{echoState{Measured: true, Reachable: true, RTT: 50 * time.Millisecond}, 5},
		{echoState{Measured: true, Reachable: true, RTT: 51 * time.Millisecond}, 4},
		{echoState{Measured: true, Reachable: true, RTT: 151 * time.Millisecond}, 3},
		{echoState{Measured: true, Reachable: true, RTT: 301 * time.Millisecond}, 2},
	}
	for _, test := range cases {
		if got := signalImage(test.state); got != test.image {
			t.Fatalf("%+v: image %d, want %d", test.state, got, test.image)
		}
	}
}
