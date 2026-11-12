package tunnel

import "testing"

func TestUISettingsPreserveDeviceProfileAndAreDeepCopied(t *testing.T) {
	enabled := true
	store := &Store{Data: Config{StartAtLogon: &enabled, Profiles: map[string]Profile{"device|https://example.test/api": {DeviceID: "device", PrivateKey: "test-device-key", Refresh: "test-refresh"}}, ExpandedNetworks: map[string]bool{"n": false}}}
	if err := store.Update(func(c *Config) {
		c.Window = WindowState{X: 100, Y: 100, Width: 360, Height: 680, Positioned: true}
		c.ExpandedNetworks["n"] = true
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if snapshot.Profiles["device|https://example.test/api"].PrivateKey != "test-device-key" || snapshot.Profiles["device|https://example.test/api"].Refresh != "test-refresh" {
		t.Fatal("UI update changed device credentials")
	}
	*snapshot.StartAtLogon = false
	snapshot.ExpandedNetworks["n"] = false
	actual := store.Snapshot()
	if !*actual.StartAtLogon || !actual.ExpandedNetworks["n"] || actual.Window.Width != 360 {
		t.Fatal("UI snapshot aliases stored configuration")
	}
}
