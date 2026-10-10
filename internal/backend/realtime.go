package backend

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) error {
	// Desktop uses Authorization headers, never long-lived URL tokens.
	up := websocket.Upgrader{HandshakeTimeout: 10 * time.Second, CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }}
	conn, e := up.Upgrade(w, r, nil)
	if e != nil {
		return nil
	}
	defer conn.Close()
	conn.SetReadLimit(1024)
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, e := conn.ReadMessage(); e != nil {
				return
			}
		}
	}()
	p := who(r)
	pub := s.redis.Subscribe(r.Context(), "linklan:events")
	defer pub.Close()
	ch := pub.Channel()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	known := map[string]bool{}
	rows, e := s.db.Query(r.Context(), `SELECT DISTINCT n.id::text FROM networks n LEFT JOIN network_members m ON m.network_id=n.id LEFT JOIN devices d ON d.id=m.device_id WHERE n.owner_id=$1 OR d.user_id=$1`, p.User.ID)
	if e == nil {
		for rows.Next() {
			var id string
			rows.Scan(&id)
			known[id] = true
		}
		rows.Close()
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn.WriteJSON(map[string]string{"type": "ready"})
	for {
		select {
		case <-done:
			return nil
		case <-r.Context().Done():
			return nil
		case <-tick.C:
			var valid bool
			s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM session_families f JOIN users u ON u.id=f.user_id WHERE f.id=$1 AND NOT f.revoked AND f.expires_at>now() AND NOT u.blocked)`, p.Session).Scan(&valid)
			if !valid {
				return nil
			}
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return nil
			}
		case msg := <-ch:
			if msg == nil {
				return nil
			}
			var event map[string]string
			if json.Unmarshal([]byte(msg.Payload), &event) != nil {
				continue
			}
			id := event["network_id"]
			if event["user_id"] == p.User.ID || known[id] || s.member(r.Context(), p.User.ID, id, false) == nil {
				known[id] = true
				delete(event, "user_id")
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if conn.WriteJSON(event) != nil {
					return nil
				}
			}
		}
	}
}
