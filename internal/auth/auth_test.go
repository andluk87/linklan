package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, e := Password("a very long password")
	if e != nil || !VerifyPassword(h, "a very long password") || VerifyPassword(h, "wrong") {
		t.Fatal("Argon2id verification failed", e)
	}
	if _, e = Password("short"); e == nil {
		t.Fatal("weak password accepted")
	}
	if VerifyPassword(strings.Replace(h, "m=65536", "m=999999999", 1), "a very long password") {
		t.Fatal("unbounded parameters accepted")
	}
}
func TestTokens(t *testing.T) {
	now := time.Unix(1700000000, 0)
	secret := []byte("01234567890123456789012345678901")
	token := Access(secret, "user-id", "session-id", now)
	c, e := Validate(secret, token, now)
	if e != nil || c.Subject != "user-id" || c.Session != "session-id" {
		t.Fatal(e)
	}
	if _, e = Validate(secret, token, now.Add(16*time.Minute)); e == nil {
		t.Fatal("expired token accepted")
	}
	if _, e = Validate([]byte("wrong"), token, now); e == nil {
		t.Fatal("bad signature accepted")
	}
	p := strings.Split(token, ".")
	p[0] = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"
	if _, e = Validate(secret, strings.Join(p, "."), now); e == nil {
		t.Fatal("alg none accepted")
	}
}
