//go:build windows

package desktop

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"linklan/internal/model"
	"linklan/internal/tunnel"
	"linklan/internal/updates"
)

type rect struct{ Left, Top, Right, Bottom int32 }
type minmax struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point }
type nmHeader struct {
	Window, ID uintptr
	Code       uint32
}
type treeItem struct {
	Mask                                    uint32
	Handle                                  uintptr
	State, StateMask                        uint32
	Text                                    *uint16
	TextMax, Image, SelectedImage, Children int32
	Param                                   uintptr
}
type treeInsert struct {
	Parent, After uintptr
	Item          treeItem
	Extra         [4]uintptr
}
type treeHit struct {
	Point point
	Flags uint32
	Item  uintptr
}
type treeNotify struct {
	Header   nmHeader
	Action   uint32
	Old, New treeItem
	Drag     point
}
type drawItem struct {
	Type, ID, Item, Action, State uint32
	Window, DC                    uintptr
	Rect                          rect
	Data                          uintptr
}
type customDraw struct {
	Header           nmHeader
	Stage            uint32
	DC               uintptr
	Rect             rect
	Item             uintptr
	State            uint32
	Param            uintptr
	Text, Background uint32
	Level            int32
}
type palette struct{ Background, Panel, Field, Text, Muted, Accent, Online uint32 }
type treeEntry struct{ NetworkID, PeerID string }
type clientDialog struct {
	Window   uintptr
	Kind     string
	First    bool
	Controls map[int]uintptr
}
type ui struct {
	window, instance, font, boldFont, smallFont uintptr
	dpi                                         int
	controls                                    map[int]uintptr
	queue                                       chan func()
	ctx                                         context.Context
	cancel                                      context.CancelFunc
	store                                       *tunnel.Store
	api                                         *tunnel.API
	userID                                      string
	views                                       []model.NetworkView
	items                                       map[uintptr]treeEntry
	expanded                                    map[string]bool
	dialog                                      *clientDialog
	palette                                     palette
	backgroundBrush, panelBrush, fieldBrush     uintptr
	busy, polling, wanted                       bool
	activeID                                    string
	engine                                      *tunnel.Engine
	connectCancel                               context.CancelFunc
	connectSerial, serverSerial                 uint64
	connectMu                                   sync.Mutex
	ticks                                       int
	images                                      uintptr
	echoes                                      map[string]echoState
	probing                                     bool
	probeRound                                  uint64
	probeCancel                                 context.CancelFunc
}

