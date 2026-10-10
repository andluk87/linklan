//go:build !windows

package tunnel

import (
	"fmt"
	"linklan/internal/model"
)

func NewAdapter(n model.Network) (Adapter, error) {
	return nil, fmt.Errorf("Клиент поддерживает Windows 10/11 x64")
}
func LoadStore() (*Store, error) { return nil, fmt.Errorf("DPAPI доступен на Windows") }
