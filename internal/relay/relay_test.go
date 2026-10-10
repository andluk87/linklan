package relay

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"linklan/internal/model"
	"linklan/internal/protocol"
)

func socket(t *testing.T) *net.UDPConn {
	t.Helper()
	c, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func read(t *testing.T, c *net.UDPConn) []byte {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1600)
	n, _, e := c.ReadFromUDP(b)
	if e != nil {
		t.Fatal(e)
	}
	return b[:n]
}
func register(t *testing.T, r *Relay, server, c *net.UDPConn, id protocol.ID, key []byte) {
	t.Helper()
	nonce := make([]byte, 16)
	rand.Read(nonce)
	hello := make([]byte, 36)
	copy(hello, "LLH1")
	copy(hello[4:20], id[:])
	copy(hello[20:], nonce)
	r.handle(context.Background(), server, hello, c.LocalAddr().(*net.UDPAddr))
	challenge := read(t, c)
	if string(challenge[:4]) != "LLC1" {
		t.Fatal("challenge missing")
	}
	auth := make([]byte, 100)
	copy(auth, "LLA1")
	copy(auth[4:68], challenge[4:])
	copy(auth[68:], protocol.MAC(key, auth[:68]))
	r.handle(context.Background(), server, auth, c.LocalAddr().(*net.UDPAddr))
	ack := read(t, c)
	if string(ack[:4]) != "LLO1" || !protocol.CheckMAC(key, ack[:36], ack[36:]) {
		t.Fatal("relay authentication failed")
	}
}
func TestRelayAuthenticatedForwardingAndReplay(t *testing.T) {
	ctx := context.Background()
	mini := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer rc.Close()
	r, e := New(rc, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	server, a, b := socket(t), socket(t), socket(t)
	aid, _ := protocol.ParseID("10000000-0000-4000-8000-000000000001")
	bid, _ := protocol.ParseID("10000000-0000-4000-8000-000000000002")
	network, _ := protocol.ParseID("10000000-0000-4000-8000-000000000003")
	keyA, keyB := make([]byte, 32), make([]byte, 32)
	rand.Read(keyA)
	rand.Read(keyB)
	for _, v := range []struct {
		id  protocol.ID
		key []byte
	}{{aid, keyA}, {bid, keyB}} {
		a := model.RelayAuth{Key: base64.RawURLEncoding.EncodeToString(v.key), NetworkID: network.String(), Generation: v.id.String()}
		raw, _ := json.Marshal(a)
		rc.Set(ctx, "auth:"+v.id.String(), raw, time.Minute)
	}
	register(t, r, server, a, aid, keyA)
	register(t, r, server, b, bid, keyB)
	pA, _ := ecdh.X25519().GenerateKey(rand.Reader)
	pB, _ := ecdh.X25519().GenerateKey(rand.Reader)
	chA, _ := protocol.NewChannel(network, aid, bid, pA, pB.PublicKey().Bytes())
	chB, _ := protocol.NewChannel(network, bid, aid, pB, pA.PublicKey().Bytes())
	frame, _ := chA.Seal(protocol.Data, []byte("private IP payload"))
	envelope := protocol.WrapRelay(aid, 1, keyA, frame)
	r.handle(ctx, server, envelope, a.LocalAddr().(*net.UDPAddr))
	received := read(t, b)
	if _, plain, e := chB.Open(received); e != nil || string(plain) != "private IP payload" {
		t.Fatal("ciphertext roundtrip failed", e)
	}
	if r.Packets.Load() != 1 {
		t.Fatal("forwarding metric")
	}
	r.handle(ctx, server, envelope, a.LocalAddr().(*net.UDPAddr))
	if r.Packets.Load() != 1 {
		t.Fatal("replayed envelope forwarded")
	}
	tampered := protocol.WrapRelay(aid, 2, keyA, frame)
	tampered[40] ^= 1
	r.handle(ctx, server, tampered, a.LocalAddr().(*net.UDPAddr))
	if r.Packets.Load() != 1 {
		t.Fatal("bad relay MAC forwarded")
	}
	other, _ := protocol.ParseID("10000000-0000-4000-8000-000000000004")
	raw, _ := json.Marshal(model.RelayAuth{Key: base64.RawURLEncoding.EncodeToString(keyB), NetworkID: other.String(), Generation: bid.String()})
	rc.Set(ctx, "auth:"+bid.String(), raw, time.Minute)
	delete(r.cache, bid)
	r.handle(ctx, server, protocol.WrapRelay(aid, 3, keyA, frame), a.LocalAddr().(*net.UDPAddr))
	if r.Packets.Load() != 1 {
		t.Fatal("cross-network traffic forwarded")
	}
}
func TestCookieBoundToEndpoint(t *testing.T) {
	id, _ := protocol.ParseID("10000000-0000-4000-8000-000000000001")
	secret := make([]byte, 32)
	nonce := make([]byte, 16)
	now := time.Now()
	cookie := protocol.Cookie(secret, "1.2.3.4:5000", id, nonce, now.Unix()/30)
	if !protocol.VerifyCookie(secret, "1.2.3.4:5000", id, nonce, cookie, now) || protocol.VerifyCookie(secret, "1.2.3.5:5000", id, nonce, cookie, now) {
		t.Fatal("cookie endpoint binding failed")
	}
}
