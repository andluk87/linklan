//go:build windows

package desktop

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const trayCallback = 0x8020
const restoreMessageName = "LinkLAN.Restore"
const quitMessageName = "LinkLAN.Quit"

type notifyIconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

func registered(name string) uint32 {
	return uint32(call("RegisterWindowMessageW", uintptr(unsafe.Pointer(wide(name)))))
}
func existingClient() uintptr {
	return call("FindWindowW", uintptr(unsafe.Pointer(wide("LinkLANClientV2"))), 0)
}
func RestoreExisting(hidden bool) bool {
	h := existingClient()
	if h == 0 || call("GetPropW", h, uintptr(unsafe.Pointer(wide("LinkLANClosing")))) != 0 {
		return false
	}
	if !hidden {
		var pid uint32
		call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&pid)))
		call("AllowSetForegroundWindow", uintptr(pid))
		call("PostMessageW", h, uintptr(registered(restoreMessageName)), 0, 0)
	}
	return true
}
func QuitExisting() error {
	for count := 0; count < 5; count++ {
		h := existingClient()
		if h == 0 {
			return nil
		}
		var pid uint32
		call("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&pid)))
		process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
		if err != nil {
			if call("IsWindow", h) == 0 {
				continue
			}
			return err
		}
		call("PostMessageW", h, uintptr(registered(quitMessageName)), 0, 0)
		call("PostMessageW", h, 0x111, 106, 0)
		status, err := windows.WaitForSingleObject(process, 20000)
		windows.CloseHandle(process)
		if err != nil {
			return err
		}
		if status != windows.WAIT_OBJECT_0 {
			return fmt.Errorf("LinkLAN ещё отключает VPN; дождитесь завершения и повторите")
		}

	}
	return fmt.Errorf("Обнаружено несколько запущенных экземпляров LinkLAN")
}
func (u *ui) trayData() notifyIconData {
	n := notifyIconData{Window: u.window, ID: 1, Flags: 1 | 2 | 4, Callback: trayCallback, Icon: u.smallIcon}
	n.Size = uint32(unsafe.Sizeof(n))
	label, _ := connectionBadge(u.engine != nil, u.wanted, u.lastError != "")
	tip := "LinkLAN · " + label
	if u.lastError != "" {
		tip += " · " + u.lastError
	}
	t := windows.StringToUTF16(tip)
	copy(n.Tip[:127], t)
	return n
}
func (u *ui) addTray() bool {
	n := u.trayData()
	ok, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW").Call(0, uintptr(unsafe.Pointer(&n)))
	u.trayAdded = ok != 0
	return u.trayAdded
}
func (u *ui) updateTray() {
	if !u.trayAdded {
		return
	}
	n := u.trayData()
	n.Flags = 4
	windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&n)))
}
func (u *ui) removeTray() {
	if !u.trayAdded {
		return
	}
	n := u.trayData()
	windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW").Call(2, uintptr(unsafe.Pointer(&n)))
	u.trayAdded = false
}
func (u *ui) hideToTray() bool {
	if !u.trayAdded && !u.addTray() {
		return false
	}
	u.saveWindowState()
	if u.dialog != nil {
		call("ShowWindow", u.dialog.Window, 0)
	}
	call("ShowWindow", u.window, 0)
	u.hidden = true
	return true
}
func (u *ui) showFromTray() {
	call("ShowWindow", u.window, 9)
	call("SetForegroundWindow", u.window)
	u.hidden = false
	if u.dialog != nil {
		call("ShowWindow", u.dialog.Window, 1)
		call("SetForegroundWindow", u.dialog.Window)
	}
}
func (u *ui) trayMenu() {
	var p point
	call("GetCursorPos", uintptr(unsafe.Pointer(&p)))
	menu := u.newMenu()
	defer call("DestroyMenu", menu)
	u.menuItem(menu, 520, "Открыть LinkLAN")
	if u.wanted {
		u.menuItem(menu, 205, "Отключить VPN")
	} else {
		u.menuItem(menu, 100, "Подключить VPN")
	}
	u.menuItem(menu, 101, "Настройки…")
	u.menuSeparator(menu)
	u.menuItem(menu, 106, "Выход")
	call("SetForegroundWindow", u.window)
	selected := call("TrackPopupMenu", menu, 0x102, uintptr(p.X), uintptr(p.Y), 0, u.window, 0)
	if selected != 0 {
		if selected == 101 {
			u.showFromTray()
		}
		u.action(int(selected))
	}
	call("PostMessageW", u.window, 0, 0, 0)
}
func (u *ui) trayEvent(event uintptr) {
	switch event & 0xffff {
	case 0x202, 0x203, 0x400, 0x401:
		u.showFromTray()
	case 0x205, 0x7b:
		u.trayMenu()
	}
}
