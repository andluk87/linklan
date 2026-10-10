package updates

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSignedManifest(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	m := Manifest{Version: "0.2.0", URL: "/downloads/client.zip", SHA256: strings.Repeat("a", 64), Size: 100}
	m.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(private, m.Message()))
	p := base64.RawStdEncoding.EncodeToString(public)
	if e := m.Verify(p); e != nil {
		t.Fatal(e)
	}
	m.Size++
	if e := m.Verify(p); e == nil {
		t.Fatal("altered size accepted")
	}
	if e := m.Verify(""); e == nil {
		t.Fatal("missing pinned key accepted")
	}
}
func TestNoDowngrade(t *testing.T) {
	for _, s := range []string{"0.1.0", "0.0.9", "garbage", "0.2.0-rc1", "-1.0.0", "+1.0.0", "01.0.0", "1.00.0", "1..0", "1.0.0/../../other"} {
		if Newer(s, "0.1.0") {
			t.Fatalf("accepted %s", s)
		}
	}
	if !Newer("0.2.0", "0.1.0") || !Newer("1.0.0", "0.9.99") {
		t.Fatal("new version rejected")
	}
}
