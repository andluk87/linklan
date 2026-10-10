package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"linklan/internal/auth"
	"linklan/internal/model"
)

func TestDomainOnlyServerAddress(t *testing.T) {
	for _, input := range []string{"linklan.lknet.ru", "https://LINKLAN.LKNET.RU/", "https://linklan.lknet.ru:443/api"} {
		got, e := NormalizeServer(input)
		if e != nil || got != "https://linklan.lknet.ru" {
			t.Fatalf("normalize %q: %q %v", input, got, e)
		}
	}
	for _, input := range []string{"", "https://user:pass@linklan.lknet.ru", "https://linklan.lknet.ru/admin", "https://linklan.lknet.ru?token=x", "https://linklan.lknet.ru#x", "ftp://example.com"} {
		if _, e := NormalizeServer(input); e == nil {
			t.Fatalf("unsafe address accepted: %s", input)
		}
	}
}
func TestDeviceKeyProfileMigrationAndServerIsolation(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	encoded := base64.RawStdEncoding.EncodeToString(key)
	store := &Store{Data: Config{Email: "owner@example.com", Profiles: map[string]Profile{"https://linklan.lknet.ru/api|owner@example.com": {PrivateKey: encoded, DeviceID: "old-device", PeerKeys: map[string]string{"peer": "pinned"}}}}}
	a, e := NewDeviceAPI(store, "linklan.lknet.ru")
	if e != nil {
		t.Fatal(e)
	}
	if a.Profile().PrivateKey != encoded || a.Profile().DeviceID != "old-device" || a.Profile().PeerKeys["peer"] != "pinned" {
		t.Fatal("existing identity lost")
	}
	b, e := NewDeviceAPI(store, "another.example")
	if e != nil {
		t.Fatal(e)
	}
	if b.Profile().PrivateKey != "" {
		t.Fatal("identity leaked across servers")
	}
}
func TestAutomaticAccessSurvivesRestartAndExpiredRefresh(t *testing.T) {
	ctx := context.Background()
	challenges := map[string]string{}
	identities := map[string]string{}
	requests := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/device/challenge":
			var b struct {
				Public string `json:"public_key"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			challenge := auth.Random(32)
			challenges[challenge] = b.Public
			json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
		case "/api/auth/device":
			requests++
			var b struct {
				Name, Challenge, Signature, Version string
				Public                              string `json:"public_key"`
			}
			json.NewDecoder(r.Body).Decode(&b)
			pub, _ := base64.RawStdEncoding.DecodeString(b.Public)
			sig, _ := base64.RawStdEncoding.DecodeString(b.Signature)
			if challenges[b.Challenge] != b.Public || !ed25519.Verify(pub, []byte(model.DeviceProof(b.Challenge, b.Public, b.Name, b.Version)), sig) {
				t.Error("invalid signed device proof")
				w.WriteHeader(403)
				return
			}
			delete(challenges, b.Challenge)
			id := identities[b.Public]
			if id == "" {
				id = auth.UUID()
				identities[b.Public] = id
			}
			json.NewEncoder(w).Encode(model.DeviceAccess{Tokens: model.Tokens{Access: "signed-token", Refresh: "saved-refresh", User: model.User{ID: "owner"}}, Device: model.Device{ID: id, UserID: "owner", Name: b.Name, PublicKey: b.Public}})
		case "/api/auth/refresh":
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"expired"}`)
		case "/api/me":
			if r.Header.Get("Authorization") != "Bearer signed-token" {
				t.Error("token missing")
			}
			json.NewEncoder(w).Encode(model.User{ID: "owner"})
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer h.Close()
	store := &Store{Data: Config{DeviceName: "PC", Profiles: map[string]Profile{}}}
	a, e := NewDeviceAPI(store, h.URL)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.ConnectDevice(ctx, "PC"); e != nil {
		t.Fatal(e)
	}
	first := a.Profile()
	b, e := NewDeviceAPI(store, h.URL)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.ConnectDevice(ctx, "Renamed PC"); e != nil {
		t.Fatal(e)
	}
	if b.Profile().DeviceID != first.DeviceID || b.Profile().PrivateKey != first.PrivateKey {
		t.Fatal("restart replaced device")
	}
	b.expires = time.Time{}
	var user model.User
	if e = b.Call(ctx, "GET", "/me", nil, &user); e != nil {
		t.Fatal(e)
	}
	if len(identities) != 1 || requests != 3 || user.ID != "owner" {
		t.Fatal("refresh recovery created new identity", requests, len(identities))
	}
}
func TestIdentityIsSavedBeforeContactingServer(t *testing.T) {
	contacted := false
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
	defer h.Close()
	store := &Store{Data: Config{}, Save: func(Config) error { return fmt.Errorf("disk unavailable") }}
	a, e := NewDeviceAPI(store, h.URL)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.ConnectDevice(context.Background(), "PC"); e == nil || !strings.Contains(e.Error(), "disk unavailable") {
		t.Fatal("failed save ignored", e)
	}
	if contacted {
		t.Fatal("server contacted before identity persisted")
	}
	if a.Profile().PrivateKey != "" {
		t.Fatal("failed save left an unpersisted identity in memory")
	}
}
