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

func TestPresenceFlapNeverReusesNonce(t *testing.T) {
	network, _ := protocol.ParseID("10000000-0000-4000-8000-000000000001")
	self, _ := protocol.ParseID("10000000-0000-4000-8000-000000000002")
	id, _ := protocol.ParseID("10000000-0000-4000-8000-000000000003")
	local, _ := ecdh.X25519().GenerateKey(rand.Reader)
	remote, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pub, identity, _ := ed25519.GenerateKey(rand.Reader)
	store := &Store{Data: Config{Profiles: map[string]Profile{"x": {}}}}
	engine := &Engine{api: &API{store: store, profileKey: "x"}, network: network, self: self, private: local, peers: map[protocol.ID]*peer{}}
	p := model.Peer{ID: id.String(), Status: "approved", Online: true, PublicKey: base64.RawStdEncoding.EncodeToString(pub), EphemeralKey: base64.RawStdEncoding.EncodeToString(remote.PublicKey().Bytes()), SessionChallenge: "n"}
	p.SessionSignature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(identity, []byte(model.SessionProof(p.SessionChallenge, p.ID, network.String(), p.EphemeralKey, p.LocalEndpoint))))
	engine.update([]model.Peer{p})
	first, _ := engine.peers[id].channel.Seal(protocol.Data, []byte("one"))
	engine.update(nil)
	engine.update([]model.Peer{p})
	second, _ := engine.peers[id].channel.Seal(protocol.Data, []byte("two"))
	h1, _ := protocol.Parse(first)
	h2, _ := protocol.Parse(second)
	if h1.Sequence >= h2.Sequence || h1.Session != h2.Session {
		t.Fatal("presence flap reset nonce counter")
	}
}
