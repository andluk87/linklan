package backend

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"linklan/internal/auth"
	"linklan/internal/model"
)

func keyAccess(t *testing.T, h *httptest.Server, key ed25519.PrivateKey, name string) model.DeviceAccess {
	t.Helper()
	pub := base64.RawStdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
	var challenge struct{ Challenge string }
	if code := request(t, h, "POST", "/auth/device/challenge", "", map[string]string{"public_key": pub}, &challenge); code != 200 {
		t.Fatal("device challenge", code)
	}
	sig := base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, []byte(model.DeviceProof(challenge.Challenge, pub, name, model.Version))))
	var access model.DeviceAccess
	if code := request(t, h, "POST", "/auth/device", "", map[string]string{"name": name, "public_key": pub, "challenge": challenge.Challenge, "signature": sig, "version": model.Version}, &access); code != 200 {
		t.Fatal("device access", code)
	}
	return access
}

func TestDeviceAccessRetainsIdentityAndMembership(t *testing.T) {
	s, h := integrationServer(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	first := keyAccess(t, h, key, "Device PC")
	if first.Access == "" || first.Device.ID == "" {
		t.Fatal("missing device access")
	}
	var n model.Network
	if code := request(t, h, "POST", "/networks", first.Access, map[string]any{"name": "No account network", "mode": "password", "password": "private network password", "device_id": first.Device.ID}, &n); code != 201 {
		t.Fatal("create", code)
	}
	second := keyAccess(t, h, key, "Renamed PC")
	if first.User.ID != second.User.ID || first.Device.ID != second.Device.ID || second.Device.Name != "Renamed PC" || second.User.Name != "Renamed PC" {
		t.Fatal("device identity changed")
	}
	var views []model.NetworkView
	if code := request(t, h, "GET", "/networks/state?device_id="+second.Device.ID, second.Access, nil, &views); code != 200 || len(views) != 1 || views[0].Network.ID != n.ID {
		t.Fatal("membership lost", code, views)
	}
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	other := keyAccess(t, h, otherKey, "Another PC")
	if code := request(t, h, "GET", "/networks/state?device_id="+second.Device.ID, other.Access, nil, nil); code != 403 {
		t.Fatal("another device's networks exposed", code)
	}
	if _, e := s.db.Exec(context.Background(), `UPDATE devices SET blocked=true WHERE id=$1`, first.Device.ID); e != nil {
		t.Fatal(e)
	}
	public := first.Device.PublicKey
	var challenge struct{ Challenge string }
	request(t, h, "POST", "/auth/device/challenge", "", map[string]string{"public_key": public}, &challenge)
	sig := base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, []byte(model.DeviceProof(challenge.Challenge, public, "PC", model.Version))))
	if code := request(t, h, "POST", "/auth/device", "", map[string]string{"name": "PC", "public_key": public, "challenge": challenge.Challenge, "signature": sig, "version": model.Version}, nil); code != 403 {
		t.Fatal("blocked device regained access", code)
	}
}
func TestExistingAccountDeviceUsesKeyWithoutLosingOwner(t *testing.T) {
	_, h := integrationServer(t)
	account := account(t, h, "upgrade-owner")
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	public := base64.RawStdEncoding.EncodeToString(pub)
	sig := base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, []byte("LinkLAN registration|Old PC|"+public)))
	var d model.Device
	if code := request(t, h, "POST", "/devices/register", account.Access, map[string]string{"name": "Old PC", "public_key": public, "signature": sig, "version": "0.1.0"}, &d); code != 200 {
		t.Fatal(code)
	}
	access := keyAccess(t, h, key, "Old PC")
	if access.User.ID != account.User.ID || access.Device.ID != d.ID {
		t.Fatal("upgrade lost old device or account ownership")
	}
}

