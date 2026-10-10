package protocol

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	mathrand "math/rand"
	"testing"
)

func pair(t *testing.T) (*Channel, *Channel) {
	t.Helper()
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	netID, _ := ParseID("10000000-0000-4000-8000-000000000001")
	aid, _ := ParseID("10000000-0000-4000-8000-000000000002")
	bid, _ := ParseID("10000000-0000-4000-8000-000000000003")
	ac, e := NewChannel(netID, aid, bid, a, b.PublicKey().Bytes())
	if e != nil {
		t.Fatal(e)
	}
	bc, e := NewChannel(netID, bid, aid, b, a.PublicKey().Bytes())
	if e != nil {
		t.Fatal(e)
	}
	return ac, bc
}
func TestEncryptedDatagram(t *testing.T) {
	a, b := pair(t)
	packet := []byte("private bytes")
	frame, e := a.Seal(Data, packet)
	if e != nil {
		t.Fatal(e)
	}
	h, p, e := b.Open(frame)
	if e != nil || string(p) != string(packet) || h.Kind != Data {
		t.Fatal("roundtrip failed", e)
	}
	if _, _, e = b.Open(frame); e == nil {
		t.Fatal("replay accepted")
	}
	back, _ := b.Seal(ProbeAck, []byte("ack"))
	if _, p, e = a.Open(back); e != nil || string(p) != "ack" {
		t.Fatal("directional key failed", e)
	}
}
func TestUnauthenticatedPacketDoesNotConsumeSequence(t *testing.T) {
	a, b := pair(t)
	good, _ := a.Seal(Data, []byte("real"))
	fake := append([]byte{}, good...)
	fake[len(fake)-1] ^= 1
	if _, _, e := b.Open(fake); e == nil {
		t.Fatal("bad tag accepted")
	}
	if _, _, e := b.Open(good); e != nil {
		t.Fatal("forgery advanced replay state", e)
	}
}
func TestHeaderAndSessionAuthenticated(t *testing.T) {
	for _, offset := range []int{5, 8, 24, 40, 56, 64, 71} {
		a, b := pair(t)
		good, _ := a.Seal(Data, []byte("x"))
		bad := append([]byte{}, good...)
		bad[offset] ^= 1
		if _, _, e := b.Open(bad); e == nil {
			t.Fatalf("header byte %d unauthenticated", offset)
		}
	}
	a, b := pair(t)
	old, _ := a.Seal(Data, []byte("x"))
	_, fresh := pair(t)
	if _, _, e := fresh.Open(old); e == nil {
		t.Fatal("old session accepted")
	}
	if _, _, e := b.Open(old); e != nil {
		t.Fatal(e)
	}
}
func TestReplayReordering(t *testing.T) {
	var w ReplayWindow
	for _, n := range []uint64{5, 3, 4, 1, 2000, 1999} {
		if !w.Accept(n) {
			t.Fatalf("valid sequence %d rejected", n)
		}
	}
	for _, n := range []uint64{0, 5, 3, 2000, 1999, 976} {
		if w.Accept(n) {
			t.Fatalf("invalid sequence %d accepted", n)
		}
	}
}
func TestReplayWindowAgainstReference(t *testing.T) {
	r := mathrand.New(mathrand.NewSource(17))
	var w ReplayWindow
	highest := uint64(0)
	seen := map[uint64]bool{}
	for i := 0; i < 12000; i++ {
		seq := uint64(r.Intn(1300)) + uint64(i/10)
		want := seq != 0 && !seen[seq] && (seq > highest || highest-seq < 1024)
		if w.Accept(seq) != want {
			t.Fatalf("seq %d highest %d mismatch", seq, highest)
		}
		if want {
			seen[seq] = true
			if seq > highest {
				highest = seq
			}
		}
	}
}
func TestMTUAndIPv4SpoofBoundaries(t *testing.T) {
	a, _ := pair(t)
	if _, e := a.Seal(Data, make([]byte, MTU+1)); e == nil {
		t.Fatal("MTU not enforced")
	}
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], 20)
	copy(ip[12:16], []byte{10, 32, 0, 2})
	copy(ip[16:20], []byte{10, 32, 0, 3})
	src, dst, e := IPv4(ip)
	if e != nil || src[3] != 2 || dst[3] != 3 {
		t.Fatal("IP parser")
	}
	ip[0] = 0x4f
	if _, _, e = IPv4(ip); e == nil {
		t.Fatal("short IHL accepted")
	}
}
func TestRelayEnvelope(t *testing.T) {
	a, _ := pair(t)
	frame, _ := a.Seal(Data, []byte("hello"))
	key := make([]byte, 32)
	wrapped := WrapRelay(a.self, 1, key, frame)
	id, seq, p, ok := UnwrapRelay(wrapped, key)
	if !ok || id != a.self || seq != 1 || string(p) != string(frame) {
		t.Fatal("envelope failed")
	}
	wrapped[len(wrapped)-1] ^= 1
	if _, _, _, ok = UnwrapRelay(wrapped, key); ok {
		t.Fatal("tampered relay envelope accepted")
	}
}
func FuzzParse(f *testing.F) {
	f.Add([]byte("LLP1"))
	f.Add(make([]byte, 100))
	f.Fuzz(func(t *testing.T, b []byte) { Parse(b); IPv4(b) })
}
