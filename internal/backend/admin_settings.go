package backend

import (
	"context"
	"net/http"
	"strings"
)

type serverSettings struct {
	RegistrationOpen bool `json:"registration_open"`
	Maintenance      bool `json:"maintenance"`
}

func (s *Server) readSettings(ctx context.Context) (serverSettings, error) {
	var b serverSettings
	e := s.db.QueryRow(ctx, `SELECT registration_open,maintenance FROM server_settings WHERE id=true`).Scan(&b.RegistrationOpen, &b.Maintenance)
	return b, e
}
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) error {
	var b serverSettings
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if _, e := s.db.Exec(r.Context(), `UPDATE server_settings SET registration_open=$1,maintenance=$2 WHERE id=true`, b.RegistrationOpen, b.Maintenance); e != nil {
		return e
	}
	s.audit(r.Context(), "", "admin_settings_updated", "")
	output(w, 200, b)
	return nil
}
func (s *Server) admission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/auth/register" || path == "/auth/device" || (r.Method == "POST" && (path == "/networks" || strings.HasSuffix(path, "/join") || strings.HasSuffix(path, "/session"))) {
			b, e := s.readSettings(r.Context())
			if e != nil {
				output(w, 503, map[string]string{"error": "Настройки недоступны"})
				return
			}
			if b.Maintenance || (path == "/auth/register" && !b.RegistrationOpen) {
				output(w, 503, map[string]string{"error": "Регистрация или новые подключения временно отключены администратором"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
