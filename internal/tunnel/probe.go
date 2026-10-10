package tunnel

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Only the common Address, Status and RoundTripTime prefix is read; pointer
// fields differ between ICMP_ECHO_REPLY and ICMP_ECHO_REPLY32 on Windows.
func echoReply(reply []byte, destination net.IP) (time.Duration, error) {
	if len(reply) < 12 {
		return 0, fmt.Errorf("Неполный ответ ICMP")
	}
	if status := binary.LittleEndian.Uint32(reply[4:8]); status != 0 {
		return 0, fmt.Errorf("Нет ответа ICMP (код %d)", status)
	}
	if !net.IP(reply[:4]).Equal(destination) {
		return 0, fmt.Errorf("Ответ ICMP от другого адреса")
	}
	return time.Duration(binary.LittleEndian.Uint32(reply[8:12])) * time.Millisecond, nil
}
