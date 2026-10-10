package stun

import (
	"net"
	"testing"
)

func TestBinding(t *testing.T) {
	q, e := Request()
	if e != nil {
		t.Fatal(e)
	}
	endpoint := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 43210}
	r, e := Response(q, endpoint)
	if e != nil {
		t.Fatal(e)
	}
	got, e := ParseResponse(r, q[8:20])
	if e != nil || got.String() != endpoint.String() {
		t.Fatal("mapping mismatch", got, e)
	}
	wrong := append([]byte{}, q[8:20]...)
	wrong[0] ^= 1
	if _, e = ParseResponse(r, wrong); e == nil {
		t.Fatal("wrong transaction accepted")
	}
	if _, e = ParseResponse(r[:31], q[8:20]); e == nil {
		t.Fatal("truncated response accepted")
	}
}
func FuzzSTUN(f *testing.F) {
	f.Add(make([]byte, 32))
	f.Fuzz(func(t *testing.T, b []byte) {
		Is(b)
		Response(b, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234})
		ParseResponse(b, make([]byte, 12))
	})
}
