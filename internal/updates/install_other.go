//go:build !windows

package updates

import "fmt"

func Install(staged string) error { return fmt.Errorf("Windows only") }
