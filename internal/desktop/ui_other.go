//go:build !windows

package desktop

import (
	"fmt"
	"linklan/internal/tunnel"
)

func EnsureAdmin() (bool, error)    { return false, fmt.Errorf("Windows only") }
func Run(store *tunnel.Store) error { return fmt.Errorf("Windows 10/11 x64 only") }
