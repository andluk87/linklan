package tunnel

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"linklan/internal/model"
	"linklan/internal/protocol"
	"testing"
)

func TestPeerEphemeralIdentityProof(t *testing.T) {
	netID, _ := protocol.ParseID("10000000-0000-4000-8000-000000000001")
	self, _ := protocol.ParseID("10000000-0000-4000-8000-000000000002")
	id := "10000000-0000-4000-8000-000000000003"
	private, _ := ecdh.X25519().GenerateKey(rand.Reader)
	remote, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pub, identity, _ := ed25519.GenerateKey(rand.Reader)
	store := &Store{Data: Config{Profiles: map[string]Profile{"test": {PeerKeys: map[string]string{}}}}}
	a := &API{store: store, profileKey: "test"}
	engine := &Engine{api: a, network: netID, self: self, private: private, peers: map[protocol.ID]*peer{}}
	p := model.Peer{ID: id, Name: "PC", Status: "approved", Online: true, PublicKey: base64.RawStdEncoding.EncodeToString(pub), EphemeralKey: base64.RawStdEncoding.EncodeToString(remote.PublicKey().Bytes()), SessionChallenge: "challenge", LocalEndpoint: "192.168.1.3:5000"}
	p.SessionSignature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(identity, []byte(model.SessionProof(p.SessionChallenge, id, netID.String(), p.EphemeralKey, p.LocalEndpoint))))
	engine.update([]model.Peer{p})
	if len(engine.peers) != 1 {
		t.Fatal("valid proof rejected")
	}
	p.EphemeralKey = base64.RawStdEncoding.EncodeToString(private.PublicKey().Bytes())
	engine.update([]model.Peer{p})
	if len(engine.peers) != 0 {
		t.Fatal("unsigned ephemeral substitution accepted")
	}
	if e := a.Pin(id, "another-key"); e == nil {
		t.Fatal("pinned identity change accepted")
	}
}
func TestServerTLSAndEndpointValidation(t *testing.T) {
	s := &Store{Data: Config{}}
	for _, server := range []string{"http://203.0.113.1:8191", "https://user:pass@host", "https://host?token=x", "file:///tmp/x"} {
		if _, e := NewAPI(s, server, "a@b.co"); e == nil {
			t.Fatalf("accepted %s", server)
		}
	}
	if _, e := NewAPI(s, "http://localhost:8191", "a@b.co"); e != nil {
		t.Fatal(e)
	}
	for _, ep := range []string{"127.0.0.1:5000", "169.254.169.254:5000", "224.0.0.1:5000", "192.168.1.2:80"} {
		if _, ok := validEndpoint(ep); ok {
			t.Fatalf("accepted %s", ep)
		}
	}
}
