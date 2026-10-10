package tunnel

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestEchoReplyRequiresSuccessAndExpectedPeer(t *testing.T) {
	ip := net.ParseIP("10.32.1.3").To4()
	reply := make([]byte, 512)
	copy(reply, ip)
	binary.LittleEndian.PutUint32(reply[8:12], 42)
	if rtt, err := echoReply(reply, ip); err != nil || rtt != 42*time.Millisecond {
		t.Fatal(rtt, err)
	}
	binary.LittleEndian.PutUint32(reply[4:8], 11010)
	if _, err := echoReply(reply, ip); err == nil {
		t.Fatal("timeout status treated as reachable")
	}
	binary.LittleEndian.PutUint32(reply[4:8], 0)
	if _, err := echoReply(reply, net.ParseIP("10.32.1.4")); err == nil {
		t.Fatal("different peer accepted")
	}
	if _, err := echoReply(reply[:8], ip); err == nil {
		t.Fatal("truncated reply accepted")
	}
}
