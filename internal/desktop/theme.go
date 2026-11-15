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
}{440, 500, 250, 350, 32, 30, 10, 66, 80, 152, 22, 8}

func scalePixel(v, dpi int) int   { return v * dpi / 96 }
func logicalPixel(v, dpi int) int { return (v*96 + dpi/2) / dpi }

func connectionBadge(connected, wanted, failed bool) (string, string) {
	if connected {
		return tr("В сети"), "online"
	}
	if failed {
		return tr("Ошибка подключения"), "error"
	}
	if wanted {
		return tr("Подключение…"), "connecting"
	}
	return tr("Не в сети"), "offline"
}

func signalDescription(online, active bool, state echoState) string {
	if !online {
		return tr("Компьютер не в сети")
	}
	if !active {
		return tr("Сеть отключена; задержка не измерена")
	}
	if !state.Measured {
		return tr("В сети; проверка ICMP ещё не выполнена")
	}
	if !state.Reachable {
		return tr("Нет ответа ICMP; соединение не подтверждено")
	}
	if state.RTT < time.Millisecond {
		return tr("В сети · <1 мс")
	}
	return fmt.Sprintf(tr("В сети · %d мс"), state.RTT.Milliseconds())
}

// Authentication/session fields must refresh, but do not require rebuilding a tree.
func sameNetworkLayout(a, b []model.NetworkView) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Network.ID != b[i].Network.ID || len(a[i].Peers) != len(b[i].Peers) {
			return false
		}
		for j, p := range a[i].Peers {
			q := b[i].Peers[j]
			if p.ID != q.ID {
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

type palette struct{ Background, Panel, Field, Text, Muted, Accent, Online, Selection, Inactive, Hover, Menu, Name, Offline uint32 }

func themePalette(light bool) palette {
	if light {
		return palette{Background: themeColor(0xD7E8F2), Panel: themeColor(0xFFFFFF), Field: themeColor(0xFFFFFF), Text: themeColor(0x243F50), Muted: themeColor(0x627786), Accent: themeColor(0x43CBD5), Online: themeColor(0x23C52B), Selection: themeColor(0xB8DDF6), Inactive: themeColor(0xD5D5D5), Hover: themeColor(0xD6EBF8), Menu: themeColor(0xFAFAFA), Name: themeColor(0x36A3B5), Offline: themeColor(0xC3C9CE)}
	}
	return palette{Background: compactColors.Background, Panel: compactColors.Panel, Field: themeColor(0x192633), Text: compactColors.Text, Muted: compactColors.Muted, Accent: compactColors.Accent, Online: compactColors.Good, Selection: compactColors.Selection, Inactive: themeColor(0x465363), Hover: compactColors.Hover, Menu: themeColor(0x293C4D), Name: compactColors.Accent, Offline: compactColors.Offline}
}
func boolSetting(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// Reserve the full IPv4 column first; only the computer name may shrink.
func peerColumns(width, dpi int) (int, int) {
	right := width - scalePixel(13, dpi)
	return right - scalePixel(112, dpi), right
}
