package backend

import (
	"encoding/json"
	"linklan/internal/model"
	"net/http"
)

func (s *Server) adminState(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	state := map[string]any{}
	users := []model.User{}
	rows, e := s.db.Query(ctx, `SELECT id::text,email,name,admin,blocked FROM users ORDER BY created_at DESC LIMIT 200`)
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
	rows.Close()
	state["users"] = users
	networks := []model.Network{}
	rows, e = s.db.Query(ctx, `SELECT `+networkColumns+` FROM networks ORDER BY created_at DESC LIMIT 200`)
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
	rows.Close()
	state["networks"] = networks
	devices := []map[string]any{}
	rows, e = s.db.Query(ctx, `SELECT id::text,user_id::text,name,blocked FROM devices ORDER BY last_seen DESC LIMIT 200`)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id, uid, name string
		var blocked bool
		rows.Scan(&id, &uid, &name, &blocked)
		online := s.redis.Exists(ctx, "endpoint:"+id).Val() > 0
		devices = append(devices, map[string]any{"id": id, "user_id": uid, "name": name, "blocked": blocked, "online": online})
	}
	rows.Close()
	state["devices"] = devices
	audit := []map[string]any{}
	rows, e = s.db.Query(ctx, `SELECT action,object_id,created_at::text FROM audit_log ORDER BY id DESC LIMIT 100`)
	if e != nil {
		return e
	}
	for rows.Next() {
		var action, id, at string
		rows.Scan(&action, &id, &at)
		audit = append(audit, map[string]any{"action": action, "object": id, "at": at})
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
	if _, e := s.db.Exec(r.Context(), `UPDATE `+table+` SET blocked=$1 WHERE id=$2`, b.Blocked, b.ID); e != nil {
		return e
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

const adminJS = `
let access=''; const $=id=>document.getElementById(id);
async function request(path,method='GET',body){const r=await fetch(path,{method,headers:{'Content-Type':'application/json','Authorization':'Bearer '+access},body:body?JSON.stringify(body):undefined});const v=await r.json();if(!r.ok)throw Error(v.error||'Ошибка');return v;}
function msg(s){$('message').textContent=s}
$('login').onsubmit=async e=>{e.preventDefault();try{const t=await request('/api/auth/login','POST',{email:$('email').value,password:$('password').value});if(!t.user.admin)throw Error('Нужны права администратора');access=t.access_token;$('password').value='';$('login').hidden=true;$('content').hidden=false;await refresh()}catch(e){msg(e.message)}};
async function block(kind,id,blocked){try{await request('/admin/block','POST',{kind,id,blocked});await refresh()}catch(e){msg(e.message)}}
function render(target,items,kind,columns){const root=$(target);root.replaceChildren();const table=document.createElement('table');const head=table.createTHead().insertRow();columns.forEach(c=>{const th=document.createElement('th');th.textContent=c;head.append(th)});if(kind){const th=document.createElement('th');th.textContent='Действие';head.append(th)}const tbody=table.createTBody();items.forEach(item=>{const row=tbody.insertRow();columns.forEach(c=>{const cell=row.insertCell();cell.textContent=String(item[c]??'—')});if(kind){const btn=document.createElement('button');btn.textContent=item.blocked?'Разблокировать':'Заблокировать';btn.onclick=()=>block(kind,item.id,!item.blocked);row.insertCell().append(btn)}});root.append(table)}
async function refresh(){try{const s=await request('/admin/state');render('users',s.users,'user',['name','email','id','blocked']);render('networks',s.networks,'network',['name','subnet','id','blocked']);render('devices',s.devices,'device',['name','online','id','blocked']);render('audit',s.audit,null,['at','action','object']);$('relay').textContent=JSON.stringify(s.relay||{},null,2);msg('Обновлено: '+new Date().toLocaleTimeString())}catch(e){msg(e.message)}}
$('reload').onclick=refresh;$('logout').onclick=async()=>{try{await request('/api/auth/logout','POST',{})}catch{}access='';location.reload()};
`
