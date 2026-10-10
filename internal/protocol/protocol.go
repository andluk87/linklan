// Package protocol carries authenticated IPv4 datagrams. The coordinator is
// trusted for identity discovery; it never receives private/session keys.
package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	HeaderSize      = 72
	MTU             = 1280
	MaxFrame        = HeaderSize + MTU + 16
	Data       byte = 1
	Probe      byte = 2
	ProbeAck   byte = 3
)

var ErrPacket = errors.New("invalid or unauthenticated packet")

type ID [16]byte

func ParseID(s string) (ID, error) {
	var id ID
	b, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if e != nil || len(b) != 16 {
		return id, fmt.Errorf("invalid UUID")
	}
	copy(id[:], b)
	return id, nil
}
func (id ID) String() string {
	h := hex.EncodeToString(id[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

type Header struct {
	Kind                         byte
	Network, Source, Destination ID
	Sequence                     uint64
	Session                      [8]byte
}

func Parse(b []byte) (Header, error) {
	var h Header
	if len(b) < HeaderSize+16 || len(b) > MaxFrame || string(b[:4]) != "LLP1" || b[4] != 1 {
		return h, ErrPacket
	}
	h.Kind = b[5]
	if h.Kind < Data || h.Kind > ProbeAck || int(binary.BigEndian.Uint16(b[6:8])) != len(b)-HeaderSize {
		return h, ErrPacket
	}
	copy(h.Network[:], b[8:24])
	copy(h.Source[:], b[24:40])
	copy(h.Destination[:], b[40:56])
	h.Sequence = binary.BigEndian.Uint64(b[56:64])
	copy(h.Session[:], b[64:72])
	if h.Sequence == 0 {
		return h, ErrPacket
	}
	return h, nil
}

// ReplayWindow allows bounded reordering. Call Accept only after AEAD verification.
type ReplayWindow struct {
	highest uint64
	bits    [16]uint64
}

func (w *ReplayWindow) Accept(seq uint64) bool {
	if seq == 0 {
		return false
	}
	if seq > w.highest {
		d := seq - w.highest
		if d >= 1024 {
			w.bits = [16]uint64{}
		} else {
			for i := 1023; i >= 0; i-- {
				v := false
				if uint64(i) >= d {
					j := uint64(i) - d
					v = w.bits[j/64]&(uint64(1)<<(j%64)) != 0
				}
				if v {
					w.bits[i/64] |= uint64(1) << uint(i%64)
				} else {
					w.bits[i/64] &^= uint64(1) << uint(i%64)
				}
			}
		}
		w.highest = seq
	}
	d := w.highest - seq
	if d >= 1024 {
		return false
	}
	bit := uint64(1) << (d % 64)
	if w.bits[d/64]&bit != 0 {
		return false
	}
	w.bits[d/64] |= bit
	return true
}

type Channel struct {
	mu                    sync.Mutex
	network, self, remote ID
	send, receive         cipher.AEAD
	session               [8]byte
	sequence              uint64
	replay                ReplayWindow
}

// A fresh X25519 private key MUST be generated for every new client session.
// The transcript includes both ephemeral public keys, network, peer IDs and
// direction. Restarting counters with the same session private key is forbidden.
func NewChannel(network, self, remote ID, private *ecdh.PrivateKey, remotePublic []byte) (*Channel, error) {
	if self == remote || len(remotePublic) != 32 {
		return nil, ErrPacket
	}
	pub, e := ecdh.X25519().NewPublicKey(remotePublic)
	if e != nil {
		return nil, e
	}
	shared, e := private.ECDH(pub)
	if e != nil {
		return nil, e
	}
	low, high := self, remote
	loPub, hiPub := private.PublicKey().Bytes(), remotePublic
	if strings.Compare(string(self[:]), string(remote[:])) > 0 {
		low, high = remote, self
		loPub, hiPub = remotePublic, private.PublicKey().Bytes()
	}
	transcript := append([]byte("LinkLAN/1/session"), network[:]...)
	transcript = append(transcript, low[:]...)
	transcript = append(transcript, high[:]...)
	transcript = append(transcript, loPub...)
	transcript = append(transcript, hiPub...)
	salt := sha256.Sum256(transcript)
	material, e := hkdf.Key(sha256.New, shared, salt[:], "LinkLAN/1/AES-256-GCM", 64)
	if e != nil {
		return nil, e
	}
	first, e := aes.NewCipher(material[:32])
	if e != nil {
		return nil, e
	}
	second, e := aes.NewCipher(material[32:])
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(first)
	if e != nil {
		return nil, e
	}
	b, e := cipher.NewGCM(second)
	if e != nil {
		return nil, e
	}
	c := &Channel{network: network, self: self, remote: remote, send: a, receive: b}
	copy(c.session[:], salt[:8])
	if self != low {
		c.send, c.receive = b, a
	}
	return c, nil
}
func (c *Channel) Seal(kind byte, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(payload) > MTU || kind < Data || kind > ProbeAck || c.sequence >= 1<<32 {
		return nil, ErrPacket
	}
	c.sequence++
	h := make([]byte, HeaderSize, HeaderSize+len(payload)+16)
	copy(h, "LLP1")
	h[4] = 1
	h[5] = kind
	binary.BigEndian.PutUint16(h[6:8], uint16(len(payload)+16))
	copy(h[8:24], c.network[:])
	copy(h[24:40], c.self[:])
	copy(h[40:56], c.remote[:])
	binary.BigEndian.PutUint64(h[56:64], c.sequence)
	copy(h[64:72], c.session[:])
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], c.sequence)
	return c.send.Seal(h, nonce[:], payload, h), nil
}
func (c *Channel) Open(frame []byte) (Header, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, e := Parse(frame)
	if e != nil {
		return h, nil, e
	}
	if h.Network != c.network || h.Source != c.remote || h.Destination != c.self || h.Session != c.session {
		return h, nil, ErrPacket
	}
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], h.Sequence)
	payload, e := c.receive.Open(nil, nonce[:], frame[HeaderSize:], frame[:HeaderSize])
	if e != nil {
		return h, nil, ErrPacket
	}
	if !c.replay.Accept(h.Sequence) {
		return h, nil, ErrPacket
	}
	return h, payload, nil
}

// IPv4 validates lengths and disallows fragments at the tunnel boundary only
// when their source/destination would be invalid; OS fragmentation is supported.
func IPv4(packet []byte) (source, destination [4]byte, err error) {
	if len(packet) < 20 || len(packet) > MTU || packet[0]>>4 != 4 {
		return source, destination, ErrPacket
	}
	ihl := int(packet[0]&15) * 4
	if ihl < 20 || ihl > len(packet) || int(binary.BigEndian.Uint16(packet[2:4])) != len(packet) {
		return source, destination, ErrPacket
	}
	copy(source[:], packet[12:16])
	copy(destination[:], packet[16:20])
	return source, destination, nil
}
