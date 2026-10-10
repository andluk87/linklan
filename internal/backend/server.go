package backend

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"linklan/internal/auth"
	"linklan/internal/model"
	"linklan/internal/protocol"
)

//go:embed schema.sql admin.html
var assets embed.FS

type Config struct {
	Database, Redis, Secret, Relay, STUN, AdminEmail, SMTP, SMTPUser, SMTPPassword, MailFrom string
	TrustProxy                                                                               bool
}
type Server struct {
	db                 *pgxpool.Pool
	redis              *redis.Client
	cfg                Config
	passwords          chan struct{}
	dummyHash          string
	requests, failures atomic.Uint64
}
type apiError struct {
	Status  int
	Message string
}

func (e apiError) Error() string { return e.Message }
func bad(s string) error         { return apiError{400, s} }
func forbidden() error           { return apiError{403, "Доступ запрещён"} }
func conflict(s string) error    { return apiError{409, s} }

type principal struct {
	User    model.User
	Session string
}
type principalKey struct{}

func who(r *http.Request) principal { p, _ := r.Context().Value(principalKey{}).(principal); return p }
func New(ctx context.Context, cfg Config) (*Server, error) {
	if len(cfg.Secret) < 32 || cfg.Relay == "" || cfg.STUN == "" {
		return nil, fmt.Errorf("JWT_SECRET (32+ bytes), RELAY_PUBLIC_ADDRESS, STUN_PUBLIC_ADDRESS are required")
	}
	db, e := pgxpool.New(ctx, cfg.Database)
	if e != nil {
		return nil, e
	}
	if e = db.Ping(ctx); e != nil {
		db.Close()
		return nil, e
	}
	ro, e := redis.ParseURL(cfg.Redis)
	if e != nil {
		db.Close()
		return nil, e
	}
	rc := redis.NewClient(ro)
	if e = rc.Ping(ctx).Err(); e != nil {
		db.Close()
		rc.Close()
		return nil, e
	}
	// PostgreSQL advisory lock serializes concurrent replica migrations.
	conn, e := db.Acquire(ctx)
	if e != nil {
		return nil, e
	}
	defer conn.Release()
	if _, e = conn.Exec(ctx, "SELECT pg_advisory_lock(19482001)"); e != nil {
		return nil, e
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(19482001)")
	schema, _ := assets.ReadFile("schema.sql")
	if _, e = conn.Exec(ctx, string(schema)); e != nil {
		return nil, e
	}
	dummy, e := auth.Password("dummy-password-" + auth.Random(32))
	if e != nil {
		return nil, e
	}
	return &Server{db: db, redis: rc, cfg: cfg, passwords: make(chan struct{}, 4), dummyHash: dummy}, nil
}
func (s *Server) Close() { s.db.Close(); s.redis.Close() }
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("Некорректный JSON")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return bad("Ожидался один JSON объект")
	}
	return nil
}
func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type endpoint func(http.ResponseWriter, *http.Request) error

