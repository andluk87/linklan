package uibridge

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"linklan/internal/tunnel"
)

func fixtureStore() *tunnel.Store {
	return &tunnel.Store{Data: tunnel.Config{DeviceName: "PC", Theme: "light", Language: "ru", NetworkID: "saved-network", Profiles: map[string]tunnel.Profile{"device|https://example.test/api": {DeviceID: "preserved-device", PrivateKey: "SECRET_PRIVATE", Refresh: "SECRET_REFRESH", PeerKeys: map[string]string{"peer": "PINNED_KEY"}}}}}
}
func TestPublicStateAndPreferencesPreserveIdentity(t *testing.T) {
	store := fixtureStore()
	c := New(store, Options{Preview: true})
	defer c.Close()
	_, err := c.Handle("settings", json.RawMessage(`{"theme":"dark","language":"en","startInTray":false,"window":{"width":440,"height":500,"x":100,"y":50,"positioned":true},"profiles":{},"networkID":"untrusted"}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Snapshot()
	profile := cfg.Profiles["device|https://example.test/api"]
	if cfg.Theme != "dark" || cfg.Language != "en" || cfg.StartInTray == nil || *cfg.StartInTray || !cfg.WindowDIP || profile.PrivateKey != "SECRET_PRIVATE" || profile.Refresh != "SECRET_REFRESH" || profile.PeerKeys["peer"] != "PINNED_KEY" || cfg.NetworkID != "saved-network" {
		t.Fatalf("preferences replaced identity: %#v", cfg)
	}
	raw, err := json.Marshal(c.State())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET_PRIVATE", "SECRET_REFRESH", "PINNED_KEY", "profiles", "private_key", "refresh_token"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("public state leaked %s", secret)
		}
	}
}
func TestPreferenceValidationIsAtomic(t *testing.T) {
	store := fixtureStore()
	c := New(store, Options{Preview: true})
	defer c.Close()
	for _, raw := range []string{`{"theme":"unknown"}`, `{"language":"xx"}`, `{"window":{"width":249,"height":500}}`} {
		if _, err := c.Handle("settings", json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if store.Snapshot().Theme != "light" {
		t.Fatal("invalid patch saved")
	}
	store.Save = func(tunnel.Config) error { return io.ErrClosedPipe }
	if _, err := c.Handle("settings", json.RawMessage(`{"theme":"dark"}`)); err == nil {
		t.Fatal("save failure lost")
	}
	if store.Snapshot().Theme != "light" {
		t.Fatal("failed persistence changed state")
	}
}
func TestStdioConcurrentFramesAndEOF(t *testing.T) {
	input, send := io.Pipe()
	output, receive := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(fixtureStore(), Options{Preview: true}, input, receive); receive.Close() }()
	defer send.Close()
	defer output.Close()
	var write sync.WaitGroup
	write.Add(1)
	go func() {
		defer write.Done()
		for i := 1; i <= 40; i++ {
			raw, _ := json.Marshal(request{ID: uint64(i), Method: "state"})
			if _, e := send.Write(append(raw, '\n')); e != nil {
				return
			}
		}
	}()
	seen := map[uint64]bool{}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for len(seen) < 40 && scanner.Scan() {
		var reply response
		if e := json.Unmarshal(scanner.Bytes(), &reply); e != nil {
			t.Fatal(e)
		}
		if seen[reply.ID] || reply.ID == 0 || reply.Error != "" {
			t.Fatalf("invalid response %#v", reply)
		}
		seen[reply.ID] = true
	}
	write.Wait()
	send.Close()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("engine survived parent EOF")
	}
	if len(seen) != 40 {
		t.Fatalf("got %d replies", len(seen))
	}
}
func TestPreviewPowerAndUnknownCommands(t *testing.T) {
	c := New(fixtureStore(), Options{Preview: true, Large: true})
	defer c.Close()
	if c.State().Networks[0].Peers[1].Name != c.State().Networks[0].Peers[2].Name {
		t.Fatal("duplicate-name regression fixture missing")
	}
	if len(c.State().Networks) != 50 {
		t.Fatal("large preview missing")
	}
	if _, e := c.Handle("disconnect", nil); e != nil {
		t.Fatal(e)
	}
	if c.State().Connected {
		t.Fatal("still connected")
	}
	if _, e := c.Handle("connect", json.RawMessage(`{"networkId":"preview-1"}`)); e != nil {
		t.Fatal(e)
	}
	if !c.State().Connected {
		t.Fatal("power not connected")
	}
	if _, e := c.Handle("anything", nil); e == nil {
		t.Fatal("unknown method accepted")
	}
}
