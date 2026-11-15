package desktop

import (
	"testing"
	"time"
)

func TestShortReconnectDoesNotRepeatDesktopNotifications(t *testing.T) {
	var s notificationState
	start := time.Unix(1, 0)
	for _, step := range []struct {
		seconds     int
		state, want string
	}{{0, "offline", ""}, {1, "online", ""}, {3, "online", "online"}, {4, "connecting", ""}, {5, "online", ""}, {7, "online", ""}, {10, "connecting", ""}, {16, "connecting", "offline"}, {17, "online", ""}, {19, "online", "online"}} {
		if got := s.observe(step.state, start.Add(time.Duration(step.seconds)*time.Second)); got != step.want {
			t.Fatalf("at %ds: notification %q, want %q", step.seconds, got, step.want)
		}
	}
}
