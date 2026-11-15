package uibridge

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linklan/internal/model"
)

// Exercise the actual device/API client through UI commands, without a Windows
// adapter. Pending memberships must never start a tunnel or imply online state.
func TestUICommandsUseAuthenticatedAPIAndKeepSavedServerOnFailure(t *testing.T) {
	seen := map[string]int{}
	public := ""
	password := "long-test-password"
	network := model.Network{ID: "network-1", Name: "Friends", Status: "pending", OwnerID: "owner"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		seen[r.Method+" "+r.URL.Path]++
		switch r.URL.Path {
		case "/api/auth/device/challenge":
			var body struct {
				Public string `json:"public_key"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			public = body.Public
			json.NewEncoder(w).Encode(map[string]string{"challenge": strings.Repeat("c", 43)})
			return
		case "/api/auth/device":
			var body struct {
				Name, Version, Signature, Challenge string
				Public                              string `json:"public_key"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			pub, _ := base64.RawStdEncoding.DecodeString(body.Public)
			signature, _ := base64.RawStdEncoding.DecodeString(body.Signature)
			if public != body.Public || !ed25519.Verify(pub, []byte(model.DeviceProof(body.Challenge, body.Public, body.Name, body.Version)), signature) {
				t.Error("missing signed device proof")
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(model.DeviceAccess{Tokens: model.Tokens{Access: "private-api-token", Refresh: "private-refresh-token", User: model.User{ID: "owner"}}, Device: model.Device{ID: "device", Name: body.Name, UserID: "owner", PublicKey: body.Public}})
			return
		}
		if r.Header.Get("Authorization") != "Bearer private-api-token" {
			t.Error("UI operation did not use authenticated engine API")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/networks/state":
			if r.URL.Query().Get("device_id") != "device" {
				t.Error("wrong device identity")
			}
			json.NewEncoder(w).Encode([]model.NetworkView{{Network: network}})
		case "/api/networks":
			var body struct {
				Name, Password, Mode string
				Device               string `json:"device_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Password != password || body.Device != "device" || body.Mode != "password" {
				t.Errorf("incorrect network payload %#v", body)
			}
			json.NewEncoder(w).Encode(network)
		case "/api/invite":
			json.NewEncoder(w).Encode(map[string]string{"token": "invitation-token"})
		case "/api/invite/invitation-token":
			json.NewEncoder(w).Encode(map[string]string{"network_id": network.ID})
		case "/api/networks/network-1/join":
			var values map[string]string
			json.NewDecoder(r.Body).Decode(&values)
			if values["token"] != "invitation-token" || values["device_id"] != "device" {
				t.Errorf("invalid join payload %#v", values)
			}
			json.NewEncoder(w).Encode(network)
		case "/api/networks/network-1/members/peer/approve", "/api/networks/network-1/members/peer/block", "/api/networks/network-1/members/peer", "/api/networks/network-1/leave", "/api/networks/network-1":
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			t.Errorf("unexpected API operation %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	store := fixtureStore()
	ctx, cancel := context.WithCancel(context.Background())
	c := &Controller{ctx: ctx, cancel: cancel, store: store, echoes: map[string]echo{}}
	defer c.Close()
	call := func(method string, body any) any {
		t.Helper()
		raw, _ := json.Marshal(body)
		result, err := c.Handle(method, raw)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return result
	}
	call("server", map[string]string{"server": server.URL, "deviceName": "Migrated PC"})
	if !c.State().APIReady || c.State().DeviceID != "device" || c.State().Connected {
		t.Fatal("incorrect authenticated state")
	}
	beforeProfile := store.Snapshot().Profiles
	beforeSerial := c.connectSerial
	call("rename", map[string]string{"deviceName": "Renamed inline"})
	if store.Snapshot().DeviceName != "Renamed inline" || c.connectSerial != beforeSerial || c.State().DeviceID != "device" {
		t.Fatal("rename reset identity/session")
	}
	for key, p := range beforeProfile {
		if store.Snapshot().Profiles[key].PrivateKey != p.PrivateKey {
			t.Fatal("rename replaced private key")
		}
	}

	call("create", map[string]string{"name": "Friends", "password": password, "confirm": password})
	if c.State().Wanted || c.State().Connected {
		t.Fatal("pending membership started VPN")
	}
	if token := call("invite", map[string]string{"networkId": network.ID}); token != "invitation-token" {
		t.Fatalf("incorrect invitation result %v", token)
	}
	call("join", map[string]string{"invite": "invitation-token"})
	for _, method := range []string{"approve-member", "block-member", "remove-member", "leave", "delete"} {
		call(method, map[string]string{"networkId": network.ID, "peerId": "peer"})
	}
	cfg := store.Snapshot()
	raw, _ := json.Marshal(c.State())
	if strings.Contains(string(raw), "private-api-token") || strings.Contains(string(raw), "private-refresh-token") {
		t.Fatal("API secrets reached UI")
	}
	invalid, _ := json.Marshal(map[string]string{"server": "https://user:password@example.test", "deviceName": "bad"})
	if _, err := c.Handle("server", invalid); err == nil {
		t.Fatal("unsafe server accepted")
	}
	after := store.Snapshot()
	if after.Server != cfg.Server || after.DeviceName != cfg.DeviceName || !c.State().APIReady {
		t.Fatal("failed server change destroyed working configuration")
	}
	for _, path := range []string{"POST /api/networks", "POST /api/networks/network-1/join", "POST /api/networks/network-1/members/peer/approve", "POST /api/networks/network-1/members/peer/block", "DELETE /api/networks/network-1/members/peer", "POST /api/networks/network-1/leave", "DELETE /api/networks/network-1"} {
		if seen[path] != 1 {
			t.Errorf("missing or duplicated action %s: %d", path, seen[path])
		}
	}
}
