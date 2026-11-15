// Package uibridge exposes a restricted, parent/child stdio API to the WPF UI.
// Device credentials remain in Go's DPAPI store and never enter a UI response.
package uibridge

import "linklan/internal/tunnel"

type Settings struct {
	Server          string             `json:"server"`
	DeviceName      string             `json:"deviceName"`
	Theme           string             `json:"theme"`
	Language        string             `json:"language"`
	AutoConnect     bool               `json:"autoConnect"`
	StartAtLogon    bool               `json:"startAtLogon"`
	StartInTray     bool               `json:"startInTray"`
	NotifyDesktop   bool               `json:"notifyDesktop"`
	NotifySound     bool               `json:"notifySound"`
	RespectFocus    bool               `json:"respectFocus"`
	FirewallAllowed bool               `json:"firewallAllowed"`
	Window          tunnel.WindowState `json:"window"`
	WindowDIP       bool               `json:"windowDip"`
	Expanded        map[string]bool    `json:"expanded"`
}
type Peer struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	Name        string `json:"name"`
	IP          string `json:"ip"`
	Online      bool   `json:"online"`
	Status      string `json:"status"`
	Route       string `json:"route"`
	Measured    bool   `json:"measured"`
	Reachable   bool   `json:"reachable"`
	RTT         *int64 `json:"rttMs"`
	Fingerprint string `json:"fingerprint"`
}
type Network struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnerID     string `json:"ownerId"`
	Description string `json:"description"`
	Subnet      string `json:"subnet"`
	IP          string `json:"ip"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	Blocked     bool   `json:"blocked"`
	MaxMembers  int    `json:"maxMembers"`
	Peers       []Peer `json:"peers"`
}
type State struct {
	Version         string    `json:"version"`
	Revision        uint64    `json:"revision"`
	Settings        Settings  `json:"settings"`
	APIReady        bool      `json:"apiReady"`
	Busy            bool      `json:"busy"`
	Connected       bool      `json:"connected"`
	Wanted          bool      `json:"wanted"`
	Reconnecting    bool      `json:"reconnecting"`
	Error           string    `json:"error"`
	Message         string    `json:"message"`
	ActiveNetworkID string    `json:"activeNetworkId"`
	UserID          string    `json:"userId"`
	DeviceID        string    `json:"deviceId"`
	OwnIP           string    `json:"ownIp"`
	Networks        []Network `json:"networks"`
}
type Preferences struct {
	Theme         *string             `json:"theme"`
	Language      *string             `json:"language"`
	AutoConnect   *bool               `json:"autoConnect"`
	StartAtLogon  *bool               `json:"startAtLogon"`
	StartInTray   *bool               `json:"startInTray"`
	NotifyDesktop *bool               `json:"notifyDesktop"`
	NotifySound   *bool               `json:"notifySound"`
	RespectFocus  *bool               `json:"respectFocus"`
	Window        *tunnel.WindowState `json:"window"`
	Expanded      map[string]bool     `json:"expanded"`
}

func option(v *bool, otherwise bool) bool {
	if v == nil {
		return otherwise
	}
	return *v
}
func publicSettings(c tunnel.Config) Settings {
	return Settings{c.Server, c.DeviceName, c.Theme, c.Language, c.AutoConnect, option(c.StartAtLogon, true), option(c.StartInTray, true), option(c.NotifyDesktop, true), option(c.NotifySound, true), option(c.RespectFocus, true), c.FirewallAllowed, c.Window, c.WindowDIP, c.ExpandedNetworks}
}
