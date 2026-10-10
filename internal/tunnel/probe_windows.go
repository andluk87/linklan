//go:build windows

package tunnel

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ipHelper = windows.NewLazySystemDLL("iphlpapi.dll")

func PingIPv4(ctx context.Context, source, destination string, timeout time.Duration) (time.Duration, error) {
	from, to := net.ParseIP(source).To4(), net.ParseIP(destination).To4()
	if from == nil || to == nil {
		return 0, fmt.Errorf("Некорректный IPv4 для ICMP")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return 0, context.DeadlineExceeded
	}
	if timeout > 2*time.Second {
		timeout = 2 * time.Second
	}
	h, _, err := ipHelper.NewProc("IcmpCreateFile").Call()
	if h == ^uintptr(0) || h == 0 {
		return 0, fmt.Errorf("Не удалось начать Ping: %v", err)
	}
	defer ipHelper.NewProc("IcmpCloseHandle").Call(h)
	payload := []byte("LinkLAN ICMP availability")
	reply := make([]byte, 512)
	count, _, err := ipHelper.NewProc("IcmpSendEcho2Ex").Call(h, 0, 0, 0,
		uintptr(binary.LittleEndian.Uint32(from)), uintptr(binary.LittleEndian.Uint32(to)),
		uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(max(1, timeout.Milliseconds())))
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if count == 0 {
		return 0, fmt.Errorf("Нет ответа ICMP: %v", err)
	}
	return echoReply(reply, to)
}
