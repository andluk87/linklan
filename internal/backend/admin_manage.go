package backend

import (
	"crypto/sha256"
	"crypto/subtle"
	"linklan/internal/auth"
	"net/http"
	"os"
	"strings"
)

func tokenEqual(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
func readAdminToken() string {
	if path := os.Getenv("ADMIN_TOKEN_FILE"); path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
}

// Each operation uses a fixed query; object kinds and column names never enter SQL from input.
func (s *Server) adminManage(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Kind, ID, Action, Name, Description, Password, Mode, Owner string
		Admin                                                      bool
		MaxMembers                                                 int
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := validID(b.ID); e != nil {
		return e
	}
	ctx := r.Context()
	var devices []string
	q := "SELECT id::text FROM devices WHERE id=$1"
	switch b.Kind {
	case "user":
		q = "SELECT id::text FROM devices WHERE user_id=$1"
		if b.Action == "delete" {
			q += " UNION SELECT m.device_id::text FROM network_members m JOIN networks n ON n.id=m.network_id WHERE n.owner_id=$1"
		}
	case "network":
		q = "SELECT device_id::text FROM network_members WHERE network_id=$1"
	case "device":
	default:
		return bad("Неизвестный объект")
	}
	rows, e := s.db.Query(ctx, q, b.ID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		devices = append(devices, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	query := ""
	args := []any{b.ID}
	switch b.Action {
	case "disconnect":
		if b.Kind == "user" {
			if _, e = s.db.Exec(ctx, "UPDATE session_families SET revoked=true WHERE user_id=$1", b.ID); e != nil {
				return e
			}
		}
	case "delete":
		switch b.Kind {
		case "user":
			query = "DELETE FROM users WHERE id=$1"
		case "network":
			query = "DELETE FROM networks WHERE id=$1"
		case "device":
			query = "DELETE FROM devices WHERE id=$1"
		}
	case "rename":
		b.Name = strings.TrimSpace(b.Name)
		if len(b.Name) < 1 || len(b.Name) > 100 {
			return bad("Имя: от 1 до 100 байт")
		}
		args = append(args, b.Name)
		switch b.Kind {
		case "user":
			query = "UPDATE users SET name=$2 WHERE id=$1"
		case "network":
			query = "UPDATE networks SET name=$2 WHERE id=$1"
		case "device":
			query = "UPDATE devices SET name=$2 WHERE id=$1"
		}
	case "password":
		if b.Kind != "user" && b.Kind != "network" {
			return bad("Пароль только для аккаунта или сети")
		}
		done, err := s.passwordSlot(ctx)
		if err != nil {
			return err
		}
		defer done()
		hash, err := auth.Password(b.Password)
		if err != nil {
			return bad(err.Error())
		}
		args = append(args, hash)
		if b.Kind == "user" {
			query = "UPDATE users SET password_hash=$2 WHERE id=$1 AND NOT device_only"
		} else {
			query = "UPDATE networks SET password_hash=$2,mode='password' WHERE id=$1"
		}
	case "mode":
		if b.Kind != "network" || (b.Mode != "approval" && b.Mode != "private" && b.Mode != "invite") {
			return bad("Используйте approval, private или invite; для пароля — смену пароля")
		}
		query = "UPDATE networks SET mode=$2 WHERE id=$1"
		args = append(args, b.Mode)
	case "owner":
		if b.Kind != "network" {
			return bad("Владелец задаётся сети")
		}
		if e := validID(b.Owner); e != nil {
			return e
		}
		query = "UPDATE networks SET owner_id=$2 WHERE id=$1 AND EXISTS(SELECT 1 FROM users WHERE id=$2 AND NOT blocked)"
		args = append(args, b.Owner)
	case "description":
		if b.Kind != "network" || len(b.Description) > 2000 {
			return bad("Описание сети: до 2000 байт")
		}
		query = "UPDATE networks SET description=$2 WHERE id=$1"
		args = append(args, b.Description)
	case "role":
		if b.Kind != "user" {
			return bad("Роль доступна только пользователю")
		}
		query = "UPDATE users SET admin=$2 WHERE id=$1"
		args = append(args, b.Admin)
	case "capacity":
		if b.Kind != "network" || b.MaxMembers < 2 || b.MaxMembers > 253 {
			return bad("Размер сети: 2–253")
		}
		query = "UPDATE networks SET max_members=$2 WHERE id=$1 AND $2 >= (SELECT count(*) FROM network_members WHERE network_id=$1)"
		args = append(args, b.MaxMembers)
	case "revoke-invites":
		if b.Kind != "network" {
			return bad("Требуется сеть")
		}
		query = "DELETE FROM invites WHERE network_id=$1"
	default:
		return bad("Неизвестная операция")
	}
	if query != "" {
		result, err := s.db.Exec(ctx, query, args...)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 && b.Action != "revoke-invites" {
			return conflict("Объект отсутствует или ограничение меньше числа участников")
		}
	}
	if b.Action == "password" && b.Kind == "user" {
		if _, e = s.db.Exec(ctx, "UPDATE session_families SET revoked=true WHERE user_id=$1", b.ID); e != nil {
			return e
		}
	}
	if b.Action == "disconnect" || b.Action == "delete" || b.Action == "password" {
		for _, id := range devices {
			s.revokeDevice(ctx, id)
		}
	}
	s.audit(ctx, "", "admin_"+b.Action+"_"+b.Kind, b.ID)
	if b.Kind == "network" {
		s.event(ctx, b.ID, "", "network_updated")
	}
	output(w, 200, map[string]bool{"ok": true})
	return nil
}

func (s *Server) adminMembers(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("network")
	if e := validID(id); e != nil {
		return e
	}
	rows, e := s.db.Query(r.Context(), `SELECT m.device_id::text,d.name,m.status,COALESCE(host(m.virtual_ip),'') FROM network_members m JOIN devices d ON d.id=m.device_id WHERE network_id=$1 ORDER BY d.name`, id)
	if e != nil {
		return e
	}
	defer rows.Close()
	list := []map[string]string{}
	for rows.Next() {
		var device, name, status, ip string
		if e = rows.Scan(&device, &name, &status, &ip); e != nil {
			return e
		}
		list = append(list, map[string]string{"id": device, "name": name, "status": status, "ip": ip, "network": id})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	output(w, 200, list)
	return nil
}
func (s *Server) adminMember(w http.ResponseWriter, r *http.Request) error {
	var b struct{ Network, Device, Action string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := validID(b.Network); e != nil {
		return e
	}
	if e := validID(b.Device); e != nil {
		return e
	}
	ctx := r.Context()
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	n, e := scanNetwork(tx.QueryRow(ctx, `SELECT `+networkColumns+` FROM networks WHERE id=$1 FOR UPDATE`, b.Network))
	if e != nil {
		return e
	}
	var status string
	if e = tx.QueryRow(ctx, `SELECT status FROM network_members WHERE network_id=$1 AND device_id=$2`, b.Network, b.Device).Scan(&status); e != nil {
		return e
	}
	switch b.Action {
	case "approve":
		if status == "approved" {
			return conflict("Участник уже одобрен")
		}
		if _, e = allocate(ctx, tx, n, b.Device); e != nil {
			return e
		}
	case "block":
		_, e = tx.Exec(ctx, `UPDATE network_members SET status='blocked',virtual_ip=NULL WHERE network_id=$1 AND device_id=$2`, b.Network, b.Device)
	case "remove":
		_, e = tx.Exec(ctx, `DELETE FROM network_members WHERE network_id=$1 AND device_id=$2`, b.Network, b.Device)
	default:
		return bad("Неизвестная операция")
	}
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	s.revokeDevice(ctx, b.Device)
	s.event(ctx, b.Network, "", "network_updated")
	s.audit(ctx, "", "admin_member_"+b.Action, b.Device)
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