// A new isolated schema is created and dropped. No existing table is mutated.
func integrationServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	raw := os.Getenv("LINKLAN_TEST_DATABASE")
	if raw == "" {
		t.Skip("LINKLAN_TEST_DATABASE not set: PostgreSQL integration test requires a disposable test database")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	schema := "ll_test_" + strings.ReplaceAll(auth.UUID(), "-", "")
	if _, e = db.Exec(ctx, `CREATE SCHEMA `+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`) })
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	mini := miniredis.RunT(t)
	s, e := New(ctx, Config{Database: u.String(), Redis: "redis://" + mini.Addr(), Secret: strings.Repeat("test-secret", 5), Relay: "127.0.0.1:51820", STUN: "127.0.0.1:3478"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	h := httptest.NewServer(s.Router())
	t.Cleanup(h.Close)
	return s, h
}
func request(t *testing.T, h *httptest.Server, method, path, token string, body, out any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	r, e := http.NewRequest(method, h.URL+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := h.Client().Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if out != nil {
		if e = json.NewDecoder(resp.Body).Decode(out); e != nil {
			t.Fatal(e)
		}
	}
	return resp.StatusCode
}
func account(t *testing.T, h *httptest.Server, name string) model.Tokens {
	t.Helper()
	var token model.Tokens
	if code := request(t, h, "POST", "/auth/register", "", map[string]string{"email": name + "@example.test", "name": name, "password": "long integration password"}, &token); code != 200 {
		t.Fatal("register", code)
	}
	return token
}
func device(t *testing.T, h *httptest.Server, token, name string) model.Device {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	public := base64.RawStdEncoding.EncodeToString(pub)
	sig := base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, []byte("LinkLAN registration|"+name+"|"+public)))
	var d model.Device
	if code := request(t, h, "POST", "/devices/register", token, map[string]string{"name": name, "public_key": public, "signature": sig, "version": model.Version}, &d); code != 200 {
		t.Fatal("device", code)
	}
	return d
}
func TestNetworkAuthorizationAndIPAllocation(t *testing.T) {
	_, h := integrationServer(t)
	owner, guest, outsider := account(t, h, "owner"), account(t, h, "guest"), account(t, h, "outsider")
	a, b := device(t, h, owner.Access, "PC A"), device(t, h, guest.Access, "PC B")
	var n model.Network
	if code := request(t, h, "POST", "/networks", owner.Access, map[string]any{"name": "private test", "mode": "invite", "device_id": a.ID, "max_members": 2}, &n); code != 201 {
		t.Fatal("create", code)
	}
	if n.IP == "" {
		t.Fatal("owner IP missing")
	}
	if code := request(t, h, "GET", "/networks/"+n.ID+"/members", outsider.Access, nil, nil); code != 403 {
		t.Fatal("outsider read", code)
	}
	if code := request(t, h, "POST", "/networks/"+n.ID+"/join", guest.Access, map[string]string{"device_id": b.ID}, nil); code != 403 {
		t.Fatal("join without invite", code)
	}
	var invite struct{ Token string }
	if code := request(t, h, "POST", "/invite", owner.Access, map[string]any{"network_id": n.ID, "uses": 1, "hours": 1}, &invite); code != 201 {
		t.Fatal("invite", code)
	}
	var joined model.Network
	if code := request(t, h, "POST", "/networks/"+n.ID+"/join", guest.Access, map[string]string{"device_id": b.ID, "token": invite.Token}, &joined); code != 200 {
		t.Fatal("join", code)
	}
	if joined.IP == n.IP || joined.IP == "" {
		t.Fatal("duplicate IP")
	}
	if code := request(t, h, "DELETE", "/networks/"+n.ID+"/members/"+a.ID, guest.Access, nil, nil); code != 403 {
		t.Fatal("nonowner removal", code)
	}
	if code := request(t, h, "DELETE", "/networks/"+n.ID+"/members/"+b.ID, owner.Access, nil, nil); code != 200 {
		t.Fatal("owner removal", code)
	}
	if code := request(t, h, "GET", "/networks/"+n.ID+"/members", guest.Access, nil, nil); code != 403 {
		t.Fatal("removed device access", code)
	}
}
func TestRefreshReplayRevokesEntireFamily(t *testing.T) {
	_, h := integrationServer(t)
	first := account(t, h, "refresh")
	var second model.Tokens
	if code := request(t, h, "POST", "/auth/refresh", "", map[string]string{"refresh_token": first.Refresh}, &second); code != 200 {
		t.Fatal("rotate", code)
	}
	if second.Refresh == first.Refresh {
		t.Fatal("refresh not rotated")
	}
	if code := request(t, h, "POST", "/auth/refresh", "", map[string]string{"refresh_token": first.Refresh}, nil); code != 401 {
		t.Fatal("replay accepted", code)
	}
	if code := request(t, h, "GET", "/me", second.Access, nil, nil); code != 401 {
		t.Fatal("family access survived replay", code)
	}
	if code := request(t, h, "POST", "/auth/refresh", "", map[string]string{"refresh_token": second.Refresh}, nil); code != 401 {
		t.Fatal("family refresh survived replay", code)
	}
}
func TestApprovalAndAdminRole(t *testing.T) {
	s, h := integrationServer(t)
	owner, guest := account(t, h, "approval-owner"), account(t, h, "approval-guest")
	a, b := device(t, h, owner.Access, "Approval A"), device(t, h, guest.Access, "Approval B")
	var n model.Network
	if code := request(t, h, "POST", "/networks", owner.Access, map[string]any{"name": "approval test", "mode": "approval", "device_id": a.ID, "max_members": 3}, &n); code != 201 {
		t.Fatal(code)
	}
	if code := request(t, h, "POST", "/networks/"+n.ID+"/join", guest.Access, map[string]string{"device_id": b.ID}, nil); code != 202 {
		t.Fatal("pending", code)
	}
	if code := request(t, h, "GET", "/networks/"+n.ID+"/members", guest.Access, nil, nil); code != 403 {
		t.Fatal("pending access", code)
	}
	if code := request(t, h, "POST", "/networks/"+n.ID+"/members/"+b.ID+"/approve", owner.Access, map[string]string{}, nil); code != 200 {
		t.Fatal("approve", code)
	}
	if code := request(t, h, "GET", "/admin/state", guest.Access, nil, nil); code != 403 {
		t.Fatal("nonadmin", code)
	}
	s.db.Exec(context.Background(), `UPDATE users SET admin=true WHERE id=$1`, owner.User.ID)
	if code := request(t, h, "GET", "/admin/state", owner.Access, nil, nil); code != 200 {
		t.Fatal("admin", code)
	}
	if code := request(t, h, "POST", "/admin/block", owner.Access, map[string]any{"kind": "user", "id": guest.User.ID, "blocked": true}, nil); code != 200 {
		t.Fatal("block", code)
	}
	if code := request(t, h, "GET", "/me", guest.Access, nil, nil); code != 401 {
		t.Fatal("blocked account", code)
	}
}
