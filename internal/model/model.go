package model

import "time"

var Version = "0.2.3"

func DeviceProof(challenge, publicKey, name, version string) string {
	return "LinkLAN device access|" + challenge + "|" + publicKey + "|" + name + "|" + version
}

func SessionProof(challenge, device, network, ephemeral, local string) string {
	return "LinkLAN session|" + challenge + "|" + device + "|" + network + "|" + ephemeral + "|" + local
}

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Admin   bool   `json:"admin"`
	Blocked bool   `json:"blocked"`
}
type Tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	User    User   `json:"user"`
}
type DeviceAccess struct {
	Tokens
	Device Device `json:"device"`
}
type NetworkView struct {
	Network Network `json:"network"`
	Peers   []Peer  `json:"peers"`
}
type Device struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
	Blocked   bool   `json:"blocked"`
}
type Network struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     string `json:"owner_id"`
	Subnet      string `json:"subnet"`
	Mode        string `json:"mode"`
	MaxMembers  int    `json:"max_members"`
	Blocked     bool   `json:"blocked"`
	Status      string `json:"status"`
	IP          string `json:"virtual_ip"`
	Members     int    `json:"members"`
}
type Peer struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Name             string    `json:"name"`
	UserName         string    `json:"user_name"`
	IP               string    `json:"virtual_ip"`
	PublicKey        string    `json:"public_key"`
	EphemeralKey     string    `json:"ephemeral_key"`
	SessionSignature string    `json:"session_signature"`
	SessionChallenge string    `json:"session_challenge"`
	Endpoint         string    `json:"endpoint"`
	LocalEndpoint    string    `json:"local_endpoint"`
	Online           bool      `json:"online"`
	Status           string    `json:"status"`
	LastSeen         time.Time `json:"last_seen"`
	Version          string    `json:"version"`
}

// RelayAuth is trusted server-side state, never sent in UDP registration packets.
type RelayAuth struct {
	Key           string `json:"key"`
	NetworkID     string `json:"network_id"`
	EphemeralKey  string `json:"ephemeral_key"`
	Signature     string `json:"signature"`
	Challenge     string `json:"challenge"`
	LocalEndpoint string `json:"local_endpoint"`
	UserID        string `json:"user_id"`
	AuthSession   string `json:"auth_session"`
	Generation    string `json:"generation"`
}
type TunnelSession struct {
	RelayAddress string  `json:"relay_address"`
	STUNAddress  string  `json:"stun_address"`
	RelayKey     string  `json:"relay_key"`
	Generation   string  `json:"generation"`
	Network      Network `json:"network"`
	Peers        []Peer  `json:"peers"`
}
