package backend

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"linklan/internal/auth"
	"linklan/internal/model"
)

func email(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	m, e := mail.ParseAddress(v)
	if e != nil || m.Address != v || len(v) > 254 {
		return "", bad("Некорректный email")
	}
	return v, nil
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Email, Name, Password string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	em, e := email(b.Email)
	if e != nil {
		return e
	}
	b.Name = strings.TrimSpace(b.Name)
	if len(b.Name) < 1 || len(b.Name) > 80 {
		return bad("Имя должно содержать 1–80 байт")
	}
	done, e := s.passwordSlot(r.Context())
	if e != nil {
		return e
	}
	defer done()
	hash, e := auth.Password(b.Password)
	if e != nil {
		return bad(e.Error())
	}
	u := model.User{ID: auth.UUID(), Email: em, Name: b.Name}
	_, e = s.db.Exec(r.Context(), `INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,$3,$4)`, u.ID, u.Email, u.Name, hash)
	if e != nil {
		var exists bool
		s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, em).Scan(&exists)
		if exists {
			return conflict("Не удалось зарегистрировать аккаунт с этим email")
		}
		return e
	}
	s.audit(r.Context(), u.ID, "auth_register", u.ID)
	return s.issue(w, r, u)
}
func (s *Server) issue(w http.ResponseWriter, r *http.Request, u model.User) error {
	return s.issueDevice(w, r, u, nil)
}
func (s *Server) issueDevice(w http.ResponseWriter, r *http.Request, u model.User, device *model.Device) error {
	family, refresh := auth.UUID(), auth.Random(32)
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), `INSERT INTO session_families(id,user_id,expires_at) VALUES($1,$2,now()+interval '30 days')`, family, u.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO refresh_tokens(hash,family_id,expires_at) VALUES($1,$2,now()+interval '30 days')`, auth.HashToken(refresh), family); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	tokens := model.Tokens{Access: auth.Access([]byte(s.cfg.Secret), u.ID, family, time.Now()), Refresh: refresh, User: u}
	if device != nil {
		output(w, 200, model.DeviceAccess{Tokens: tokens, Device: *device})
	} else {
		output(w, 200, tokens)
	}
	return nil
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Email, Password string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	em, e := email(b.Email)
	if e != nil {
		return apiError{401, "Неверный email или пароль"}
	}
	done, e := s.passwordSlot(r.Context())
	if e != nil {
		return e
	}
	defer done()
	var u model.User
	var hash string
	e = s.db.QueryRow(r.Context(), `SELECT id::text,email,name,admin,blocked,password_hash FROM users WHERE email=$1`, em).Scan(&u.ID, &u.Email, &u.Name, &u.Admin, &u.Blocked, &hash)
	if e != nil {
		hash = s.dummyHash
	}
	good := auth.VerifyPassword(hash, b.Password)
	if e != nil || u.Blocked || !good {
		s.audit(r.Context(), "", "auth_login_failed", "")
		return apiError{401, "Неверный email или пароль"}
	}
	s.audit(r.Context(), u.ID, "auth_login", u.ID)
	return s.issue(w, r, u)
}
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Refresh string `json:"refresh_token"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(b.Refresh) < 32 || len(b.Refresh) > 256 {
		return apiError{401, "Сессия завершена"}
	}
	ctx := r.Context()
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var family string
	var used, revoked, valid bool
	var u model.User
	e = tx.QueryRow(ctx, `SELECT f.id::text,t.used,f.revoked,(t.expires_at>now() AND f.expires_at>now()),u.id::text,u.email,u.name,u.admin,u.blocked FROM refresh_tokens t JOIN session_families f ON f.id=t.family_id JOIN users u ON u.id=f.user_id WHERE t.hash=$1 FOR UPDATE OF f,t`, auth.HashToken(b.Refresh)).Scan(&family, &used, &revoked, &valid, &u.ID, &u.Email, &u.Name, &u.Admin, &u.Blocked)
	if errors.Is(e, pgx.ErrNoRows) {
		return apiError{401, "Сессия завершена"}
	}
	if e != nil {
		return e
	}
	if used {
		if _, e = tx.Exec(ctx, `UPDATE session_families SET revoked=true WHERE id=$1`, family); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		s.revokeFamily(r, family, u.ID)
		s.audit(ctx, u.ID, "refresh_replay", family)
		return apiError{401, "Повторное использование токена: сессия отозвана"}
	}
	if revoked || !valid || u.Blocked {
		return apiError{401, "Сессия завершена"}
	}
	refresh := auth.Random(32)
	if _, e = tx.Exec(ctx, `UPDATE refresh_tokens SET used=true WHERE hash=$1`, auth.HashToken(b.Refresh)); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO refresh_tokens(hash,family_id,expires_at) SELECT $1,id,expires_at FROM session_families WHERE id=$2`, auth.HashToken(refresh), family); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	output(w, 200, model.Tokens{Access: auth.Access([]byte(s.cfg.Secret), u.ID, family, time.Now()), Refresh: refresh, User: u})
	return nil
}
func (s *Server) revokeFamily(r *http.Request, family, uid string) {
	rows, e := s.db.Query(r.Context(), `SELECT id::text FROM devices WHERE user_id=$1`, uid)
	if e != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		rows.Scan(&id)
		var a model.RelayAuth
		raw, _ := s.redis.Get(r.Context(), "auth:"+id).Bytes()
		if json.Unmarshal(raw, &a) == nil && a.AuthSession == family {
			s.revokeDevice(r.Context(), id)
		}
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	p := who(r)
	if _, e := s.db.Exec(r.Context(), `UPDATE session_families SET revoked=true WHERE id=$1`, p.Session); e != nil {
		return e
	}
	s.revokeFamily(r, p.Session, p.User.ID)
	s.audit(r.Context(), p.User.ID, "auth_logout", p.Session)
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	output(w, 200, who(r).User)
	return nil
}
func (s *Server) forgot(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Email string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	em, e := email(b.Email)
	if e != nil {
		return bad("Некорректный email")
	}
	// SMTP is required. Tokens are never returned by the API or written to logs.
	if s.cfg.SMTP == "" || s.cfg.MailFrom == "" {
		return apiError{503, "Восстановление пароля пока не настроено администратором"}
	}
	var uid string
	e = s.db.QueryRow(r.Context(), `SELECT id::text FROM users WHERE email=$1 AND NOT blocked`, em).Scan(&uid)
	if e == nil {
		token := auth.Random(32)
		if _, e = s.db.Exec(r.Context(), `INSERT INTO password_resets(hash,user_id,expires_at) VALUES($1,$2,now()+interval '15 minutes')`, auth.HashToken(token), uid); e != nil {
			return e
		}
		if e = s.sendReset(em, token); e != nil {
			slogSafeMailFailure()
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	output(w, 200, map[string]string{"message": "Если аккаунт существует, код восстановления отправлен на email"})
	return nil
}
func slogSafeMailFailure() { fmt.Println("password_reset_delivery_failed") }
func (s *Server) sendReset(to, token string) error {
	host, _, e := net.SplitHostPort(s.cfg.SMTP)
	if e != nil {
		return e
	}
	if strings.ContainsAny(s.cfg.MailFrom, "\r\n") {
		return fmt.Errorf("invalid mail sender")
	}
	conn, e := net.DialTimeout("tcp", s.cfg.SMTP, 10*time.Second)
	if e != nil {
		return e
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	c, e := smtp.NewClient(conn, host)
	if e != nil {
		return e
	}
	defer c.Close()
	if e = c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); e != nil {
		return e
	}
	if s.cfg.SMTPUser != "" {
		if e = c.Auth(smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPassword, host)); e != nil {
			return e
		}
	}
	if e = c.Mail(s.cfg.MailFrom); e != nil {
		return e
	}
	if e = c.Rcpt(to); e != nil {
		return e
	}
	w, e := c.Data()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: LinkLAN password reset\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour one-time recovery code (valid 15 minutes):\r\n%s\r\n\r\nEnter it in LinkLAN Settings > Reset password.\r\n", s.cfg.MailFrom, to, token)
	if e != nil {
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	return c.Quit()
}
func (s *Server) reset(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Token, Password string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if len(b.Token) > 256 {
		return bad("Некорректный код")
	}
	done, e := s.passwordSlot(r.Context())
	if e != nil {
		return e
	}
	defer done()
	hash, e := auth.Password(b.Password)
	if e != nil {
		return bad(e.Error())
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var uid string
	e = tx.QueryRow(r.Context(), `UPDATE password_resets SET used=true WHERE hash=$1 AND NOT used AND expires_at>now() RETURNING user_id::text`, auth.HashToken(b.Token)).Scan(&uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return bad("Код недействителен или истёк")
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE users SET password_hash=$1 WHERE id=$2`, hash, uid); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE session_families SET revoked=true WHERE user_id=$1`, uid); e != nil {
		return e
	}
	if _, e = tx.Exec(r.Context(), `UPDATE password_resets SET used=true WHERE user_id=$1`, uid); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	rows, e := s.db.Query(r.Context(), `SELECT id::text FROM devices WHERE user_id=$1`, uid)
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			rows.Scan(&id)
			s.revokeDevice(r.Context(), id)
		}
	}
	s.audit(r.Context(), uid, "password_reset", uid)
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
