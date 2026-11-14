package backend

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"linklan/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminTokenIsRequiredAndDisabledWithoutConfiguration(t *testing.T) {
	s := &Server{}
	called := false
	handler := func(w http.ResponseWriter, r *http.Request) error { called = true; return nil }
	for _, token := range []string{"", strings.Repeat("x", 64)} {
		r := httptest.NewRequest("GET", "/admin/state", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		if s.admin(handler)(httptest.NewRecorder(), r) == nil {
			t.Fatal("unconfigured admin accepted")
		}
	}
	s.cfg.AdminToken = strings.Repeat("x", 64)
	for _, token := range []string{"", strings.Repeat("y", 64), s.cfg.AdminToken} {
		r := httptest.NewRequest("GET", "/admin/state", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		called = false
		e := s.admin(handler)(httptest.NewRecorder(), r)
		if (e == nil) != (token == s.cfg.AdminToken) || called != (token == s.cfg.AdminToken) {
			t.Fatal("token authorization mismatch")
		}
	}
}
func TestAdminManagementRevokesAndPersists(t *testing.T) {
	s, h := integrationServer(t)
	s.cfg.AdminToken = strings.Repeat("test-admin-secret", 4)
	token := s.cfg.AdminToken
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	device := keyAccess(t, h, key, "Managed PC")
	var network model.Network
	if code := request(t, h, "POST", "/networks", device.Access, map[string]any{"name": "Managed network", "mode": "approval", "device_id": device.Device.ID}, &network); code != 201 {
		t.Fatal("network", code)
	}
	if code := request(t, h, "POST", "/admin/create", token, map[string]any{"kind": "user", "data": map[string]string{"name": "Created by admin", "email": "created@example.test", "password": "a secure test password"}}, nil); code != 200 {
		t.Fatal("create user", code)
	}
	if code := request(t, h, "POST", "/admin/create", token, map[string]any{"kind": "network", "data": map[string]string{"name": "Created by admin", "mode": "approval", "device_id": device.Device.ID}}, nil); code != 201 {
		t.Fatal("create network", code)
	}
	ctx := context.Background()
	s.redis.Set(ctx, "endpoint:"+device.Device.ID, "test", 0)
	s.redis.Set(ctx, "auth:"+device.Device.ID, "test", 0)
	if code := request(t, h, "POST", "/admin/manage", device.Access, map[string]any{"kind": "device", "id": device.Device.ID, "action": "delete"}, nil); code != 401 {
		t.Fatal("ordinary user managed server", code)
	}
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "device", "id": device.Device.ID, "action": "disconnect"}, nil); code != 200 {
		t.Fatal("disconnect", code)
	}
	if s.redis.Exists(ctx, "endpoint:"+device.Device.ID, "auth:"+device.Device.ID).Val() != 0 {
		t.Fatal("device still authorized")
	}
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "network", "id": network.ID, "action": "rename", "name": "Renamed network"}, nil); code != 200 {
		t.Fatal("rename", code)
	}
	user := account(t, h, "password-reset-target")
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "user", "id": user.User.ID, "action": "password", "password": "another secure password"}, nil); code != 200 {
		t.Fatal("password reset", code)
	}
	if code := request(t, h, "GET", "/me", user.Access, nil, nil); code != 401 {
		t.Fatal("old session survived password reset", code)
	}
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "network", "id": network.ID, "action": "password", "password": "new network password"}, nil); code != 200 {
		t.Fatal("network password", code)
	}
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "network", "id": network.ID, "action": "owner", "owner": user.User.ID}, nil); code != 200 {
		t.Fatal("network owner", code)
	}
	if code := request(t, h, "POST", "/admin/settings", token, map[string]any{"registration_open": false, "maintenance": true}, nil); code != 200 {
		t.Fatal("settings", code)
	}
	if code := request(t, h, "POST", "/networks", device.Access, map[string]any{"name": "Forbidden network", "mode": "approval", "device_id": device.Device.ID}, nil); code != 503 {
		t.Fatal("maintenance ignored", code)
	}
	var state map[string]any
	if code := request(t, h, "GET", "/admin/state", token, nil, &state); code != 200 {
		t.Fatal("state", code)
	}
	if state["settings"].(map[string]any)["maintenance"] != true {
		t.Fatal("settings lost")
	}
	if code := request(t, h, "GET", "/admin/members?network="+network.ID, token, nil, nil); code != 200 {
		t.Fatal("members", code)
	}
	for _, action := range []string{"block", "approve", "remove"} {
		if code := request(t, h, "POST", "/admin/member", token, map[string]string{"network": network.ID, "device": device.Device.ID, "action": action}, nil); code != 200 {
			t.Fatal(action, code)
		}
	}
	if code := request(t, h, "POST", "/admin/manage", token, map[string]any{"kind": "device", "id": device.Device.ID, "action": "delete"}, nil); code != 200 {
		t.Fatal("delete", code)
	}
	var count int
	if e := s.db.QueryRow(ctx, "SELECT count(*) FROM devices WHERE id=$1", device.Device.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("delete failed", e)
	}
	s.cfg.AdminToken = strings.Repeat("rotated-token", 5)
	if code := request(t, h, "GET", "/admin/state", token, nil, nil); code != 401 {
		t.Fatal("old token survived rotation", code)
	}
}
