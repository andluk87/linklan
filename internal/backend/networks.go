package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"linklan/internal/auth"
	"linklan/internal/model"
)

func scanNetwork(row pgx.Row) (model.Network, error) {
	var n model.Network
	e := row.Scan(&n.ID, &n.Name, &n.Description, &n.OwnerID, &n.Subnet, &n.Mode, &n.MaxMembers, &n.Blocked)
	return n, e
}

const networkColumns = `id::text,name,description,owner_id::text,subnet::text,mode,max_members,blocked`

func (s *Server) listNetworks(w http.ResponseWriter, r *http.Request) error {
	out, e := s.readNetworks(r)
	if e != nil {
		return e
	}
	output(w, 200, out)
	return nil
}
func (s *Server) readNetworks(r *http.Request) ([]model.Network, error) {
	device := r.URL.Query().Get("device_id")
	if device != "" {
		if _, err := s.ownsDevice(r.Context(), who(r).User.ID, device); err != nil {
			return nil, err
		}
	}
	rows, e := s.db.Query(r.Context(), `SELECT n.id::text,n.name,n.description,n.owner_id::text,n.subnet::text,n.mode,n.max_members,n.blocked,m.status,COALESCE(host(m.virtual_ip),''),(SELECT count(*) FROM network_members WHERE network_id=n.id AND status='approved') FROM networks n JOIN network_members m ON m.network_id=n.id JOIN devices d ON d.id=m.device_id WHERE d.user_id=$1 AND ($2='' OR d.id::text=$2) ORDER BY n.created_at`, who(r).User.ID, device)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Network{}
	seen := map[string]bool{}
	for rows.Next() {
		var n model.Network
		if e = rows.Scan(&n.ID, &n.Name, &n.Description, &n.OwnerID, &n.Subnet, &n.Mode, &n.MaxMembers, &n.Blocked, &n.Status, &n.IP, &n.Members); e != nil {
			return nil, e
		}
		if !seen[n.ID] {
			out = append(out, n)
			seen[n.ID] = true
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Server) networkState(w http.ResponseWriter, r *http.Request) error {
	if r.URL.Query().Get("device_id") == "" {
		return bad("Укажите устройство")
	}
	networks, e := s.readNetworks(r)
	if e != nil {
		return e
	}
	if len(networks) > 128 {
		return conflict("Слишком много сетей для одного устройства")
	}
	out := make([]model.NetworkView, 0, len(networks))
	for _, n := range networks {
		v := model.NetworkView{Network: n, Peers: []model.Peer{}}
		if !n.Blocked && n.Status == "approved" {
			peers, err := s.peerList(r.Context(), n.ID)
			if err != nil {
				return err
			}
			for _, p := range peers {
				if n.OwnerID == who(r).User.ID || p.Status == "approved" || p.UserID == who(r).User.ID {
					v.Peers = append(v.Peers, p)
				}
			}
		}
		out = append(out, v)
	}
	output(w, 200, out)
	return nil
}
func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Name, Description, Mode, Password string
		DeviceID                          string `json:"device_id"`
		MaxMembers                        int    `json:"max_members"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	b.Name = strings.TrimSpace(b.Name)
	if len(b.Name) < 2 || len(b.Name) > 80 || len(b.Description) > 1024 {
		return bad("Название 2–80 байт, описание до 1024 байт")
	}
	if b.Mode == "" {
		b.Mode = "invite"
	}
	if b.Mode != "private" && b.Mode != "password" && b.Mode != "invite" && b.Mode != "approval" {
		return bad("Некорректный режим доступа")
	}
	if b.MaxMembers == 0 {
		b.MaxMembers = 32
	}
	if b.MaxMembers < 2 || b.MaxMembers > 253 {
		return bad("Допустимо 2–253 устройства")
	}
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, b.DeviceID); e != nil {
		return e
	}
	hash := ""
	if b.Mode == "password" {
		done, e := s.passwordSlot(r.Context())
		if e != nil {
			return e
		}
		hash, e = auth.Password(b.Password)
		done()
		if e != nil {
			return bad("Пароль сети: 12–256 байт")
		}
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var index int
	// Per-user quota serializes creation on the user row.
	var uid string
	if e = tx.QueryRow(r.Context(), `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, who(r).User.ID).Scan(&uid); e != nil {
		return e
	}
	var count int
	tx.QueryRow(r.Context(), `SELECT count(*) FROM networks WHERE owner_id=$1`, uid).Scan(&count)
	if count >= 32 {
		return conflict("Лимит: 32 сети на аккаунт")
	}
	if e = tx.QueryRow(r.Context(), `SELECT nextval('subnet_sequence')`).Scan(&index); e != nil {
		return e
	}
	n := model.Network{ID: auth.UUID(), Name: b.Name, Description: b.Description, OwnerID: uid, Subnet: fmt.Sprintf("10.%d.%d.0/24", 32+index/256, index%256), Mode: b.Mode, MaxMembers: b.MaxMembers, Status: "approved", IP: fmt.Sprintf("10.%d.%d.2", 32+index/256, index%256)}
	if _, e = tx.Exec(r.Context(), `INSERT INTO networks(id,owner_id,name,description,subnet,mode,password_hash,max_members) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, n.ID, n.OwnerID, n.Name, n.Description, n.Subnet, n.Mode, hash, n.MaxMembers); e != nil {
		return conflict("Название сети занято или сеть не удалось создать")
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO network_members(network_id,device_id,virtual_ip,status) VALUES($1,$2,$3,'approved')`, n.ID, b.DeviceID, n.IP); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	s.audit(r.Context(), uid, "network_created", n.ID)
	s.event(r.Context(), n.ID, "", "network_updated")
	output(w, 201, n)
	return nil
}
func (s *Server) getNetwork(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if e := s.member(r.Context(), who(r).User.ID, id, false); e != nil {
		return e
	}
	n, e := scanNetwork(s.db.QueryRow(r.Context(), `SELECT `+networkColumns+` FROM networks WHERE id=$1`, id))
	if e != nil {
		return e
	}
	output(w, 200, n)
	return nil
}
func allocate(ctx context.Context, tx pgx.Tx, n model.Network, device string) (string, error) {
	rows, e := tx.Query(ctx, `SELECT host(virtual_ip) FROM network_members WHERE network_id=$1 AND status='approved'`, n.ID)
	if e != nil {
		return "", e
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		if e = rows.Scan(&ip); e != nil {
			rows.Close()
			return "", e
		}
		used[ip] = true
	}
	rows.Close()
	if len(used) >= n.MaxMembers {
		return "", conflict("Сеть заполнена")
	}
	_, sub, e := net.ParseCIDR(n.Subnet)
	if e != nil {
		return "", e
	}
	base := sub.IP.To4()
	for host := 2; host <= 254; host++ {
		ip := fmt.Sprintf("%d.%d.%d.%d", base[0], base[1], base[2], host)
		if !used[ip] {
			_, e = tx.Exec(ctx, `INSERT INTO network_members(network_id,device_id,virtual_ip,status) VALUES($1,$2,$3,'approved') ON CONFLICT(network_id,device_id) DO UPDATE SET virtual_ip=excluded.virtual_ip,status='approved'`, n.ID, device, ip)
			return ip, e
		}
	}
	return "", conflict("Нет свободных IPv4")
}
func (s *Server) joinNetwork(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		DeviceID        string `json:"device_id"`
		Password, Token string
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, b.DeviceID); e != nil {
		return e
	}
	id := r.PathValue("id")
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	// ID or globally unique name can be used; parameter remains text until resolved.
	var n model.Network
	var hash string
	e = tx.QueryRow(r.Context(), `SELECT `+networkColumns+`,password_hash FROM networks WHERE id::text=$1 OR lower(name)=lower($1) FOR UPDATE`, id).Scan(&n.ID, &n.Name, &n.Description, &n.OwnerID, &n.Subnet, &n.Mode, &n.MaxMembers, &n.Blocked, &hash)
	if e != nil {
		return e
	}
	if n.Blocked {
		return forbidden()
	}
	var status, ip string
	_ = tx.QueryRow(r.Context(), `SELECT status,COALESCE(host(virtual_ip),'') FROM network_members WHERE network_id=$1 AND device_id=$2`, n.ID, b.DeviceID).Scan(&status, &ip)
	if status == "blocked" {
		return forbidden()
	}
	if status == "approved" {
		n.IP = ip
		n.Status = status
		output(w, 200, n)
		return nil
	}
	owner := n.OwnerID == who(r).User.ID
	validInvite := false
	if b.Token != "" && len(b.Token) <= 256 {
		var left int
		e = tx.QueryRow(r.Context(), `SELECT remaining FROM invites WHERE hash=$1 AND network_id=$2 AND expires_at>now() AND remaining>0 FOR UPDATE`, auth.HashToken(b.Token), n.ID).Scan(&left)
		validInvite = e == nil
	}
	if !owner && !validInvite {
		switch n.Mode {
		case "private", "invite":
			return forbidden()
		case "password":
			done, e := s.passwordSlot(r.Context())
			if e != nil {
				return e
			}
			ok := auth.VerifyPassword(hash, b.Password)
			done()
			if !ok {
				return forbidden()
			}
		case "approval":
			var count int
			tx.QueryRow(r.Context(), `SELECT count(*) FROM network_members WHERE network_id=$1`, n.ID).Scan(&count)
			if count >= n.MaxMembers*2 {
				return conflict("Слишком много заявок")
			}
			_, e = tx.Exec(r.Context(), `INSERT INTO network_members(network_id,device_id,status) VALUES($1,$2,'pending') ON CONFLICT DO NOTHING`, n.ID, b.DeviceID)
			if e != nil {
				return e
			}
			if e = tx.Commit(r.Context()); e != nil {
				return e
			}
			n.Status = "pending"
			s.event(r.Context(), n.ID, n.OwnerID, "peer_joined")
			output(w, 202, n)
			return nil
		}
	}
	ip, e = allocate(r.Context(), tx, n, b.DeviceID)
	if e != nil {
		return e
	}
	if validInvite {
		if _, e = tx.Exec(r.Context(), `UPDATE invites SET remaining=remaining-1 WHERE hash=$1`, auth.HashToken(b.Token)); e != nil {
			return e
		}
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	n.IP = ip
	n.Status = "approved"
	s.audit(r.Context(), who(r).User.ID, "network_join", n.ID)
	s.event(r.Context(), n.ID, "", "peer_joined")
	output(w, 200, n)
	return nil
}
func (s *Server) leaveNetwork(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		DeviceID string `json:"device_id"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, b.DeviceID); e != nil {
		return e
	}
	id := r.PathValue("id")
	if e := validID(id); e != nil {
		return e
	}
	_, e := s.db.Exec(r.Context(), `DELETE FROM network_members WHERE network_id=$1 AND device_id=$2`, id, b.DeviceID)
	if e != nil {
		return e
	}
	s.revokeDevice(r.Context(), b.DeviceID)
	s.audit(r.Context(), who(r).User.ID, "network_leave", id)
	s.event(r.Context(), id, "", "peer_removed")
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) deleteNetwork(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if e := s.member(r.Context(), who(r).User.ID, id, true); e != nil {
		return e
	}
	rows, e := s.db.Query(r.Context(), `SELECT device_id::text FROM network_members WHERE network_id=$1`, id)
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	if _, e = s.db.Exec(r.Context(), `DELETE FROM networks WHERE id=$1`, id); e != nil {
		return e
	}
	for _, d := range ids {
		s.revokeDevice(r.Context(), d)
	}
	s.audit(r.Context(), who(r).User.ID, "network_deleted", id)
	s.event(r.Context(), id, "", "network_updated")
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) error {
	return s.modifyMember(w, r, false)
}
func (s *Server) blockMember(w http.ResponseWriter, r *http.Request) error {
	return s.modifyMember(w, r, true)
}
func (s *Server) modifyMember(w http.ResponseWriter, r *http.Request, block bool) error {
	id, peer := r.PathValue("id"), r.PathValue("peer")
	if e := s.member(r.Context(), who(r).User.ID, id, true); e != nil {
		return e
	}
	if e := validID(peer); e != nil {
		return e
	}
	var e error
	if block {
		_, e = s.db.Exec(r.Context(), `UPDATE network_members SET status='blocked',virtual_ip=NULL WHERE network_id=$1 AND device_id=$2`, id, peer)
	} else {
		_, e = s.db.Exec(r.Context(), `DELETE FROM network_members WHERE network_id=$1 AND device_id=$2`, id, peer)
	}
	if e != nil {
		return e
	}
	s.revokeDevice(r.Context(), peer)
	s.audit(r.Context(), who(r).User.ID, "member_removed", peer)
	s.event(r.Context(), id, "", "peer_removed")
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) approveMember(w http.ResponseWriter, r *http.Request) error {
	id, peer := r.PathValue("id"), r.PathValue("peer")
	if e := s.member(r.Context(), who(r).User.ID, id, true); e != nil {
		return e
	}
	if e := validID(peer); e != nil {
		return e
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	n, e := scanNetwork(tx.QueryRow(r.Context(), `SELECT `+networkColumns+` FROM networks WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return e
	}
	var status string
	if e = tx.QueryRow(r.Context(), `SELECT status FROM network_members WHERE network_id=$1 AND device_id=$2`, id, peer).Scan(&status); e != nil {
		return e
	}
	if status != "pending" {
		return conflict("Можно подтвердить только ожидающую заявку")
	}
	ip, e := allocate(r.Context(), tx, n, peer)
	if e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	s.event(r.Context(), id, "", "peer_joined")
	output(w, 200, map[string]string{"virtual_ip": ip})
	return nil
}
func (s *Server) invite(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		NetworkID string `json:"network_id"`
		Uses      int
		Hours     int
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	if e := s.member(r.Context(), who(r).User.ID, b.NetworkID, true); e != nil {
		return e
	}
	if b.Uses == 0 {
		b.Uses = 10
	}
	if b.Hours == 0 {
		b.Hours = 24
	}
	if b.Uses < 1 || b.Uses > 253 || b.Hours < 1 || b.Hours > 168 {
		return bad("Uses 1–253; Hours 1–168")
	}
	token := auth.Random(32)
	expires := time.Now().Add(time.Duration(b.Hours) * time.Hour)
	if _, e := s.db.Exec(r.Context(), `INSERT INTO invites(hash,network_id,remaining,expires_at) VALUES($1,$2,$3,$4)`, auth.HashToken(token), b.NetworkID, b.Uses, expires); e != nil {
		return e
	}
	s.audit(r.Context(), who(r).User.ID, "invite_created", b.NetworkID)
	output(w, 201, map[string]any{"token": token, "network_id": b.NetworkID, "expires_at": expires})
	return nil
}
func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) error {
	token := r.PathValue("token")
	if len(token) > 256 {
		return bad("Некорректный код")
	}
	var id, name string
	e := s.db.QueryRow(r.Context(), `SELECT n.id::text,n.name FROM invites i JOIN networks n ON n.id=i.network_id WHERE i.hash=$1 AND i.remaining>0 AND i.expires_at>now() AND NOT n.blocked`, auth.HashToken(token)).Scan(&id, &name)
	if e != nil {
		return e
	}
	output(w, 200, map[string]string{"network_id": id, "name": name})
	return nil
}
