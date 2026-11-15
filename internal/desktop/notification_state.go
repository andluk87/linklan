package desktop

import "time"

// Announce stable changes, not short disconnect/reconnect transitions.
type notificationState struct {
	observed, announced string
	since               time.Time
}

func (s *notificationState) observe(current string, now time.Time) string {
	if s.observed == "" {
		s.observed = current
		s.since = now
		s.announced = "offline"
	}
	if s.observed != current {
		s.observed = current
		s.since = now
	}
	if current == "online" && s.announced != "online" && now.Sub(s.since) >= 1500*time.Millisecond {
		s.announced = "online"
		return "online"
	}
	if current != "online" && s.announced == "online" && now.Sub(s.since) >= 5*time.Second {
		s.announced = "offline"
		return "offline"
	}
	return ""
}
