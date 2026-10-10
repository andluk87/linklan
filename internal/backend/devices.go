package backend

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"linklan/internal/auth"
	"linklan/internal/model"
)

func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Name      string
		PublicKey string `json:"public_key"`
		Signature string
		Version   string
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	b.Name = strings.TrimSpace(b.Name)
	pub, e := base64.RawStdEncoding.DecodeString(b.PublicKey)
	sig, se := base64.RawStdEncoding.DecodeString(b.Signature)
	if e != nil || se != nil || len(pub) != 32 || len(b.Name) < 1 || len(b.Name) > 64 || !ed25519.Verify(pub, []byte("LinkLAN registration|"+b.Name+"|"+b.PublicKey), sig) {
		return bad("Некорректное имя или подпись устройства")
	}
	if len(b.Version) > 32 {
		return bad("Некорректная версия")
	}
	d := model.Device{ID: auth.UUID(), UserID: who(r).User.ID, Name: b.Name, PublicKey: b.PublicKey}
	var count int
	s.db.QueryRow(r.Context(), `SELECT count(*) FROM devices WHERE user_id=$1`, d.UserID).Scan(&count)
	if count >= 32 {
		return conflict("Лимит: 32 устройства на аккаунт")
	}
	e = s.db.QueryRow(r.Context(), `INSERT INTO devices(id,user_id,name,public_key,version) VALUES($1,$2,$3,$4,$5) ON CONFLICT(public_key) DO UPDATE SET name=excluded.name,version=excluded.version WHERE devices.user_id=excluded.user_id AND NOT devices.blocked RETURNING id::text,user_id::text,name,public_key,blocked`, d.ID, d.UserID, d.Name, d.PublicKey, b.Version).Scan(&d.ID, &d.UserID, &d.Name, &d.PublicKey, &d.Blocked)
	if e != nil {
		return forbidden()
	}
	s.audit(r.Context(), d.UserID, "device_registered", d.ID)
	output(w, 200, d)
	return nil
}
func (s *Server) devices(w http.ResponseWriter, r *http.Request) error {
	rows, e := s.db.Query(r.Context(), `SELECT id::text,user_id::text,name,public_key,blocked FROM devices WHERE user_id=$1 ORDER BY created_at`, who(r).User.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []model.Device{}
	for rows.Next() {
		var d model.Device
		if e = rows.Scan(&d.ID, &d.UserID, &d.Name, &d.PublicKey, &d.Blocked); e != nil {
			return e
		}
		out = append(out, d)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	output(w, 200, out)
	return nil
}
func (s *Server) challenge(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, id); e != nil {
		return e
	}
	c := auth.Random(32)
	if e := s.redis.Set(r.Context(), "challenge:"+id+":"+auth.HashToken(c), who(r).Session, 30*time.Second).Err(); e != nil {
		return e
	}
	output(w, 200, map[string]string{"challenge": c})
	return nil
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	d, e := s.ownsDevice(r.Context(), who(r).User.ID, id)
	if e != nil {
		return e
	}
	var b struct {
		NetworkID            string `json:"network_id"`
		Challenge, Signature string
		EphemeralKey         string `json:"ephemeral_key"`
		LocalEndpoint        string `json:"local_endpoint"`
	}
	if e = decode(w, r, &b); e != nil {
		return e
	}
	if e = validID(b.NetworkID); e != nil {
		return e
	}
	if len(b.Challenge) > 128 || len(b.LocalEndpoint) > 128 {
		return bad("Некорректные данные сеанса")
	}
	key, e := base64.RawStdEncoding.DecodeString(b.EphemeralKey)
	if e != nil || len(key) != 32 {
		return bad("Некорректный X25519 ключ")
	}
	pub, _ := base64.RawStdEncoding.DecodeString(d.PublicKey)
	sig, e := base64.RawStdEncoding.DecodeString(b.Signature)
	proof := model.SessionProof(b.Challenge, id, b.NetworkID, b.EphemeralKey, b.LocalEndpoint)
	if e != nil || !ed25519.Verify(pub, []byte(proof), sig) {
		return forbidden()
	}
	challenge, e := s.redis.GetDel(r.Context(), "challenge:"+id+":"+auth.HashToken(b.Challenge)).Result()
	if e != nil || challenge != who(r).Session {
		return forbidden()
	}
	if b.LocalEndpoint != "" {
		a, e := net.ResolveUDPAddr("udp4", b.LocalEndpoint)
		if e != nil || a.IP == nil || !a.IP.IsPrivate() || a.Port < 1024 {
			return bad("Локальный endpoint должен быть частным IPv4 адресом")
		}
	}
	var n model.Network
	e = s.db.QueryRow(r.Context(), `SELECT n.id::text,n.name,n.description,n.owner_id::text,n.subnet::text,n.mode,n.max_members,n.blocked,host(m.virtual_ip),m.status FROM networks n JOIN network_members m ON m.network_id=n.id WHERE n.id=$1 AND m.device_id=$2 AND m.status='approved' AND NOT n.blocked`, b.NetworkID, id).Scan(&n.ID, &n.Name, &n.Description, &n.OwnerID, &n.Subnet, &n.Mode, &n.MaxMembers, &n.Blocked, &n.IP, &n.Status)
	if e != nil {
		return forbidden()
	}
	a := model.RelayAuth{Key: auth.Random(32), NetworkID: n.ID, EphemeralKey: b.EphemeralKey, Signature: b.Signature, Challenge: b.Challenge, LocalEndpoint: b.LocalEndpoint, UserID: d.UserID, AuthSession: who(r).Session, Generation: auth.Random(16)}
	raw, _ := json.Marshal(a)
	if e = s.redis.Set(r.Context(), "auth:"+id, raw, 180*time.Second).Err(); e != nil {
		return e
	}
	s.redis.Del(r.Context(), "endpoint:"+id)
	s.db.Exec(r.Context(), `UPDATE devices SET last_seen=now() WHERE id=$1`, id)
	peers, e := s.peerList(r.Context(), n.ID)
	if e != nil {
		return e
	}
	s.event(r.Context(), n.ID, "", "peer_online")
	output(w, 200, model.TunnelSession{RelayAddress: s.cfg.Relay, STUNAddress: s.cfg.STUN, RelayKey: a.Key, Generation: a.Generation, Network: n, Peers: peers})
	return nil
}
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, id); e != nil {
		return e
	}
	var b struct{ Generation string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	raw, e := s.redis.Get(r.Context(), "auth:"+id).Bytes()
	if e != nil {
		return apiError{409, "Сеанс VPN завершён"}
	}
	var a model.RelayAuth
	if json.Unmarshal(raw, &a) != nil || a.Generation != b.Generation || a.AuthSession != who(r).Session {
		return apiError{409, "Сеанс VPN заменён"}
	}
	var ok bool
	s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM network_members m JOIN networks n ON n.id=m.network_id WHERE m.network_id=$1 AND m.device_id=$2 AND m.status='approved' AND NOT n.blocked)`, a.NetworkID, id).Scan(&ok)
	if !ok {
		s.revokeDevice(r.Context(), id)
		return forbidden()
	}
	result, e := s.redis.Eval(r.Context(), `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('EXPIRE',KEYS[1],180) end;return 0`, []string{"auth:" + id}, string(raw)).Int()
	if e != nil {
		return e
	}
	if result != 1 {
		return apiError{409, "Сеанс VPN заменён"}
	}
	s.db.Exec(r.Context(), `UPDATE devices SET last_seen=now() WHERE id=$1`, id)
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) disconnect(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, e := s.ownsDevice(r.Context(), who(r).User.ID, id); e != nil {
		return e
	}
	var b struct{ Generation string }
	if e := decode(w, r, &b); e != nil {
		return e
	}
	raw, _ := s.redis.Get(r.Context(), "auth:"+id).Bytes()
	var a model.RelayAuth
	if json.Unmarshal(raw, &a) == nil && a.Generation == b.Generation {
		s.redis.Eval(r.Context(), `if redis.call('GET',KEYS[1])==ARGV[1] then redis.call('DEL',KEYS[1],KEYS[2]);return 1 end;return 0`, []string{"auth:" + id, "endpoint:" + id}, string(raw))
		s.event(r.Context(), a.NetworkID, "", "peer_offline")
	}
	output(w, 200, map[string]bool{"ok": true})
	return nil
}
func (s *Server) peerList(ctx context.Context, network string) ([]model.Peer, error) {
	rows, e := s.db.Query(ctx, `SELECT d.id::text,d.user_id::text,d.name,u.name,COALESCE(host(m.virtual_ip),''),d.public_key,m.status,d.last_seen,d.version FROM network_members m JOIN devices d ON d.id=m.device_id JOIN users u ON u.id=d.user_id WHERE m.network_id=$1 AND m.status<>'blocked' AND NOT d.blocked AND NOT u.blocked ORDER BY m.joined_at`, network)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Peer{}
	for rows.Next() {
		var p model.Peer
		if e = rows.Scan(&p.ID, &p.UserID, &p.Name, &p.UserName, &p.IP, &p.PublicKey, &p.Status, &p.LastSeen, &p.Version); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	keys := make([]string, 0, len(out)*2)
	for _, p := range out {
		keys = append(keys, "auth:"+p.ID, "endpoint:"+p.ID)
	}
	if len(keys) == 0 {
		return out, nil
	}
	data, e := s.redis.MGet(ctx, keys...).Result()
	if e != nil {
		return nil, e
	}
	for i := range out {
		var a model.RelayAuth
		var endpoint struct{ Address, Generation string }
		raw, ok := data[i*2].(string)
		if !ok || json.Unmarshal([]byte(raw), &a) != nil || a.NetworkID != network {
			continue
		}
		out[i].Online = true
		out[i].EphemeralKey = a.EphemeralKey
		out[i].SessionSignature = a.Signature
		out[i].SessionChallenge = a.Challenge
		out[i].LocalEndpoint = a.LocalEndpoint
		if ep, ok := data[i*2+1].(string); ok && json.Unmarshal([]byte(ep), &endpoint) == nil && endpoint.Generation == a.Generation {
			out[i].Endpoint = endpoint.Address
			out[i].Online = true
		}
	}
	return out, nil
}
func (s *Server) members(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if e := s.member(r.Context(), who(r).User.ID, id, false); e != nil {
		return e
	}
	out, e := s.peerList(r.Context(), id)
	if e != nil {
		return e
	}
	isOwner := s.member(r.Context(), who(r).User.ID, id, true) == nil
	filtered := []model.Peer{}
	for _, p := range out {
		if isOwner || p.Status == "approved" || p.UserID == who(r).User.ID {
			filtered = append(filtered, p)
		}
	}
	output(w, 200, filtered)
	return nil
}
