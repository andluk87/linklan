package backend

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"linklan/internal/auth"
	"linklan/internal/model"
)

func TestDeviceAccessProofRejectsReplayAndWrongKey(t *testing.T) {
	m := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer rc.Close()
	s := &Server{redis: rc}
	ctx := context.Background()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	b := deviceAccessRequest{Name: "PC", PublicKey: base64.RawStdEncoding.EncodeToString(pub), Version: model.Version, Challenge: auth.Random(32)}
	sign := func() {
		b.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, []byte(model.DeviceProof(b.Challenge, b.PublicKey, b.Name, b.Version))))
	}
	remember := func() { rc.Set(ctx, "device-access:"+auth.HashToken(b.Challenge), b.PublicKey, 30*time.Second) }
	sign()
	remember()
	tampered := b
	tampered.Name = "Another PC"
	if s.verifyDeviceAccess(ctx, tampered) == nil {
		t.Fatal("changed name accepted")
	}
	if e := s.verifyDeviceAccess(ctx, b); e != nil {
		t.Fatal("valid proof rejected", e)
	}
	if s.verifyDeviceAccess(ctx, b) == nil {
		t.Fatal("proof replay accepted")
	}
	b.Challenge = auth.Random(32)
	sign()
	remember()
	m.FastForward(31 * time.Second)
	if s.verifyDeviceAccess(ctx, b) == nil {
		t.Fatal("expired proof accepted")
	}
	b.Challenge = auth.Random(32)
	sign()
	rc.Set(ctx, "device-access:"+auth.HashToken(b.Challenge), "different-key", 30*time.Second)
	if s.verifyDeviceAccess(ctx, b) == nil {
		t.Fatal("challenge for another key accepted")
	}
}
