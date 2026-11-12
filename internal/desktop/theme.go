package desktop

import (
	"fmt"
	"time"

	"linklan/internal/model"
)

// One source of truth for logical (96-DPI) dimensions used by the Win32 view.
var compactTheme = struct {
	Width, Height, MinWidth, MinHeight                              int
	Title, Menu, Padding, CardTop, CardHeight, ListTop, Row, Radius int
}{360, 680, 320, 400, 32, 28, 10, 64, 80, 150, 22, 8}

func scalePixel(v, dpi int) int   { return v * dpi / 96 }
func logicalPixel(v, dpi int) int { return (v*96 + dpi/2) / dpi }

func connectionBadge(connected, wanted, failed bool) (string, string) {
	if connected {
		return "В сети", "online"
	}
	if failed {
		return "Ошибка", "error"
	}
	if wanted {
		return "Подключение…", "connecting"
	}
	return "Не в сети", "offline"
}

func signalDescription(online, active bool, state echoState) string {
	if !online {
		return "Компьютер не в сети"
	}
	if !active {
		return "Сеть отключена; задержка не измерена"
	}
	if !state.Measured {
		return "В сети; проверка ICMP ещё не выполнена"
	}
	if !state.Reachable {
		return "Нет ответа ICMP; соединение не подтверждено"
	}
	if state.RTT < time.Millisecond {
		return "В сети · <1 мс"
	}
	return fmt.Sprintf("В сети · %d мс", state.RTT.Milliseconds())
}

// Authentication/session fields must refresh, but do not require rebuilding a tree.
func sameNetworkLayout(a, b []model.NetworkView) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Network != b[i].Network || len(a[i].Peers) != len(b[i].Peers) {
			return false
		}
		for j, p := range a[i].Peers {
			q := b[i].Peers[j]
			if p.ID != q.ID || p.UserID != q.UserID || p.Name != q.Name || p.IP != q.IP || p.Status != q.Status || p.Online != q.Online {
				return false
			}
		}
	}
	return true
}

type Options struct {
	StartHidden, Preview, LargePreview bool
	PreviewDPI                         int
}

func themeColor(hex uint32) uint32 { return hex>>16&255 | hex&0xff00 | (hex&255)<<16 }

var compactColors = struct{ Background, Panel, Selection, Hover, Accent, Online, Text, Muted, Good, Warning, Bad, Offline, Connecting, Error uint32 }{
	themeColor(0x0E1621), themeColor(0x252E3D), themeColor(0x2B5278), themeColor(0x2F3F4E), themeColor(0x29B7CE), themeColor(0x348FA4), themeColor(0xFFFFFF), themeColor(0x9AAFC0), themeColor(0x00BD22), themeColor(0xE5BD39), themeColor(0xE6694C), themeColor(0x5B6A78), themeColor(0x377787), themeColor(0xB4434B),
}