func (s *Server) handle(f endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if e := f(w, r); e != nil {
			s.failures.Add(1)
			var ae apiError
			if errors.As(e, &ae) {
				output(w, ae.Status, map[string]string{"error": ae.Message})
			} else if errors.Is(e, pgx.ErrNoRows) {
				output(w, 404, map[string]string{"error": "Объект не найден"})
			} else {
				slog.Error("request_failed", "route", r.Pattern)
				output(w, 500, map[string]string{"error": "Внутренняя ошибка сервера"})
			}
		}
	}
}
func (s *Server) authenticated(f endpoint) endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, e := auth.Validate([]byte(s.cfg.Secret), token, time.Now())
		if e != nil {
			return apiError{401, "Требуется вход"}
		}
		var u model.User
		e = s.db.QueryRow(r.Context(), `SELECT u.id::text,u.email,u.name,u.admin,u.blocked FROM users u JOIN session_families f ON f.user_id=u.id WHERE u.id=$1 AND f.id=$2 AND NOT f.revoked AND f.expires_at>now()`, c.Subject, c.Session).Scan(&u.ID, &u.Email, &u.Name, &u.Admin, &u.Blocked)
		if e != nil || u.Blocked {
			return apiError{401, "Сессия завершена"}
		}
		return f(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{u, c.Session})))
	}
}
func (s *Server) admin(f endpoint) endpoint {
	return s.authenticated(func(w http.ResponseWriter, r *http.Request) error {
		if !who(r).User.Admin {
			return forbidden()
		}
		return f(w, r)
	})
}
func (s *Server) audit(ctx context.Context, uid, action, object string) {
	if _, e := s.db.Exec(ctx, `INSERT INTO audit_log(user_id,action,object_id) VALUES(NULLIF($1,'')::uuid,$2,$3)`, uid, action, object); e != nil {
		slog.Error("audit_write_failed", "action", action)
	}
}
func (s *Server) event(ctx context.Context, network, uid, kind string) {
	b, _ := json.Marshal(map[string]string{"type": kind, "network_id": network, "user_id": uid})
	s.redis.Publish(ctx, "linklan:events", b)
}
func (s *Server) passwordSlot(ctx context.Context) (func(), error) {
	select {
	case s.passwords <- struct{}{}:
		return func() { <-s.passwords }, nil
	case <-ctx.Done():
		return nil, apiError{503, "Повторите позже"}
	}
}
func validID(id string) error {
	if _, e := protocol.ParseID(id); e != nil {
		return bad("Некорректный ID")
	}
	return nil
}
func (s *Server) ownsDevice(ctx context.Context, user, id string) (model.Device, error) {
	var d model.Device
	if e := validID(id); e != nil {
		return d, e
	}
	e := s.db.QueryRow(ctx, `SELECT id::text,user_id::text,name,public_key,blocked FROM devices WHERE id=$1 AND user_id=$2`, id, user).Scan(&d.ID, &d.UserID, &d.Name, &d.PublicKey, &d.Blocked)
	if errors.Is(e, pgx.ErrNoRows) || d.Blocked {
		return d, forbidden()
	}
	return d, e
}
func (s *Server) member(ctx context.Context, user, network string, owner bool) error {
	if e := validID(network); e != nil {
		return e
	}
	var allowed bool
	if owner {
		s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM networks WHERE id=$1 AND owner_id=$2 AND NOT blocked)`, network, user).Scan(&allowed)
	} else {
		s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM networks n JOIN network_members m ON m.network_id=n.id JOIN devices d ON d.id=m.device_id WHERE n.id=$1 AND NOT n.blocked AND NOT d.blocked AND d.user_id=$2 AND m.status='approved')`, network, user).Scan(&allowed)
	}
	if !allowed {
		return forbidden()
	}
	return nil
}
func (s *Server) revokeDevice(ctx context.Context, id string) {
	s.redis.Del(ctx, "auth:"+id, "endpoint:"+id)
	s.event(ctx, "", "", "peer_offline")
}
func (s *Server) Router() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		ctx, c := context.WithTimeout(r.Context(), 2*time.Second)
		defer c()
		if s.db.Ping(ctx) != nil || s.redis.Ping(ctx).Err() != nil {
			output(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		output(w, 200, map[string]string{"status": "ok", "version": model.Version})
	})
	m.HandleFunc("GET /metrics", s.metrics)
	m.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		b, _ := assets.ReadFile("admin.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'")
		w.Write(b)
	})
	m.HandleFunc("GET /admin.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte(adminJS))
	})
	public := map[string]endpoint{"POST /auth/register": s.register, "POST /auth/login": s.login, "POST /auth/refresh": s.refresh, "POST /auth/forgot": s.forgot, "POST /auth/reset": s.reset, "POST /auth/device/challenge": s.deviceAccessChallenge, "POST /auth/device": s.deviceAccess}
	for p, f := range public {
		m.HandleFunc(p, s.handle(f))
	}
	routes := map[string]endpoint{"POST /auth/logout": s.logout, "GET /me": s.me, "GET /networks": s.listNetworks, "POST /networks": s.createNetwork, "GET /networks/{id}": s.getNetwork, "DELETE /networks/{id}": s.deleteNetwork, "POST /networks/{id}/join": s.joinNetwork, "POST /networks/{id}/leave": s.leaveNetwork, "GET /networks/{id}/members": s.members, "DELETE /networks/{id}/members/{peer}": s.removeMember, "POST /networks/{id}/members/{peer}/approve": s.approveMember, "POST /networks/{id}/members/{peer}/block": s.blockMember, "POST /invite": s.invite, "GET /invite/{token}": s.getInvite, "POST /devices/register": s.registerDevice, "GET /devices": s.devices, "POST /devices/{id}/challenge": s.challenge, "POST /devices/{id}/session": s.session, "POST /devices/{id}/heartbeat": s.heartbeat, "DELETE /devices/{id}/session": s.disconnect, "GET /ws": s.websocket}
	for p, f := range routes {
		m.HandleFunc(p, s.handle(s.authenticated(f)))
	}
	m.HandleFunc("GET /networks/state", s.handle(s.authenticated(s.networkState)))
	m.HandleFunc("GET /admin/state", s.handle(s.admin(s.adminState)))
	m.HandleFunc("POST /admin/block", s.handle(s.admin(s.adminBlock)))
	return s.limit(m)
}
func (s *Server) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if s.cfg.TrustProxy {
			if v := net.ParseIP(r.Header.Get("X-Real-IP")); v != nil {
				ip = v.String()
			}
		}
		limit := int64(240)
		window := 60
		bucket := "api"
		if r.URL.Path == "/auth/device" || r.URL.Path == "/auth/device/challenge" {
			limit = 120
			bucket = "device_access"
		} else if strings.HasPrefix(r.URL.Path, "/auth/") && r.URL.Path != "/auth/refresh" {
			limit = 12
			window = 60
			bucket = "auth"
		}
		key := fmt.Sprintf("rate:%s:%s:%d", bucket, auth.HashToken(ip), time.Now().Unix()/int64(window))
		n, e := s.redis.Eval(r.Context(), `local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('EXPIRE',KEYS[1],ARGV[1]) end;return n`, []string{key}, window).Int64()
		if e != nil {
			output(w, 503, map[string]string{"error": "Сервис временно недоступен"})
			return
		}
		if n > limit {
			w.Header().Set("Retry-After", "60")
			output(w, 429, map[string]string{"error": "Слишком много запросов"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE linklan_api_requests_total counter\nlinklan_api_requests_total %d\n# TYPE linklan_api_errors_total counter\nlinklan_api_errors_total %d\n", s.requests.Load(), s.failures.Load())
	for _, v := range []struct{ name, query string }{{"users", "SELECT count(*) FROM users"}, {"networks", "SELECT count(*) FROM networks WHERE NOT blocked"}, {"devices", "SELECT count(*) FROM devices WHERE NOT blocked"}} {
		var n int
		s.db.QueryRow(r.Context(), v.query).Scan(&n)
		fmt.Fprintf(w, "# TYPE linklan_%s gauge\nlinklan_%s %d\n", v.name, v.name, n)
	}
}
func EnvConfig() Config {
	return Config{Database: os.Getenv("DATABASE_URL"), Redis: os.Getenv("REDIS_URL"), Secret: os.Getenv("JWT_SECRET"), Relay: os.Getenv("RELAY_PUBLIC_ADDRESS"), STUN: os.Getenv("STUN_PUBLIC_ADDRESS"), AdminEmail: strings.ToLower(os.Getenv("ADMIN_EMAIL")), SMTP: os.Getenv("SMTP_ADDRESS"), SMTPUser: os.Getenv("SMTP_USER"), SMTPPassword: os.Getenv("SMTP_PASSWORD"), MailFrom: os.Getenv("SMTP_FROM"), TrustProxy: os.Getenv("TRUST_PROXY") == "true"}
}
