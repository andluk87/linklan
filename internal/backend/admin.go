package backend

import (
	"encoding/json"
	"linklan/internal/model"
	"net/http"
	"strconv"
)

func (s *Server) adminState(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	page := 0
	var parseErr error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, parseErr = strconv.Atoi(raw)
	}
	if parseErr != nil || page < 0 || page > 1000000 {
		return bad("Некорректная страница")
	}
	offset := page * 200
	state := map[string]any{"version": model.Version, "relay_address": s.cfg.Relay, "stun_address": s.cfg.STUN, "page": page}
	users := []model.User{}
	rows, e := s.db.Query(ctx, `SELECT id::text,email,name,admin,blocked FROM users ORDER BY created_at DESC LIMIT 200 OFFSET $1`, offset)
	if e != nil {
		return e
	}
	for rows.Next() {
		var u model.User
		if e = rows.Scan(&u.ID, &u.Email, &u.Name, &u.Admin, &u.Blocked); e != nil {
			rows.Close()
			return e
		}
		users = append(users, u)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	state["users"] = users
	networks := []model.Network{}
	rows, e = s.db.Query(ctx, `SELECT `+networkColumns+` FROM networks ORDER BY created_at DESC LIMIT 200 OFFSET $1`, offset)
	if e != nil {
		return e
	}
	for rows.Next() {
		n, e := scanNetwork(rows)
		if e != nil {
			rows.Close()
			return e
		}
		networks = append(networks, n)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	state["networks"] = networks
	devices := []map[string]any{}
	rows, e = s.db.Query(ctx, `SELECT id::text,user_id::text,name,blocked FROM devices ORDER BY last_seen DESC LIMIT 200 OFFSET $1`, offset)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id, uid, name string
		var blocked bool
		if e = rows.Scan(&id, &uid, &name, &blocked); e != nil {
			rows.Close()
			return e
		}
		online := s.redis.Exists(ctx, "endpoint:"+id).Val() > 0
		devices = append(devices, map[string]any{"id": id, "user_id": uid, "name": name, "blocked": blocked, "online": online})
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	state["devices"] = devices
	audit := []map[string]any{}
	rows, e = s.db.Query(ctx, `SELECT action,object_id,created_at::text FROM audit_log ORDER BY id DESC LIMIT 100 OFFSET $1`, page*100)
	if e != nil {
		return e
	}
	for rows.Next() {
		var action, id, at string
		if e = rows.Scan(&action, &id, &at); e != nil {
			rows.Close()
			return e
		}
		audit = append(audit, map[string]any{"action": action, "object": id, "at": at})
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	rows.Close()
	state["audit"] = audit
	online := 0
	for _, d := range devices {
		if d["online"] == true {
			online++
		}
	}
	state["online_in_page"] = online
	var userCount, networkCount, deviceCount int64
	if e = s.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM networks),(SELECT count(*) FROM devices)`).Scan(&userCount, &networkCount, &deviceCount); e != nil {
		return e
	}
	state["totals"] = map[string]int64{"users": userCount, "networks": networkCount, "devices": deviceCount}
	settings, err := s.readSettings(ctx)
	if err != nil {
		return err
	}
	state["settings"] = settings
	state["services"] = map[string]bool{"postgres": s.db.Ping(ctx) == nil, "redis": s.redis.Ping(ctx).Err() == nil}
	state["requests"] = s.requests.Load()
	state["failures"] = s.failures.Load()
	raw, _ := s.redis.Get(ctx, "relay:stats").Bytes()
	var relay map[string]any
	if json.Unmarshal(raw, &relay) == nil {
		state["relay"] = relay
	}
	output(w, 200, state)
	return nil
}
func (s *Server) adminBlock(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Kind, ID string
		Blocked  bool
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := validID(b.ID); e != nil {
		return e
	}
	table := ""
	switch b.Kind {
	case "user":
		table = "users"
		if b.ID == who(r).User.ID {
			return bad("Нельзя заблокировать собственный аккаунт")
		}
	case "network":
		table = "networks"
	case "device":
		table = "devices"
	default:
		return bad("Некорректный тип объекта")
	}
	result, e := s.db.Exec(r.Context(), `UPDATE `+table+` SET blocked=$1 WHERE id=$2`, b.Blocked, b.ID)
	if e != nil {
		return e
	}
	if result.RowsAffected() == 0 {
		return apiError{404, "Объект не найден"}
	}
	if b.Blocked {
		q := `SELECT id::text FROM devices WHERE id=$1`
		if b.Kind == "user" {
			s.db.Exec(r.Context(), `UPDATE session_families SET revoked=true WHERE user_id=$1`, b.ID)
			q = `SELECT id::text FROM devices WHERE user_id=$1`
		}
		if b.Kind == "network" {
			q = `SELECT device_id::text FROM network_members WHERE network_id=$1`
		}
		rows, e := s.db.Query(r.Context(), q, b.ID)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			rows.Scan(&id)
			s.revokeDevice(r.Context(), id)
		}
		rows.Close()
	}
	s.audit(r.Context(), who(r).User.ID, "admin_block_"+b.Kind, b.ID)
	if b.Kind == "network" {
		s.event(r.Context(), b.ID, "", "network_updated")
	}
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