func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }
func (u *ui) s(v int) int       { return v * u.dpi / 96 }
func brush(c uint32) uintptr    { h, _, _ := gdi.NewProc("CreateSolidBrush").Call(uintptr(c)); return h }
func (u *ui) post(f func()) {
	select {
	case u.queue <- f:
		call("PostMessageW", u.window, appMessage, 0, 0)
	case <-u.ctx.Done():
	}
}
func (u *ui) status(s string) {
	slog.Info("client_status", "message", s)
	u.post(func() { set(u.controls[999], s) })
}
func (u *ui) setTheme(theme string) {
	old := []uintptr{u.backgroundBrush, u.panelBrush, u.fieldBrush}
	if theme == "light" {
		u.palette = palette{rgb(245, 247, 250), rgb(231, 237, 246), rgb(226, 233, 241), rgb(30, 43, 60), rgb(110, 127, 146), rgb(40, 112, 216), rgb(25, 153, 98)}
	} else {
		u.palette = palette{rgb(15, 20, 29), rgb(24, 33, 46), rgb(35, 46, 62), rgb(234, 240, 249), rgb(133, 151, 175), rgb(91, 165, 255), rgb(70, 207, 151)}
	}
	u.backgroundBrush = brush(u.palette.Background)
	u.panelBrush = brush(u.palette.Panel)
	u.fieldBrush = brush(u.palette.Field)
	if tree := u.controls[3000]; tree != 0 {
		send(tree, 0x111d, 0, uintptr(u.palette.Background))
		send(tree, 0x111e, 0, uintptr(u.palette.Text))
		send(tree, 0x1128, 0, uintptr(u.palette.Muted))
		u.signalIcons()
	}
	for _, h := range old {
		if h != 0 {
			gdi.NewProc("DeleteObject").Call(h)
		}
	}
	if u.window != 0 {
		call("RedrawWindow", u.window, 0, 0, 0x185)
	}
}
func (u *ui) colorControl(msg uint32, dc, control uintptr) uintptr {
	p := u.palette
	color, background, b := p.Text, p.Background, u.backgroundBrush
	if msg == 0x133 {
		background, b = p.Field, u.fieldBrush
	}
	id := int(call("GetDlgCtrlID", control))
	if id == 1004 || id == 1005 || id == 1006 {
		background, b = p.Panel, u.panelBrush
	}
	if id == 1005 {
		color = p.Accent
	}
	if id == 999 || id == 1006 || id == 1009 || id == 1010 {
		color = p.Muted
	}
	gdi.NewProc("SetTextColor").Call(dc, uintptr(color))
	gdi.NewProc("SetBkColor").Call(dc, uintptr(background))
	return b
}
func (u *ui) paintBackground(h, dc uintptr) {
	var r rect
	call("GetClientRect", h, uintptr(unsafe.Pointer(&r)))
	call("FillRect", dc, uintptr(unsafe.Pointer(&r)), u.backgroundBrush)
	if h == u.window {
		card := rect{int32(u.s(10)), int32(u.s(8)), r.Right - int32(u.s(10)), int32(u.s(86))}
		u.rounded(dc, card, u.panelBrush)
	}
}
func (u *ui) drawButton(d *drawItem) {
	p := u.palette
	b := u.fieldBrush
	if d.ID == 100 || d.ID == 200 || d.ID == 201 || d.ID == 1 {
		b = u.panelBrush
	}
	background := u.backgroundBrush
	if d.ID == 100 {
		background = u.panelBrush
	}
	call("FillRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)), background)
	if d.ID != 100 {
		u.rounded(d.DC, d.Rect, b)
	}
	color := p.Text
	if d.State&4 != 0 {
		color = p.Muted
	}
	gdi.NewProc("SetBkMode").Call(d.DC, 1)
	gdi.NewProc("SetTextColor").Call(d.DC, uintptr(color))
	old, _, _ := gdi.NewProc("SelectObject").Call(d.DC, u.font)
	if d.ID == 100 {
		c := p.Muted
		title := "OFF"
		if u.wanted {
			c = p.Accent
			title = "ON"
		}
		pen, _, _ := gdi.NewProc("CreatePen").Call(0, uintptr(u.s(2)), uintptr(c))
		oldPen, _, _ := gdi.NewProc("SelectObject").Call(d.DC, pen)
		oldBrush, _, _ := gdi.NewProc("SelectObject").Call(d.DC, u.panelBrush)
		gdi.NewProc("Ellipse").Call(d.DC, uintptr(d.Rect.Left+2), uintptr(d.Rect.Top+2), uintptr(d.Rect.Right-2), uintptr(d.Rect.Bottom-2))
		gdi.NewProc("SetTextColor").Call(d.DC, uintptr(c))
		call("DrawTextW", d.DC, uintptr(unsafe.Pointer(wide(title))), ^uintptr(0), uintptr(unsafe.Pointer(&d.Rect)), 0x25)
		gdi.NewProc("SelectObject").Call(d.DC, oldPen)
		gdi.NewProc("SelectObject").Call(d.DC, oldBrush)
		gdi.NewProc("DeleteObject").Call(pen)
	} else {
		call("DrawTextW", d.DC, uintptr(unsafe.Pointer(wide(text(d.Window)))), ^uintptr(0), uintptr(unsafe.Pointer(&d.Rect)), 0x25)
	}
	gdi.NewProc("SelectObject").Call(d.DC, old)
	if d.State&0x10 != 0 {
		call("DrawFocusRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)))
	}
}
func (u *ui) add(parent uintptr, controls map[int]uintptr, id int, kind, title string, x, y, w, h int, style uintptr) uintptr {
	handle := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wide(kind))), uintptr(unsafe.Pointer(wide(title))), 0x50010000|style, uintptr(u.s(x)), uintptr(u.s(y)), uintptr(u.s(w)), uintptr(u.s(h)), parent, uintptr(id), u.instance, 0)
	controls[id] = handle
	send(handle, 0x30, u.font, 1)
	return handle
}
func (u *ui) menu() {
	bar := call("CreateMenu")
	add := func(menu uintptr, id int, title string) {
		call("AppendMenuW", menu, 0, uintptr(id), uintptr(unsafe.Pointer(wide(title))))
	}
	popup := func(title string) uintptr {
		h := call("CreatePopupMenu")
		call("AppendMenuW", bar, 0x10, h, uintptr(unsafe.Pointer(wide(title))))
		return h
	}
	sys := popup("Система")
	add(sys, 101, "Сервер и настройки…")
	add(sys, 101, "Изменить имя компьютера…")
	add(sys, 102, "Доступ через брандмауэр…")
	call("AppendMenuW", sys, 0x800, 0, 0)
	add(sys, 106, "Выход")
	network := popup("Сеть")
	add(network, 200, "Создать сеть…")
	add(network, 201, "Присоединиться к сети…")
	call("AppendMenuW", network, 0x800, 0, 0)
	add(network, 204, "Подключить выбранную сеть")
	add(network, 205, "Отключить VPN")
	add(network, 208, "Обновить список")
	add(network, 202, "Создать приглашение")
	add(network, 203, "Покинуть сеть")
	add(network, 206, "Удалить сеть")
	help := popup("Справка")
	add(help, 400, "Как подключить компьютеры")
	add(help, 401, "Просмотреть журнал")
	add(help, 105, "Проверить обновления")
	add(help, 402, "О программе")
	call("SetMenu", u.window, bar)
}
func Run(store *tunnel.Store) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &ui{controls: map[int]uintptr{}, queue: make(chan func(), 256), ctx: ctx, cancel: cancel, store: store, items: map[uintptr]treeEntry{}, expanded: map[string]bool{}, echoes: map[string]echoState{}, dpi: 96}
	user.NewProc("SetProcessDPIAware").Call()
	if p := user.NewProc("GetDpiForSystem"); p.Find() == nil {
		v, _, _ := p.Call()
		if v >= 96 {
			u.dpi = int(v)
		}
	}
	u.instance, _, _ = kernel.NewProc("GetModuleHandleW").Call(0)
	cc := windows.NewLazySystemDLL("comctl32.dll")
	init := struct{ Size, Classes uint32 }{8, 2}
	cc.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&init)))
	cfg := store.Snapshot()
	u.setTheme(cfg.Theme)
	makeFont := func(height, weight int) uintptr {
		f, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(int64(-u.s(height))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(wide("Segoe UI"))))
		return f
	}
	u.font = makeFont(13, 400)
	u.boldFont = makeFont(16, 600)
	u.smallFont = makeFont(12, 400)
	mainClass := windowClass{Instance: u.instance, Cursor: call("LoadCursorW", 0, 32512), Name: wide("LinkLANClientV2")}
	mainClass.Size = uint32(unsafe.Sizeof(mainClass))
	mainClass.Proc = windows.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case 2:
			u.cancel()
			u.disconnect(false)
			call("PostQuitMessage", 0)
			return 0
		case 0x10:
			call("DestroyWindow", h)
			return 0
		case appMessage:
			for {
				select {
				case f := <-u.queue:
					f()
				default:
					return 0
				}
			}
		case 5:
			u.layout()
			return 0
		case 0x24:
			mm := (*minmax)(unsafe.Pointer(l))
			mm.MinTrack = point{int32(u.s(320)), int32(u.s(360))}
			return 0
		case 0x14:
			u.paintBackground(h, w)
			return 1
		case 0x133, 0x135, 0x138:
			return u.colorControl(msg, w, l)
		case 0x2b:
			u.drawButton((*drawItem)(unsafe.Pointer(l)))
			return 1
		case 0x111:
			if w>>16 == 0 {
				u.action(int(w & 0xffff))
			}
			return 0
		case 0x4e:
			return u.notify(l)
		case 0x7b:
			if w == u.controls[3000] && l == ^uintptr(0) {
				u.contextMenu()
			}
			return 0
		case 0x113:
			u.tick()
			return 0
		}
		return call("DefWindowProcW", h, uintptr(msg), w, l)
	})
	if call("RegisterClassExW", uintptr(unsafe.Pointer(&mainClass))) == 0 {
		return fmt.Errorf("Не удалось создать окно LinkLAN")
	}
	dialogClass := windowClass{Instance: u.instance, Cursor: mainClass.Cursor, Name: wide("LinkLANDialogV2")}
	dialogClass.Size = uint32(unsafe.Sizeof(dialogClass))
	dialogClass.Proc = windows.NewCallback(func(h uintptr, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case 0x10:
			u.closeDialog()
			return 0
		case 0x14:
			u.paintBackground(h, w)
			return 1
		case 0x133, 0x135, 0x138:
			return u.colorControl(msg, w, l)
		case 0x2b:
			u.drawButton((*drawItem)(unsafe.Pointer(l)))
			return 1
		case 0x111:
			if w>>16 == 0 {
				switch w & 0xffff {
				case 1:
					u.submitDialog()
				case 2:
					u.closeDialog()
				}
			}
			return 0
		}
		return call("DefWindowProcW", h, uintptr(msg), w, l)
	})
	call("RegisterClassExW", uintptr(unsafe.Pointer(&dialogClass)))
	height := u.s(480)
	screenHeight := int(call("GetSystemMetrics", 1)) - u.s(70)
	if height > screenHeight {
		height = screenHeight
	}
	u.window = call("CreateWindowExW", 0, uintptr(unsafe.Pointer(mainClass.Name)), uintptr(unsafe.Pointer(wide("LinkLAN"))), 0x00cf0000, 0x80000000, 0x80000000, uintptr(u.s(360)), uintptr(height), 0, 0, u.instance, 0)
	if u.window == 0 {
		return fmt.Errorf("Не удалось создать окно LinkLAN")
	}
	u.menu()
	u.add(u.window, u.controls, 100, "BUTTON", "", 22, 23, 46, 46, 0xb)
	u.add(u.window, u.controls, 1004, "STATIC", cfg.DeviceName, 82, 16, 250, 22, 0x4080)
	send(u.controls[1004], 0x30, u.boldFont, 1)
	u.add(u.window, u.controls, 1005, "STATIC", "VPN отключён", 82, 41, 250, 20, 0x4080)
	u.add(u.window, u.controls, 1006, "STATIC", "Укажите домен сервера", 82, 64, 250, 18, 0x4080)
	u.add(u.window, u.controls, 200, "BUTTON", "+ Создать сеть", 10, 96, 155, 28, 0xb)
	u.add(u.window, u.controls, 201, "BUTTON", "Присоединиться", 173, 96, 155, 28, 0xb)
	u.add(u.window, u.controls, 3000, "SysTreeView32", "", 10, 136, 318, 200, 0x00200027)
	u.add(u.window, u.controls, 1009, "STATIC", "Создайте сеть или присоединитесь к существующей.", 24, 154, 290, 52, 0)
	u.add(u.window, u.controls, 300, "BUTTON", "Копировать IP", 10, 350, 112, 26, 0xb)
	u.add(u.window, u.controls, 301, "BUTTON", "Ping", 130, 350, 58, 26, 0xb)
	u.add(u.window, u.controls, 1010, "STATIC", "ICMP", 198, 355, 126, 22, 0x4080)
	u.add(u.window, u.controls, 999, "STATIC", "Укажите домен сервера.", 10, 386, 318, 32, 0)
	for _, id := range []int{1006, 1010, 999} {
		send(u.controls[id], 0x30, u.smallFont, 1)
	}
	u.setTheme(cfg.Theme)
	u.layout()
	u.header()
	call("SetTimer", u.window, 1, 2000, 0)
	call("ShowWindow", u.window, 1)
	call("UpdateWindow", u.window)
	u.requestFirewall(false, func() {
		if cfg.Server == "" {
			u.openDialog("server", true)
		} else {
			u.attach(cfg.Server, cfg.DeviceName)
		}
	})
	var msg message
	for int32(call("GetMessageW", uintptr(unsafe.Pointer(&msg)), 0, 0, 0)) > 0 {
		if u.dialog != nil && call("IsDialogMessageW", u.dialog.Window, uintptr(unsafe.Pointer(&msg))) != 0 {
			continue
		}
		call("TranslateMessage", uintptr(unsafe.Pointer(&msg)))
		call("DispatchMessageW", uintptr(unsafe.Pointer(&msg)))
	}
	u.cancel()
	u.disconnect(false)
	// Let the tunnel worker remove the adapter and firewall rule before exit.
	u.connectMu.Lock()
	u.connectMu.Unlock()
	if u.images != 0 {
		commonControls.NewProc("ImageList_Destroy").Call(u.images)
	}
	for _, h := range []uintptr{u.font, u.boldFont, u.smallFont, u.backgroundBrush, u.panelBrush, u.fieldBrush} {
		if h != 0 {
			gdi.NewProc("DeleteObject").Call(h)
		}
	}
	return nil
}
func (u *ui) layout() {
	if u.controls[3000] == 0 {
		return
	}
	var r rect
	call("GetClientRect", u.window, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.Right), int(r.Bottom)
	move := func(id, x, y, width, height int) {
		call("MoveWindow", u.controls[id], uintptr(x), uintptr(y), uintptr(width), uintptr(height), 1)
	}
	pad := u.s(10)
	gap := u.s(8)
	bw := (w - 2*pad - gap) / 2
	move(200, pad, u.s(96), bw, u.s(28))
	move(201, pad+bw+gap, u.s(96), bw, u.s(28))
	move(3000, pad, u.s(136), w-2*pad, max(u.s(40), h-u.s(218)))
	move(1009, u.s(24), u.s(154), w-u.s(48), u.s(52))
	move(300, pad, h-u.s(68), u.s(112), u.s(26))
	move(301, pad+u.s(120), h-u.s(68), u.s(58), u.s(26))
	move(1010, pad+u.s(188), h-u.s(62), max(u.s(70), w-u.s(208)), u.s(20))
	move(999, pad, h-u.s(34), w-2*pad, u.s(30))
	for _, id := range []int{1004, 1005, 1006} {
		var y int
		switch id {
		case 1004:
			y = 16
		case 1005:
			y = 41
		case 1006:
			y = 64
		}
		move(id, u.s(82), u.s(y), w-u.s(102), u.s(22))
	}
	call("InvalidateRect", u.window, 0, 1)
}
func (u *ui) header() {
	cfg := u.store.Snapshot()
	set(u.controls[1004], cfg.DeviceName)
	state := "VPN отключён"
	if u.wanted {
		state = "Подключение…"
	}
	for _, v := range u.views {
		if v.Network.ID == u.activeID && u.engine != nil {
			state = v.Network.IP + "  ·  В сети"
		}
	}
	set(u.controls[1005], state)
	domain := strings.TrimPrefix(strings.TrimPrefix(cfg.Server, "https://"), "http://")
	if domain == "" {
		domain = "Укажите домен сервера"
	} else if u.api == nil {
		domain += " · нет подключения"
	}
	set(u.controls[1006], domain)
	call("InvalidateRect", u.controls[100], 0, 1)
	call("EnableWindow", u.controls[300], boolArg(u.selected().PeerID != ""))
	call("EnableWindow", u.controls[301], boolArg(u.selected().PeerID != ""))
	hint := "ICMP"
	entry := u.selected()
	if entry.PeerID != "" {
		state := u.echo(entry.PeerID, entry.NetworkID)
		if entry.NetworkID != u.activeID || u.engine == nil {
			hint = "Сеть отключена"
		} else if !state.Measured {
			hint = "ICMP…"
		} else if state.Reachable {
			hint = "ICMP " + echoTime(state.RTT)
		} else {
			hint = "Нет ICMP"
		}
	}
	set(u.controls[1010], hint)
}
func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
func (u *ui) work(label string, f func(context.Context) error) {
	if u.busy {
		return
	}
	u.busy = true
	set(u.controls[999], label+"…")
	go func() {
		ctx, c := context.WithTimeout(u.ctx, 75*time.Second)
		defer c()
		err := f(ctx)
		u.post(func() {
			u.busy = false
			if err != nil {
				set(u.controls[999], err.Error())
				slog.Warn("client_operation_failed", "message", err.Error())
			} else {
				set(u.controls[999], label+": готово")
			}
			u.header()
		})
	}()
}
func (u *ui) attach(server, name string) {
	if u.busy {
		return
	}
	normalized, err := tunnel.NormalizeServer(server)
	if err != nil {
		set(u.controls[999], err.Error())
		return
	}
	if name == "" {
		name = tunnel.DefaultDeviceName()
	}
	u.disconnect(false)
	u.serverSerial++
	serial := u.serverSerial
	u.api = nil
	u.views = nil
	u.renderTree()
	u.header()
	u.work("Подключение к серверу", func(ctx context.Context) error {
		a, e := tunnel.NewDeviceAPI(u.store, normalized)
		if e != nil {
			return e
		}
		if e = a.ConnectDevice(ctx, name); e != nil {
			return e
		}
		if e = u.store.Update(func(c *tunnel.Config) { c.Server = normalized; c.DeviceName = name }); e != nil {
			return e
		}
		var views []model.NetworkView
		if e = a.Call(ctx, "GET", "/networks/state?device_id="+a.Profile().DeviceID, nil, &views); e != nil {
			return e
		}
		userID := a.User.ID
		u.post(func() {
			if serial != u.serverSerial {
				return
			}
			u.api = a
			u.userID = userID
			u.views = views
			u.renderTree()
			u.header()
			cfg := u.store.Snapshot()
			if cfg.AutoConnect {
				for _, v := range views {
					if v.Network.ID == cfg.NetworkID && v.Network.Status == "approved" && !v.Network.Blocked {
						u.connect(v.Network.ID)
						break
					}
				}
			}
		})
		return nil
	})
}
func (u *ui) selected() treeEntry { return u.items[send(u.controls[3000], 0x110a, 9, 0)] }
func (u *ui) network(id string) (model.Network, bool) {
	for _, v := range u.views {
		if v.Network.ID == id {
			return v.Network, true
		}
	}
	return model.Network{}, false
}
func (u *ui) peer(entry treeEntry) (model.Peer, bool) {
	for _, v := range u.views {
		if v.Network.ID == entry.NetworkID {
			for _, p := range v.Peers {
				if p.ID == entry.PeerID {
					return p, true
				}
			}
		}
	}
	return model.Peer{}, false
}
func (u *ui) renderTree() {
	selected := u.selected()
	tree := u.controls[3000]
	if tree == 0 {
		return
	}
	firstVisible := u.items[send(tree, 0x110a, 5, 0)]
	for item, entry := range u.items {
		if entry.PeerID == "" {
			v := treeItem{Mask: 8, Handle: item, StateMask: 0x20}
			send(tree, 0x113e, 0, uintptr(unsafe.Pointer(&v)))
			u.expanded[entry.NetworkID] = v.State&0x20 != 0
		}
	}
	send(tree, 0xb, 0, 0)
	send(tree, 0x1101, 0, ^uintptr(0xffff))
	u.items = map[uintptr]treeEntry{}
	insert := func(parent uintptr, title string, entry treeEntry) uintptr {
		image := int32(-2)
		if entry.PeerID != "" {
			image = signalImage(u.echo(entry.PeerID, entry.NetworkID))
		}
		v := treeInsert{Parent: parent, After: ^uintptr(0xfffd), Item: treeItem{Mask: 1 | 2 | 0x20, Text: wide(title), Image: image, SelectedImage: image}}
		h := send(tree, 0x1132, 0, uintptr(unsafe.Pointer(&v)))
		u.items[h] = entry
		return h
	}
	var fallback, top uintptr
	for _, v := range u.views {
		n := v.Network
		title := shortName(n.Name, 28)
		if n.Blocked {
			title += " · ×"
		} else if n.Status == "pending" {
			title += " · ожидание"
		} else if n.ID == u.activeID && u.wanted {
			if u.engine != nil {
				title += "  •"
			} else {
				title += " · …"
			}
		}
		root := insert(^uintptr(0xffff), title, treeEntry{NetworkID: n.ID})
		if firstVisible.NetworkID == n.ID && firstVisible.PeerID == "" {
			top = root
		}
		if fallback == 0 {
			fallback = root
		}
		for _, p := range visiblePeers(v.Peers, u.deviceID()) {
			child := insert(root, u.peerLine(p, n.ID), treeEntry{n.ID, p.ID})
			if firstVisible.NetworkID == n.ID && firstVisible.PeerID == p.ID {
				top = child
			}
			if selected.NetworkID == n.ID && selected.PeerID == p.ID {
				fallback = child
			}
		}
		if selected.NetworkID == n.ID && selected.PeerID == "" {
			fallback = root
		}
		if expand, exists := u.expanded[n.ID]; !exists || expand {
			send(tree, 0x1102, 2, root)
		}
	}
	if fallback != 0 {
		send(tree, 0x110b, 9, fallback)
	}
	if top != 0 {
		send(tree, 0x110b, 5, top)
	}
	send(tree, 0xb, 1, 0)
	call("InvalidateRect", tree, 0, 1)
	call("ShowWindow", u.controls[1009], boolArg(len(u.views) == 0))
	u.header()
}
func routeName(s string) string {
	switch s {
	case "Offline":
		return "не в сети"
	case "Online":
		return "в сети"
	case "P2P":
		return "напрямую"
	case "Relay":
		return "через сервер"
	}
	return s
}
func (u *ui) deviceID() string {
	if u.api != nil {
		return u.api.Profile().DeviceID
	}
	return ""
}
func (u *ui) notify(l uintptr) uintptr {
	h := (*nmHeader)(unsafe.Pointer(l))
	if h.Window != u.controls[3000] {
		return 0
	}
	switch int32(h.Code) {
	case -451:
		u.header()
		return 0
	case -3:
		u.action(204)
		return 0
	case -5:
		u.contextMenu()
		return 0
	case -12:
		d := (*customDraw)(unsafe.Pointer(l))
		if d.Stage == 1 {
			return 0x20
		}
		if d.Stage == 0x10001 {
			d.Text = u.palette.Text
			entry := u.items[d.Item]
			if p, ok := u.peer(entry); ok {
				gdi.NewProc("SelectObject").Call(d.DC, u.font)
				if state := u.echo(p.ID, entry.NetworkID); !state.Measured || !state.Reachable {
					d.Text = u.palette.Muted
				}
			} else {
				gdi.NewProc("SelectObject").Call(d.DC, u.boldFont)
			}
			if d.State&1 != 0 {
				d.Text = rgb(255, 255, 255)
			}
			return 2
		}
	}
	return 0
}
func (u *ui) tick() {
	u.ticks++
	u.header()
	if u.ticks%2 == 0 {
		u.probePeers()
	}
	if u.ticks%8 == 0 && u.api != nil && !u.polling && !u.busy {
		u.poll()
	}
}
func (u *ui) poll() {
	if u.api == nil || u.polling {
		return
	}
	u.polling = true
	a, serial := u.api, u.serverSerial
	go func() {
		ctx, c := context.WithTimeout(u.ctx, 20*time.Second)
		defer c()
		var views []model.NetworkView
		e := a.Call(ctx, "GET", "/networks/state?device_id="+a.Profile().DeviceID, nil, &views)
		u.post(func() {
			u.polling = false
			if serial != u.serverSerial {
				return
			}
			if e != nil {
				set(u.controls[999], e.Error())
				return
			}
			u.views = views
			u.renderTree()
			if u.wanted {
				if n, ok := u.network(u.activeID); !ok || n.Blocked || n.Status != "approved" {
					u.disconnect(true)
					set(u.controls[999], "Доступ к сети прекращён")
				}
			}
		})
	}()
}
func (u *ui) disconnect(save bool) {
	u.connectSerial++
	u.clearEchoes()
	if u.connectCancel != nil {
		u.connectCancel()
		u.connectCancel = nil
	}
	u.engine = nil
	u.wanted = false
	u.activeID = ""
	if save {
		if e := u.store.Update(func(c *tunnel.Config) { c.AutoConnect = false }); e != nil {
			set(u.controls[999], e.Error())
		}
	}
	if u.window != 0 && u.ctx.Err() == nil {
		u.renderTree()
	}
}
func (u *ui) connect(id string) {
	if u.api == nil {
		return
	}
	n, ok := u.network(id)
	if !ok || n.Blocked || n.Status != "approved" {
		set(u.controls[999], "Выберите доступную сеть")
		return
	}
	if u.activeID == id && u.wanted {
		return
	}
	u.disconnect(false)
	u.wanted = true
	u.activeID = id
	serial := u.connectSerial
	api := u.api
	allowAll := u.store.Snapshot().FirewallAllowed
	factory := func(n model.Network) (tunnel.Adapter, error) { return tunnel.NewAdapterWithFirewall(n, allowAll) }
	if e := u.store.Update(func(c *tunnel.Config) { c.NetworkID = id; c.AutoConnect = true }); e != nil {
		u.wanted = false
		set(u.controls[999], e.Error())
		return
	}
	ctx, c := context.WithCancel(u.ctx)
	u.connectCancel = c
	u.renderTree()
	set(u.controls[999], "Подключение к сети «"+n.Name+"»…")
	go func() {
		u.connectMu.Lock()
		defer u.connectMu.Unlock()
		backoff := 2 * time.Second
		for ctx.Err() == nil {
			e, err := tunnel.Start(ctx, api, id, factory, func(s string) {
				u.post(func() {
					if serial == u.connectSerial {
						set(u.controls[999], s)
					}
				})
			})
			if err == nil {
				u.post(func() {
					if serial == u.connectSerial && ctx.Err() == nil {
						u.engine = e
						u.renderTree()
						u.header()
						u.probePeers()
					}
				})
				<-e.Done()
				e.Close()
				u.post(func() {
					if serial == u.connectSerial {
						u.engine = nil
						u.clearEchoes()
						u.renderTree()
					}
				})
				backoff = 2 * time.Second
			} else {
				u.post(func() {
					if serial == u.connectSerial {
						set(u.controls[999], err.Error())
					}
				})
				if ae, ok := err.(tunnel.APIError); ok && (ae.Status == 401 || ae.Status == 403) {
					u.post(func() {
						if serial == u.connectSerial {
							u.disconnect(false)
						}
					})
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
	}()
}
func (u *ui) openDialog(kind string, first bool) {
	if u.dialog != nil {
		call("SetForegroundWindow", u.dialog.Window)
		return
	}
	if u.busy {
		return
	}
	if kind != "server" && u.api == nil {
		set(u.controls[999], "Укажите сервер в меню «Система»")
		u.openDialog("server", true)
		return
	}
	title, height := "Настройки LinkLAN", 350
	if kind == "server" && first {
		title, height = "Подключение к серверу", 200
	}
	if kind == "create" {
		title, height = "Создать сеть", 350
	}
	if kind == "join" {
		title, height = "Присоединиться к сети", 310
	}
	var parent rect
	call("GetWindowRect", u.window, uintptr(unsafe.Pointer(&parent)))
	x := (parent.Left + parent.Right - int32(u.s(380))) / 2
	y := parent.Top + int32(u.s(75))
	h := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wide("LinkLANDialogV2"))), uintptr(unsafe.Pointer(wide(title))), 0x80c80000, uintptr(x), uintptr(y), uintptr(u.s(380)), uintptr(u.s(height)), u.window, 0, u.instance, 0)
	d := &clientDialog{Window: h, Kind: kind, First: first, Controls: map[int]uintptr{}}
	u.dialog = d
	label := func(id int, title string, y int) { u.add(h, d.Controls, id, "STATIC", title, 18, y, 328, 22, 0) }
	edit := func(id int, value string, y int, password bool) {
		style := uintptr(0x00800080)
		if password {
			style |= 0x20
		}
		u.add(h, d.Controls, id, "EDIT", value, 18, y, 328, 28, style)
		send(d.Controls[id], 0xc5, 256, 0)
	}
	cfg := u.store.Snapshot()
	caption := "Сохранить"
	switch kind {
	case "server":
		label(10, "Домен сервера", 14)
		edit(1000, cfg.Server, 40, false)
		send(d.Controls[1000], 0x1501, 1, uintptr(unsafe.Pointer(wide("linklan.lknet.ru"))))
		if first {
			label(11, "Имя компьютера: "+cfg.DeviceName, 82)
			caption = "Подключиться"
		} else {
			label(11, "Имя компьютера", 82)
			edit(1001, cfg.DeviceName, 108, false)
			label(12, "Тема", 148)
			u.add(h, d.Controls, 1030, "COMBOBOX", "", 18, 174, 328, 100, 0x00200003)
			for _, t := range []string{"Тёмная", "Светлая"} {
				send(d.Controls[1030], 0x143, 0, uintptr(unsafe.Pointer(wide(t))))
			}
			choice := uintptr(0)
			if cfg.Theme == "light" {
				choice = 1
			}
			send(d.Controls[1030], 0x14e, choice, 0)
			u.add(h, d.Controls, 1040, "BUTTON", "Подключать последнюю сеть при запуске", 18, 220, 328, 26, 5)
			send(d.Controls[1040], 0xf1, boolArg(cfg.AutoConnect), 0)
		}
	case "create":
		label(10, "Имя сети", 14)
		edit(1000, "", 40, false)
		label(11, "Пароль сети", 80)
		edit(1001, "", 106, true)
		label(12, "Подтвердите пароль", 146)
		edit(1002, "", 172, true)
		label(13, "Доступ", 210)
		u.add(h, d.Controls, 1030, "COMBOBOX", "", 96, 206, 250, 100, 0x00200003)
		for _, t := range []string{"По паролю", "По приглашению"} {
			send(d.Controls[1030], 0x143, 0, uintptr(unsafe.Pointer(wide(t))))
		}
		send(d.Controls[1030], 0x14e, 0, 0)
		label(14, "Пароль сети — от 12 символов.", 248)
		caption = "Создать"
	case "join":
		label(10, "Имя сети", 14)
		edit(1000, "", 40, false)
		label(11, "Пароль сети", 80)
		edit(1001, "", 106, true)
		label(12, "Или код приглашения", 146)
		edit(1002, "", 172, false)
		label(13, "Укажите имя и пароль либо только код.", 212)
		caption = "Присоединиться"
	}
	u.add(h, d.Controls, 1, "BUTTON", caption, 114, height-78, 138, 32, 0xb)
	u.add(h, d.Controls, 2, "BUTTON", "Отмена", 266, height-78, 90, 32, 0xb)
	call("EnableWindow", u.window, 0)
	call("ShowWindow", h, 1)
	call("SetFocus", d.Controls[1000])
	send(d.Controls[1000], 0xb1, 0, ^uintptr(0))
}
func (u *ui) closeDialog() {
	if u.dialog == nil {
		return
	}
	h := u.dialog.Window
	u.dialog = nil
	call("EnableWindow", u.window, 1)
	call("DestroyWindow", h)
	call("SetForegroundWindow", u.window)
}
func (u *ui) submitDialog() {
	d := u.dialog
	if d == nil || u.busy {
		return
	}
	name := strings.TrimSpace(text(d.Controls[1000]))
	password := text(d.Controls[1001])
	extra := strings.TrimSpace(text(d.Controls[1002]))
	if d.Kind == "server" {
		normalized, e := tunnel.NormalizeServer(name)
		if e != nil {
			notice(d.Window, "Сервер", e.Error())
			return
		}
		cfg := u.store.Snapshot()
		deviceName, theme, auto := cfg.DeviceName, cfg.Theme, cfg.AutoConnect
		if !d.First {
			deviceName = strings.TrimSpace(text(d.Controls[1001]))
			theme = "dark"
			if send(d.Controls[1030], 0x147, 0, 0) == 1 {
				theme = "light"
			}
			auto = send(d.Controls[1040], 0xf0, 0, 0) == 1
		}
		if len(deviceName) < 1 || len(deviceName) > 64 || strings.ContainsAny(deviceName, "|\r\n\x00") {
			notice(d.Window, "Имя компьютера", "Укажите имя до 64 байт")
			return
		}
		if e = u.store.Update(func(c *tunnel.Config) { c.Theme = theme; c.AutoConnect = auto }); e != nil {
			notice(d.Window, "Настройки", e.Error())
			return
		}
		u.closeDialog()
		u.setTheme(theme)
		u.attach(normalized, deviceName)
		return
	}
	mode := "password"
	if d.Kind == "create" && send(d.Controls[1030], 0x147, 0, 0) == 1 {
		mode = "invite"
	}
	if d.Kind == "create" {
		if len(name) < 2 || len(name) > 80 {
			notice(d.Window, "Создать сеть", "Имя сети: 2–80 байт")
			return
		}
		if mode == "password" && (len(password) < 12 || len(password) > 256 || password != text(d.Controls[1002])) {
			notice(d.Window, "Пароль сети", "Введите пароль от 12 символов и одинаковое подтверждение")
			return
		}
	}
	if d.Kind == "join" && name == "" && extra == "" {
		notice(d.Window, "Присоединиться", "Введите имя сети или код приглашения")
		return
	}
	api, kind := u.api, d.Kind
	u.closeDialog()
	u.work("Подключение сети", func(ctx context.Context) error {
		var n model.Network
		if kind == "create" {
			if e := api.Call(ctx, "POST", "/networks", map[string]any{"name": name, "password": password, "mode": mode, "max_members": 32, "device_id": api.Profile().DeviceID}, &n); e != nil {
				return e
			}
		} else {
			if extra != "" {
				var invite struct {
					NetworkID string `json:"network_id"`
				}
				if e := api.Call(ctx, "GET", "/invite/"+urlPath(extra), nil, &invite); e != nil {
					return e
				}
				name = invite.NetworkID
			}
			if e := api.Call(ctx, "POST", "/networks/"+urlPath(name)+"/join", map[string]string{"device_id": api.Profile().DeviceID, "password": password, "token": extra}, &n); e != nil {
				return e
			}
		}
		var views []model.NetworkView
		if e := api.Call(ctx, "GET", "/networks/state?device_id="+api.Profile().DeviceID, nil, &views); e != nil {
			return e
		}
		u.post(func() {
			u.views = views
			u.renderTree()
			u.selectNetwork(n.ID)
			if n.Status == "approved" {
				u.connect(n.ID)
			} else {
				set(u.controls[999], "Заявка отправлена владельцу сети")
			}
		})
		return nil
	})
}
func (u *ui) selectNetwork(id string) {
	for item, e := range u.items {
		if e.NetworkID == id && e.PeerID == "" {
			send(u.controls[3000], 0x110b, 9, item)
			send(u.controls[3000], 0x1102, 2, item)
			break
		}
	}
}
func (u *ui) contextMenu() {
	var p point
	call("GetCursorPos", uintptr(unsafe.Pointer(&p)))
	hit := treeHit{Point: p}
	call("ScreenToClient", u.controls[3000], uintptr(unsafe.Pointer(&hit.Point)))
	item := send(u.controls[3000], 0x1111, 0, uintptr(unsafe.Pointer(&hit)))
	if item != 0 {
		send(u.controls[3000], 0x110b, 9, item)
	}
	entry := u.selected()
	n, ok := u.network(entry.NetworkID)
	if !ok {
		return
	}
	menu := call("CreatePopupMenu")
	defer call("DestroyMenu", menu)
	add := func(id int, label string) {
		call("AppendMenuW", menu, 0, uintptr(id), uintptr(unsafe.Pointer(wide(label))))
	}
	if entry.PeerID != "" {
		add(300, "Копировать IP")
		add(301, "Ping")
		add(302, "Информация")
		if n.OwnerID == u.userID && entry.PeerID != u.deviceID() {
			add(303, "Удалить участника")
			add(304, "Заблокировать")
			if peer, ok := u.peer(entry); ok && peer.Status == "pending" {
				add(305, "Принять")
			}
		}
	} else {
		add(204, "Подключить сеть")
		add(205, "Отключить VPN")
		if n.OwnerID == u.userID {
			add(202, "Создать приглашение")
			add(206, "Удалить сеть")
		} else {
			add(203, "Покинуть сеть")
		}
	}
	chosen := call("TrackPopupMenu", menu, 0x100|2, uintptr(p.X), uintptr(p.Y), 0, u.window, 0)
	if chosen != 0 {
		u.action(int(chosen))
	}
}
func (u *ui) action(id int) {
	if id == 102 {
		u.requestFirewall(true, func() {
			if u.wanted {
				network := u.activeID
				u.disconnect(false)
				u.connect(network)
			}
		})
		return
	}
	if id == 106 {
		call("PostMessageW", u.window, 0x10, 0, 0)
		return
	}
	if id == 101 {
		u.openDialog("server", false)
		return
	}
	if id == 400 {
		notice(u.window, "Как подключить компьютеры", "1. Укажите один и тот же домен сервера на компьютерах.\n2. На первом нажмите «Создать сеть»: имя и пароль.\n3. На втором — «Присоединиться»: то же имя и пароль.\n4. VPN включится автоматически. В списке появятся IP.\n\nКнопка ON/OFF включает выбранную сеть. Правая кнопка мыши открывает Ping и копирование IP.\nОдновременно активна одна сеть; остальные остаются в списке.")
		return
	}
	if id == 402 {
		notice(u.window, "О LinkLAN", "LinkLAN "+model.Version+"\nВиртуальная локальная сеть для Windows 10/11 x64.\nДоступ устройства сохраняется на этом компьютере.\nАккаунт и Email не требуются.")
		return
	}
	if id == 401 {
		root, _ := os.UserConfigDir()
		exec.Command("notepad.exe", filepath.Join(root, "LinkLAN", "client.log")).Start()
		return
	}
	if id == 200 {
		u.openDialog("create", false)
		return
	}
	if id == 201 {
		u.openDialog("join", false)
		return
	}
	if u.busy {
		return
	}
	entry := u.selected()
	n, hasNetwork := u.network(entry.NetworkID)
	peer, hasPeer := u.peer(entry)
	api := u.api
	switch id {
	case 100:
		if u.wanted {
			u.disconnect(true)
			set(u.controls[999], "VPN отключён")
			return
		}
		if api == nil {
			cfg := u.store.Snapshot()
			if cfg.Server == "" {
				u.openDialog("server", true)
			} else {
				u.attach(cfg.Server, cfg.DeviceName)
			}
			return
		}
		if !hasNetwork {
			set(u.controls[999], "Создайте сеть или выберите её в списке")
			return
		}
		u.connect(n.ID)
	case 204:
		if api != nil && hasNetwork {
			u.connect(n.ID)
		}
	case 205:
		u.disconnect(true)
		set(u.controls[999], "VPN отключён")
	case 208:
		u.poll()
	case 202:
		if api == nil || !hasNetwork {
			return
		}
		u.work("Создание приглашения", func(ctx context.Context) error {
			var v struct{ Token string }
			if e := api.Call(ctx, "POST", "/invite", map[string]any{"network_id": n.ID, "uses": 10, "hours": 24}, &v); e != nil {
				return e
			}
			u.post(func() {
				if e := clipboard(u.window, v.Token); e != nil {
					set(u.controls[999], e.Error())
				}
				notice(u.window, "Приглашение", "Сеть: "+n.Name+"\nКод: "+v.Token+"\n\nКод скопирован. Действует 24 часа, до 10 подключений.")
			})
			return nil
		})
	case 203, 206:
		if api == nil || !hasNetwork {
			return
		}
		prompt := "Покинуть сеть «" + n.Name + "»?"
		if id == 206 {
			prompt = "Удалить сеть «" + n.Name + "» для всех участников?"
		}
		if call("MessageBoxW", u.window, uintptr(unsafe.Pointer(wide(prompt))), uintptr(unsafe.Pointer(wide("LinkLAN"))), 0x124) != 6 {
			return
		}
		if u.activeID == n.ID {
			u.disconnect(true)
		}
		u.work("Изменение сети", func(ctx context.Context) error {
			var e error
			if id == 206 {
				e = api.Call(ctx, "DELETE", "/networks/"+n.ID, nil, nil)
			} else {
				e = api.Call(ctx, "POST", "/networks/"+n.ID+"/leave", map[string]string{"device_id": api.Profile().DeviceID}, nil)
			}
			if e == nil {
				u.post(u.poll)
			}
			return e
		})
	case 300:
		if hasPeer && peer.IP != "" {
			if e := clipboard(u.window, peer.IP); e != nil {
				set(u.controls[999], e.Error())
			} else {
				set(u.controls[999], "IP скопирован: "+peer.IP)
			}
		}
	case 301:
		if !hasPeer || net.ParseIP(peer.IP) == nil {
			return
		}
		if u.engine == nil || entry.NetworkID != u.activeID {
			set(u.controls[999], "Сначала подключитесь к сети этого компьютера")
			return
		}
		serial := u.connectSerial
		u.work("Ping", func(ctx context.Context) error {
			result := peer.Name + " · " + peer.IP + "\n\n"
			for i := 0; i < 3; i++ {
				rtt, err := tunnel.PingIPv4(ctx, n.IP, peer.IP, 1500*time.Millisecond)
				state := echoState{Measured: true, Reachable: err == nil, RTT: rtt}
				if err == nil {
					result += fmt.Sprintf("Ответ %d: %s\n", i+1, echoTime(rtt))
				} else {
					result += fmt.Sprintf("Запрос %d: %s\n", i+1, err.Error())
				}
				u.post(func() {
					if serial == u.connectSerial {
						u.echoes[peer.ID] = state
						u.updateEcho(peer.ID)
					}
				})
			}
			u.post(func() { notice(u.window, "Ping "+peer.IP, result) })
			return nil
		})
	case 302:
		if hasPeer {
			fingerprint := sha256.Sum256([]byte(peer.PublicKey))
			route := "Не в сети"
			if peer.Online {
				route = "В сети"
			}
			if u.engine != nil {
				for _, s := range u.engine.Status() {
					if s.Peer.ID == peer.ID {
						route = routeName(s.Route)
						if s.Latency >= 0 {
							route += fmt.Sprintf(" · %d мс", s.Latency)
						}
					}
				}
			}
			notice(u.window, "Информация", fmt.Sprintf("Компьютер: %s\nСеть: %s\nIP: %s\nСоединение: %s\nКлюч: %x", peer.Name, n.Name, peer.IP, route, fingerprint[:12]))
		}
	case 303, 304, 305:
		if api == nil || !hasPeer || !hasNetwork || n.OwnerID != u.userID || peer.ID == u.deviceID() {
			return
		}
		u.work("Изменение участника", func(ctx context.Context) error {
			path := "/networks/" + n.ID + "/members/" + peer.ID
			method := "DELETE"
			if id == 304 {
				path += "/block"
				method = "POST"
			}
			if id == 305 {
				path += "/approve"
				method = "POST"
			}
			e := api.Call(ctx, method, path, map[string]string{}, nil)
			if e == nil {
				u.post(u.poll)
			}
			return e
		})
	case 105:
		cfg := u.store.Snapshot()
		if cfg.Server == "" {
			return
		}
		u.work("Проверка обновлений", func(ctx context.Context) error {
			m, e := updates.Check(ctx, cfg.Server)
			if e != nil {
				return e
			}
			if !updates.Newer(m.Version, model.Version) {
				u.post(func() { notice(u.window, "Обновления", "Установлена версия "+model.Version) })
				return nil
			}
			staged, e := updates.Stage(ctx, cfg.Server, m)
			if e != nil {
				return e
			}
			u.post(func() { u.disconnect(false) })
			if e = updates.Install(staged); e != nil {
				return e
			}
			u.post(func() { call("PostMessageW", u.window, 0x10, 0, 0) })
			return nil
		})
	}
}
