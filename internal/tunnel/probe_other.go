//go:build !windows

package tunnel

import (
	"context"
	"fmt"
	"linklan/internal/model"
	"time"
)

func PingIPv4(context.Context, string, string, time.Duration) (time.Duration, error) {
	return 0, fmt.Errorf("ICMP client requires Windows")
}
func EnsureClientFirewall(context.Context) error { return fmt.Errorf("Windows only") }
func NewAdapterWithFirewall(n model.Network, allowAll bool) (Adapter, error) {
	return nil, fmt.Errorf("Windows only")
}
