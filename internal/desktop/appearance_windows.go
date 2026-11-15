//go:build windows

package desktop

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func windowsLightTheme() bool {
	var key windows.Handle
	if e := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, wide(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`), 0, windows.KEY_QUERY_VALUE, &key); e != nil {
		return true
	}
	defer windows.RegCloseKey(key)
	var value, kind uint32
	size := uint32(4)
	e := windows.RegQueryValueEx(key, wide("AppsUseLightTheme"), nil, &kind, (*byte)(unsafe.Pointer(&value)), &size)
	return e != nil || kind != windows.REG_DWORD || value != 0
}
func (u *ui) roundWindow(h uintptr) {
	var r rect
	call("GetClientRect", h, uintptr(unsafe.Pointer(&r)))
	region, _, _ := gdi.NewProc("CreateRoundRectRgn").Call(0, 0, uintptr(r.Right+1), uintptr(r.Bottom+1), uintptr(u.s(16)), uintptr(u.s(16)))
	if region != 0 && call("SetWindowRgn", h, region, 1) == 0 {
		gdi.NewProc("DeleteObject").Call(region)
	}
}
func (u *ui) openBrowser(address string) {
	result, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW").Call(u.window, uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(address))), 0, 0, 1)
	if result <= 32 {
		u.showMessage(tr("Браузер"), tr("Не удалось открыть браузер"))
	}
}

func (u *ui) badge() (string, string) {
	label, kind := connectionBadge(u.engine != nil || (u.preview && u.wanted), u.wanted || u.busy && u.api == nil, u.lastError != "")
	if kind == "connecting" && u.reconnecting {
		return tr("Переподключение…"), kind
	}
	return label, kind
}

func (u *ui) retryConnection() {
	cfg := u.store.Snapshot()
	if cfg.Server == "" {
		u.openDialog("server", true)
		return
	}
	id := u.activeID
	if id == "" {
		id = cfg.NetworkID
	}
	if id == "" {
		id = u.selected().NetworkID
	}
	u.disconnect(false)
	if u.api == nil {
		u.attach(cfg.Server, cfg.DeviceName)
	} else if id != "" {
		u.connect(id)
	}
}

func (u *ui) dialogBounds(width, height, offset int) (int, int) {
	var parent rect
	call("GetWindowRect", u.window, uintptr(unsafe.Pointer(&parent)))
	x, y := int(parent.Left)+(int(parent.Right-parent.Left)-u.s(width))/2, int(parent.Top)+u.s(offset)
	monitor := call("MonitorFromWindow", u.window, 2)
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{Size: 40}
	if call("GetMonitorInfoW", monitor, uintptr(unsafe.Pointer(&info))) != 0 {
		x = max(int(info.Work.Left), min(x, int(info.Work.Right)-u.s(width)))
		y = max(int(info.Work.Top), min(y, int(info.Work.Bottom)-u.s(height)))
	}
	return x, y
}
