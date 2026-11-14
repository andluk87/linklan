package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"linklan/internal/model"
	"net/http"
)

// Reuse normal validation/allocation. Admin identity selects an existing device's
// owner; no private key is generated or transferred by this endpoint.
func (s *Server) adminCreate(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Kind string
		Data json.RawMessage
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	child := r.Clone(r.Context())
	child.Body = io.NopCloser(bytes.NewReader(b.Data))
	switch b.Kind {
	case "user":
		return s.register(w, child)
	case "network":
		var data struct {
			DeviceID string `json:"device_id"`
		}
		if e := json.Unmarshal(b.Data, &data); e != nil {
			return bad("Некорректная сеть")
		}
		if e := validID(data.DeviceID); e != nil {
			return e
		}
		var uid string
		if e := s.db.QueryRow(r.Context(), `SELECT user_id::text FROM devices WHERE id=$1 AND NOT blocked`, data.DeviceID).Scan(&uid); e != nil {
			return e
		}
		child = child.WithContext(context.WithValue(child.Context(), principalKey{}, principal{User: model.User{ID: uid}}))
		return s.createNetwork(w, child)
	default:
		return bad("Неизвестный тип объекта")
	}
}
