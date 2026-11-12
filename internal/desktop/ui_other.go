//go:build !windows

package desktop

import (
	"fmt"
	"linklan/internal/tunnel"
)

func EnsureAdmin() (bool, error)    { return false, fmt.Errorf("Windows only") }
func Run(store *tunnel.Store) error { return fmt.Errorf("Windows 10/11 x64 only") }

func Preview(store *tunnel.Store) error { return fmt.Errorf("Windows UI preview only") }

func CheckIcon() error { return nil }

func RunWithOptions(store *tunnel.Store, options Options) error { return Run(store) }
func RestoreExisting(hidden bool) bool                          { return false }
func QuitExisting() error                                       { return fmt.Errorf("Windows only") }
func ConfigureAutostart(enable bool) error                      { return fmt.Errorf("Windows only") }
func StartupEnabled() (bool, error)                             { return false, fmt.Errorf("Windows only") }

func ApplyAutostart(store *tunnel.Store, enable bool) error { return fmt.Errorf("Windows only") }
