//go:build windows

package desktop

import (
	"fmt"
	"golang.org/x/sys/windows"
	"linklan/internal/model"
	"time"
	"unsafe"
)

func (u *ui) desktopNotification(key, title, body string) {
	if u.preview || !u.trayAdded {
		return
	}
	cfg := u.store.Snapshot()
	if !boolSetting(cfg.NotifyDesktop, true) {
		return
	}
	now := time.Now()
	if len(u.notifyTimes) > 2048 {
		for key, at := range u.notifyTimes {
			if now.Sub(at) > time.Hour {
				delete(u.notifyTimes, key)
			}
		}
	}
	if u.notifyTimes == nil {
		u.notifyTimes = map[string]time.Time{}
	}
	if now.Sub(u.notifyTimes[key]) < time.Minute || now.Sub(u.lastNotification) < 10*time.Second {
		return
	}
	if boolSetting(cfg.RespectFocus, true) {
		var state uint32
		proc := windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")
		if proc.Find() == nil {
			result, _, _ := proc.Call(uintptr(unsafe.Pointer(&state)))
			if result == 0 && state != 5 {
				return
			}
		}
	}
	n := u.trayData()
	n.Flags = 16 | 0x40
	n.InfoFlags = 1
	if !boolSetting(cfg.NotifySound, true) {
		n.InfoFlags |= 0x10
	}
	if boolSetting(cfg.RespectFocus, true) {
		n.InfoFlags |= 0x80
	}
	copy(n.InfoTitle[:63], windows.StringToUTF16(title))
	copy(n.Info[:255], windows.StringToUTF16(body))
	ok, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&n)))
	if ok != 0 {
		u.notifyTimes[key] = now
		u.lastNotification = now
	}
}
func (u *ui) connectionNotification() {
	state := "offline"
	if u.engine != nil {
		state = "online"
	} else if u.wanted {
		state = "connecting"
	}
	switch u.notifyState.observe(state, time.Now()) {
	case "online":
		u.desktopNotification("vpn-online", tr("LinkLAN: подключено"), tr("VPN-соединение установлено"))
	case "offline":
		u.desktopNotification("vpn-offline", tr("LinkLAN: соединение закрыто"), tr("VPN-соединение отключено или потеряно"))
	}
}
func (u *ui) peerNotifications(old, next []model.NetworkView) {
	previous := map[string]bool{}
	for _, v := range old {
		for _, p := range v.Peers {
			previous[v.Network.ID+"/"+p.ID] = p.Online
		}
	}
	for _, v := range next {
		for _, p := range v.Peers {
			key := v.Network.ID + "/" + p.ID
			before, known := previous[key]
			if !known || before == p.Online || p.ID == u.deviceID() {
				continue
			}
			status := tr("Компьютер отключился")
			if p.Online {
				status = tr("Компьютер подключился")
			}
			u.desktopNotification(fmt.Sprintf("peer-%s-%t", key, p.Online), "LinkLAN", p.Name+" · "+v.Network.Name+"\n"+status)
		}
	}
}
