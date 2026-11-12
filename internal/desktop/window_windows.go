//go:build windows

package desktop

import (
	"golang.org/x/sys/windows"
	"linklan/internal/tunnel"
	"unsafe"
)

func (u *ui) makeFont(height, weight int) uintptr {
	f, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(int64(-u.s(height))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(wide("Segoe UI"))))
	return f
}
func (u *ui) refreshFonts() {
	old := []uintptr{u.font, u.boldFont, u.networkFont, u.smallFont, u.ipFont, u.menuFont}
	u.font = u.makeFont(13, 400)
	u.boldFont = u.makeFont(16, 600)
	u.networkFont = u.makeFont(13, 600)
	u.smallFont = u.makeFont(12, 400)
	u.ipFont = u.makeFont(14, 600)
	u.menuFont = u.makeFont(12, 400)
	for id, h := range u.controls {
		font := u.font
		if id == 1004 {
			font = u.boldFont
		} else if id == 1005 {
			font = u.ipFont
		} else if id == 1006 || id == 999 {
			font = u.smallFont
		} else if id >= 500 && id <= 502 {
			font = u.menuFont
		}
		send(h, 0x30, font, 1)
	}
	if u.dialog != nil {
		for _, h := range u.dialog.Controls {
			send(h, 0x30, u.font, 1)
		}
	}
	for _, f := range old {
		if f != 0 {
			gdi.NewProc("DeleteObject").Call(f)
		}
	}
	if u.controls[3000] != 0 {
		u.signalIcons()
	}
}
func (u *ui) applyDPI(dpi int, suggested *rect) {
	if dpi < 96 || dpi > 480 {
		return
	}
	u.dpi = dpi
	u.refreshFonts()
	if suggested != nil {
		call("SetWindowPos", u.window, 0, uintptr(suggested.Left), uintptr(suggested.Top), uintptr(suggested.Right-suggested.Left), uintptr(suggested.Bottom-suggested.Top), 0x14)
	}
	u.layout()
	call("RedrawWindow", u.window, 0, 0, 0x185)
}
func (u *ui) initialBounds(cfg tunnel.Config) (int, int, int, int) {
	w, h := compactTheme.Width, compactTheme.Height
	if cfg.Window.Width >= compactTheme.MinWidth && cfg.Window.Width <= 5000 {
		w = cfg.Window.Width
	}
	if cfg.Window.Height >= compactTheme.MinHeight && cfg.Window.Height <= 5000 {
		h = cfg.Window.Height
	}
	x, y := u.s(40), u.s(40)
	if cfg.Window.Positioned {
		x, y = cfg.Window.X, cfg.Window.Y
	}
	r := rect{int32(x), int32(y), int32(x + u.s(w)), int32(y + u.s(h))}
	monitor := call("MonitorFromRect", uintptr(unsafe.Pointer(&r)), 2)
	info := struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}{Size: 40}
	if call("GetMonitorInfoW", monitor, uintptr(unsafe.Pointer(&info))) != 0 {
		width, height := min(u.s(w), int(info.Work.Right-info.Work.Left)), min(u.s(h), int(info.Work.Bottom-info.Work.Top))
		x = max(int(info.Work.Left), min(x, int(info.Work.Right)-width))
		y = max(int(info.Work.Top), min(y, int(info.Work.Bottom)-height))
		return x, y, width, height
	}
	return x, y, u.s(w), u.s(h)
}
func (u *ui) saveWindowState() {
	if u.preview {
		return
	}
	expanded := u.store.Snapshot().ExpandedNetworks
	if expanded == nil {
		expanded = map[string]bool{}
	}
	for item, e := range u.items {
		if e.PeerID == "" {
			v := treeItem{Mask: 8, Handle: item, StateMask: 0x20}
			send(u.controls[3000], 0x113e, 0, uintptr(unsafe.Pointer(&v)))
			expanded[e.NetworkID] = v.State&0x20 != 0
		}
	}
	var r rect
	valid := call("IsWindow", u.window) != 0 && call("IsIconic", u.window) == 0 && call("GetWindowRect", u.window, uintptr(unsafe.Pointer(&r))) != 0
	e := u.store.Update(func(c *tunnel.Config) {
		if valid && r.Right > r.Left && r.Bottom > r.Top {
			c.Window = tunnel.WindowState{X: int(r.Left), Y: int(r.Top), Width: logicalPixel(int(r.Right-r.Left), u.dpi), Height: logicalPixel(int(r.Bottom-r.Top), u.dpi), Positioned: true}
		}
		c.ExpandedNetworks = expanded
	})
	if e != nil {
		u.lastError = "Не удалось сохранить расположение окна: " + e.Error()
	}
}
func (u *ui) scheduleWindowSave() {
	if !u.preview {
		call("SetTimer", u.window, 3, 700, 0)
	}
}
func (u *ui) startAutoloadSync() {
	if u.preview {
		return
	}
	cfg := u.store.Snapshot()
	enable := cfg.StartAtLogon == nil || *cfg.StartAtLogon
	u.startupWait.Add(1)
	go func() {
		defer u.startupWait.Done()
		err := ApplyAutostart(u.store, enable)
		u.post(func() {
			if err != nil {
				u.lastError = "Автозагрузка: " + err.Error()
			}
			u.header()
		})
	}()
}

// A second launch must restore the existing interactive window, not start a second VPN.
func acquireClientMutex() (windows.Handle, bool, error) {
	_, sid, e := startupIdentity()
	if e != nil {
		return 0, false, e
	}
	name, _ := windows.UTF16PtrFromString(`Local\LinkLAN-` + sid)
	handle, e := windows.CreateMutex(nil, false, name)
	if e == windows.ERROR_ALREADY_EXISTS {
		return handle, false, nil
	}
	return handle, e == nil, e
}
