//go:build windows

package desktop

import (
	"fmt"
	"math"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"linklan/internal/model"
	"linklan/internal/tunnel"
)

func (u *ui) label(dc uintptr, value string, r rect, font uintptr, color uint32, flags uintptr) {
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, font)
	gdi.NewProc("SetBkMode").Call(dc, 1)
	gdi.NewProc("SetTextColor").Call(dc, uintptr(color))
	call("DrawTextW", dc, uintptr(unsafe.Pointer(wide(value))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags|0x800|0x20)
	gdi.NewProc("SelectObject").Call(dc, old)
}
func (u *ui) paintChrome(dc uintptr, r rect) {
	title := rect{0, 0, r.Right, int32(u.s(compactTheme.Title))}
	call("FillRect", dc, uintptr(unsafe.Pointer(&title)), u.panelBrush)
	x := int(r.Right)/2 - u.s(56)
	if u.smallIcon != 0 {
		call("DrawIconEx", dc, uintptr(x), uintptr(u.s(4)), u.smallIcon, uintptr(u.s(24)), uintptr(u.s(24)), 0, 0, 3)
	}
	u.label(dc, "LINKLAN", rect{int32(x + u.s(29)), 0, r.Right - int32(u.s(75)), int32(u.s(compactTheme.Title))}, u.boldFont, u.palette.Accent, 4)
	u.roundedRadius(dc, rect{int32(u.s(compactTheme.Padding)), int32(u.s(compactTheme.CardTop)), r.Right - int32(u.s(compactTheme.Padding)), int32(u.s(compactTheme.CardTop + compactTheme.CardHeight))}, u.panelBrush, compactTheme.Radius)
	call("FrameRect", dc, uintptr(unsafe.Pointer(&r)), u.panelBrush)
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, 1, uintptr(u.palette.Muted))
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	for i := 0; i < 3; i++ {
		gap := int32(u.s(3 + i*3))
		gdi.NewProc("MoveToEx").Call(dc, uintptr(r.Right-gap), uintptr(r.Bottom-3), 0)
		gdi.NewProc("LineTo").Call(dc, uintptr(r.Right-3), uintptr(r.Bottom-gap))
	}
	gdi.NewProc("SelectObject").Call(dc, old)
	gdi.NewProc("DeleteObject").Call(pen)
}
func (u *ui) drawPower(d *drawItem) {
	r := d.Rect
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	pixels := make([]uint32, w*h)
	radius := float64(min(w, h))/2 - 1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sums := [3]float64{}
			for _, dy := range []float64{0.25, 0.75} {
				for _, dx := range []float64{0.25, 0.75} {
					distance := math.Hypot(float64(x)+dx-float64(w)/2, float64(y)+dy-float64(h)/2) / radius
					c := u.palette.Panel
					if distance < 1 {
						if distance > 0.94 {
							c = rgb(103, 129, 141)
						} else if distance > 0.89 {
							c = rgb(214, 240, 245)
						} else {
							highlight := uint32(22*(1-float64(y)/float64(h)) + 12*math.Max(0, 1-distance))
							if u.hotButton == d.Window {
								highlight += 8
							}
							if u.wanted {
								c = rgb(19+highlight, 155+highlight, 177+highlight)
							} else {
								c = rgb(176+highlight, 79+highlight, 51+highlight)
							}
						}
					}
					sums[0] += float64(c & 255)
					sums[1] += float64(c >> 8 & 255)
					sums[2] += float64(c >> 16 & 255)
				}
			}
			pixels[y*w+x] = argb(rgb(uint32(sums[0]/4), uint32(sums[1]/4), uint32(sums[2]/4)))
		}
	}
	info := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, Bits: 32}
	gdi.NewProc("SetDIBitsToDevice").Call(d.DC, uintptr(r.Left), uintptr(r.Top), uintptr(w), uintptr(h), 0, 0, 0, uintptr(h), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), 0)
	cx, cy := r.Left+int32(w/2), r.Top+int32(u.s(22))
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, uintptr(max(2, u.s(2))), uintptr(rgb(255, 255, 255)))
	old, _, _ := gdi.NewProc("SelectObject").Call(d.DC, pen)
	gdi.NewProc("Arc").Call(d.DC, uintptr(cx-int32(u.s(7))), uintptr(cy-int32(u.s(7))), uintptr(cx+int32(u.s(7))), uintptr(cy+int32(u.s(7))), uintptr(cx-int32(u.s(4))), uintptr(cy-int32(u.s(6))), uintptr(cx+int32(u.s(4))), uintptr(cy-int32(u.s(6))))
	gdi.NewProc("MoveToEx").Call(d.DC, uintptr(cx), uintptr(cy-int32(u.s(10))), 0)
	gdi.NewProc("LineTo").Call(d.DC, uintptr(cx), uintptr(cy+int32(u.s(1))))
	gdi.NewProc("SelectObject").Call(d.DC, old)
	gdi.NewProc("DeleteObject").Call(pen)
	bottom := r
	bottom.Top += int32(u.s(35))
	bottom.Bottom -= int32(u.s(7))
	state := "OFF"
	if u.wanted {
		state = "ON"
	}
	u.label(d.DC, state, bottom, u.smallFont, rgb(255, 255, 255), 1|4)
}
func (u *ui) loadIcons() {
	if u.iconCache == nil {
		u.iconCache = map[int][2]uintptr{}
	}
	if pair, ok := u.iconCache[u.dpi]; ok {
		u.icon, u.smallIcon = pair[0], pair[1]
		return
	}
	u.icon = call("LoadImageW", u.instance, 1, 1, uintptr(u.s(48)), uintptr(u.s(48)), 0)
	u.smallIcon = call("LoadImageW", u.instance, 1, 1, uintptr(u.s(24)), uintptr(u.s(24)), 0)
	u.iconCache[u.dpi] = [2]uintptr{u.icon, u.smallIcon}
}
func (u *ui) hitTest(h, l uintptr) uintptr {
	var r rect
	call("GetWindowRect", h, uintptr(unsafe.Pointer(&r)))
	x, y := int32(int16(l&0xffff))-r.Left, int32(int16((l>>16)&0xffff))-r.Top
	edge := int32(u.s(6))
	width, height := r.Right-r.Left, r.Bottom-r.Top
	left, right, top, bottom := x < edge, x >= width-edge, y < edge, y >= height-edge
	if top && left {
		return 13
	}
	if top && right {
		return 14
	}
	if bottom && left {
		return 16
	}
	if bottom && right {
		return 17
	}
	if left {
		return 10
	}
	if right {
		return 11
	}
	if top {
		return 12
	}
	if bottom {
		return 15
	}
	if y < int32(u.s(compactTheme.Title)) && x < width-int32(u.s(82)) {
		return 2
	}
	return 1
}
func (u *ui) drawTreeRow(d *customDraw) {
	entry, exists := u.items[d.Item]
	if !exists {
		return
	}
	var bounds rect
	call("GetClientRect", u.controls[3000], uintptr(unsafe.Pointer(&bounds)))
	r := rect{0, d.Rect.Top, bounds.Right, d.Rect.Bottom + 1}
	call("FillRect", d.DC, uintptr(unsafe.Pointer(&r)), u.backgroundBrush)
	if entry.PeerID == "" {
		expanded := treeItem{Mask: 8, Handle: d.Item, StateMask: 0x20}
		send(u.controls[3000], 0x113e, 0, uintptr(unsafe.Pointer(&expanded)))
		hasChildren := send(u.controls[3000], 0x110a, 4, d.Item) != 0
		open := expanded.State&0x20 != 0 && hasChildren
		card := r
		card.Top += int32(u.s(4))
		if open {
			card.Bottom += int32(u.s(7))
		}
		u.roundedRadius(d.DC, card, u.panelBrush, 6)
		if open {
			bottom := r
			bottom.Top = card.Top + int32(u.s(6))
			call("FillRect", d.DC, uintptr(unsafe.Pointer(&bottom)), u.panelBrush)
		}
		arrow := "▸"
		if open {
			arrow = "▾"
		}
		tr := r
		tr.Top += int32(u.s(3))
		u.label(d.DC, arrow, rect{int32(u.s(7)), tr.Top, int32(u.s(23)), tr.Bottom}, u.menuFont, u.palette.Text, 4)
		n, _ := u.network(entry.NetworkID)
		name := n.Name
		if n.Blocked {
			name += " · заблокирована"
		} else if n.Status == "pending" {
			name += " · заявка"
		}
		u.label(d.DC, name, rect{int32(u.s(27)), tr.Top, r.Right - int32(u.s(10)), tr.Bottom}, u.networkFont, u.palette.Text, 4|0x8000)
		return
	}
	if send(u.controls[3000], 0x110a, 1, d.Item) == 0 {
		u.roundedRadius(d.DC, r, u.panelBrush, 6)
		top := r
		top.Bottom = top.Top + (top.Bottom-top.Top)/2
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&top)), u.panelBrush)
	} else {
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&r)), u.panelBrush)
	}
	selected := send(u.controls[3000], 0x110a, 9, 0) == d.Item
	if selected || u.hovered == d.Item {
		color := compactColors.Hover
		if selected {
			color = compactColors.Selection
		}
		b := brush(color)
		sr := r
		sr.Left += int32(u.s(4))
		sr.Right -= int32(u.s(4))
		sr.Top++
		sr.Bottom--
		u.roundedRadius(d.DC, sr, b, 11)
		gdi.NewProc("DeleteObject").Call(b)
	}
	peer, ok := u.peer(entry)
	if !ok {
		return
	}
	state := u.echo(peer.ID, entry.NetworkID)
	image := signalImage(state)
	if !peer.Online {
		image = 0
	}
	for bar := 0; bar < 4; bar++ {
		color := u.palette.Muted
		if image >= 2 && bar < int(image)-1 {
			color = u.palette.Online
			if image == 3 {
				color = compactColors.Warning
			} else if image == 2 {
				color = compactColors.Bad
			}
		}

		if image < 2 && bar > 0 {
			color = u.palette.Field
		}
		bottom := r.Top + (r.Bottom-r.Top)/2 + int32(u.s(6))
		br := rect{int32(u.s(14 + bar*4)), bottom - int32(u.s(3+bar*3)), int32(u.s(17 + bar*4)), bottom}
		b := brush(color)
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&br)), b)
		gdi.NewProc("DeleteObject").Call(b)
	}
	if image <= 1 {
		u.label(d.DC, "×", rect{int32(u.s(4)), r.Top, int32(u.s(14)), r.Bottom}, u.smallFont, u.palette.Muted, 4)
	}
	color := u.palette.Text
	if selected || u.hovered == d.Item {
		color = rgb(255, 255, 255)
	}
	if !peer.Online && !selected {
		color = u.palette.Muted
	}
	split := max(int32(u.s(130)), r.Right-int32(u.s(116)))
	name := peer.Name
	ip := peer.IP
	if peer.Status == "pending" {
		ip = "Ожидает принятия"
	}
	u.label(d.DC, name, rect{int32(u.s(34)), r.Top, split - int32(u.s(8)), r.Bottom}, u.smallFont, color, 4|0x8000)
	u.label(d.DC, ip, rect{split, r.Top, r.Right - int32(u.s(13)), r.Bottom}, u.smallFont, color, 2|4|0x8000)
}
func (u *ui) toggleHitNetwork() {
	var p point
	call("GetCursorPos", uintptr(unsafe.Pointer(&p)))
	call("ScreenToClient", u.controls[3000], uintptr(unsafe.Pointer(&p)))
	hit := treeHit{Point: p}
	item := send(u.controls[3000], 0x1111, 0, uintptr(unsafe.Pointer(&hit)))
	if e, ok := u.items[item]; ok && e.PeerID == "" {
		send(u.controls[3000], 0x1102, 3, item)
		u.expanded[e.NetworkID] = !u.expanded[e.NetworkID]
		u.scheduleWindowSave()
	}
}

type menuMeasure struct {
	Type, ID, Item, Width, Height uint32
	Data                          uintptr
}

func (u *ui) newMenu() uintptr {
	return call("CreatePopupMenu")
}
func (u *ui) menuItem(menu uintptr, id int, title string) {
	if u.menuLabels == nil {
		u.menuLabels = map[int]string{}
	}
	u.menuLabels[id] = title
	call("AppendMenuW", menu, 0x100, uintptr(id), uintptr(id))
}
func (u *ui) menuSeparator(menu uintptr) { call("AppendMenuW", menu, 0x102, 60000, 60000) }
func (u *ui) measureMenu(l uintptr) uintptr {
	m := (*menuMeasure)(unsafe.Pointer(l))
	if m.Type != 1 {
		return 0
	}
	m.Width = uint32(u.s(254))
	m.Height = uint32(u.s(28))
	if m.Item == 60000 {
		m.Height = uint32(u.s(8))
	}
	return 1
}
func (u *ui) drawMenu(d *drawItem) {
	b := u.fieldBrush
	if d.State&1 != 0 {
		b = brush(rgb(48, 91, 125))
		defer gdi.NewProc("DeleteObject").Call(b)
	}
	call("FillRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)), b)
	r := d.Rect
	r.Left += int32(u.s(18))
	r.Right -= int32(u.s(12))
	if d.Item == 60000 {
		line := r
		line.Top = (r.Top + r.Bottom) / 2
		line.Bottom = line.Top + 1
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&line)), u.backgroundBrush)
		return
	}
	u.label(d.DC, u.menuLabels[int(d.Item)], r, u.menuFont, u.palette.Text, 4)
}
func (u *ui) showTopMenu(id int) {
	h := u.controls[id]
	menu := u.menus[h]
	var r rect
	call("GetWindowRect", h, uintptr(unsafe.Pointer(&r)))
	chosen := call("TrackPopupMenu", menu, 0x100, uintptr(r.Left), uintptr(r.Bottom), 0, u.window, 0)
	if chosen != 0 {
		u.action(int(chosen))
	}
}
func (u *ui) mainKey(key uintptr) bool {
	ctrl := int16(call("GetKeyState", 0x11)) < 0
	switch key {
	case 0x74:
		u.action(208)
		return true
	case 'C':
		if ctrl {
			u.action(300)
			return true
		}
	case 0x79:
		u.showTopMenu(500)
		return true
	case 13:
		if u.selected().PeerID != "" {
			u.action(301)
		} else {
			u.action(204)
		}
		return true
	}
	return false
}
func (u *ui) showProperties(peer model.Peer, n model.Network, route, fingerprint string) {
	if u.dialog != nil {
		return
	}
	var parent rect
	call("GetWindowRect", u.window, uintptr(unsafe.Pointer(&parent)))
	title := "Свойства: " + peer.Name
	h := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wide("LinkLANDialogV2"))), uintptr(unsafe.Pointer(wide(title))), 0x82c80000, uintptr(parent.Left+int32(u.s(22))), uintptr(parent.Top+int32(u.s(100))), uintptr(u.s(340)), uintptr(u.s(360)), u.window, 0, u.instance, 0)
	d := &clientDialog{Window: h, Kind: "properties", Controls: map[int]uintptr{}}
	u.dialog = d
	role := "Участник"
	if peer.UserID == n.OwnerID {
		role = "Владелец"
	}
	for _, section := range []struct {
		title, body string
		y, height   int
	}{
		{"Свойства", fmt.Sprintf("Имя: %s\nID: %s\nIPv4: %s", peer.Name, peer.ID, peer.IP), 12, 88},
		{"Состояние сети", fmt.Sprintf("Имя сети: %s\nРоль: %s", n.Name, role), 112, 67},
		{"Статус подключения", fmt.Sprintf("Статус: %s\nОтпечаток ключа: %s", route, fingerprint), 191, 74},
	} {
		u.add(h, d.Controls, section.y, "STATIC", section.title, 18, section.y, 296, 20, 0)
		u.add(h, d.Controls, section.y+1, "STATIC", section.body, 24, section.y+25, 284, section.height-20, 0)
	}
	u.add(h, d.Controls, 2, "BUTTON", "Закрыть", 214, 286, 100, 30, 0xb)
	u.themeWindow(h)
	call("EnableWindow", u.window, 0)
	call("ShowWindow", h, 1)
}
func (u *ui) loadPreview() {
	u.preview = true
	u.userID = "preview-user"
	u.store.Update(func(c *tunnel.Config) { c.DeviceName = "and-home" })
	u.views = []model.NetworkView{}
	if u.options.LargePreview {
		u.largePreview()
		return
	}
	names := []string{"chukotka-museum", "lknet-home", "LinkLAN · друзья"}
	for i, name := range names {
		id := fmt.Sprint(i)
		n := model.Network{ID: id, Name: name, OwnerID: "preview-owner", IP: "26.27.94.226", Status: "approved"}
		view := model.NetworkView{Network: n}
		for j, name := range []string{"and-work", "ivan", "whatsapp", "DESKTOP-BADG655", "kate-book", "RS-ADMIN"} {
			peer := model.Peer{ID: fmt.Sprintf("%d-%d", i, j), Name: name, IP: fmt.Sprintf("26.%d.17.%d", 38+i, j+10), Status: "approved", Online: j%3 != 1}
			view.Peers = append(view.Peers, peer)
			u.echoes[peer.ID] = echoState{Measured: true, Reachable: peer.Online}
		}
		u.views = append(u.views, view)
		u.expanded[id] = i > 0
	}
	u.activeID = "1"
	u.wanted = true
	u.renderTree()
	u.header()
	set(u.controls[999], "Предпросмотр интерфейса · VPN не запущен")
}

func (u *ui) themeWindow(h uintptr) {
	dark := int32(1)
	if u.store.Snapshot().Theme == "light" {
		dark = 0
	}
	send(h, 0x80, 1, u.icon)
	send(h, 0x80, 0, u.smallIcon)
	windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute").Call(h, 20, uintptr(unsafe.Pointer(&dark)), 4)
}

// CheckIcon exercises the actual executable resources, including every common size.
func CheckIcon() error {
	instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
	for _, size := range []uintptr{16, 24, 32, 48, 64, 128, 256} {
		icon := call("LoadImageW", instance, 1, 1, size, size, 0)
		if icon == 0 {
			return fmt.Errorf("Application icon resource missing at %d px", size)
		}
		call("DestroyIcon", icon)
	}
	return nil
}

func (u *ui) showMessage(title, body string) {
	if u.dialog != nil {
		return
	}
	var parent rect
	call("GetWindowRect", u.window, uintptr(unsafe.Pointer(&parent)))
	h := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wide("LinkLANDialogV2"))), uintptr(unsafe.Pointer(wide(title))), 0x82c80000, uintptr(parent.Left+int32(u.s(20))), uintptr(parent.Top+int32(u.s(100))), uintptr(u.s(400)), uintptr(u.s(280)), u.window, 0, u.instance, 0)
	d := &clientDialog{Window: h, Kind: "info", Controls: map[int]uintptr{}}
	u.dialog = d
	u.add(h, d.Controls, 11, "STATIC", body, 18, 18, 350, 165, 0)
	u.add(h, d.Controls, 2, "BUTTON", "Закрыть", 266, 196, 100, 30, 0xb)
	u.themeWindow(h)
	call("EnableWindow", u.window, 0)
	call("ShowWindow", h, 1)
	call("SetFocus", d.Controls[2])
}

func (u *ui) roundedRadius(dc uintptr, r rect, fill uintptr, radius int) {
	oldBrush, _, _ := gdi.NewProc("SelectObject").Call(dc, fill)
	pen, _, _ := gdi.NewProc("GetStockObject").Call(8)
	oldPen, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	gdi.NewProc("RoundRect").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(u.s(radius*2)), uintptr(u.s(radius*2)))
	gdi.NewProc("SelectObject").Call(dc, oldBrush)
	gdi.NewProc("SelectObject").Call(dc, oldPen)
}
func (u *ui) largePreview() {
	for i := 0; i < 50; i++ {
		id := fmt.Sprint(i)
		v := model.NetworkView{Network: model.Network{ID: id, Name: fmt.Sprintf("Сеть %02d", i+1), IP: "26.27.94.226", Status: "approved", OwnerID: "preview-owner"}}
		for j := 0; j < 10; j++ {
			p := model.Peer{ID: fmt.Sprintf("%d-%d", i, j), Name: fmt.Sprintf("Компьютер-%02d · длинное имя", i*10+j), IP: fmt.Sprintf("26.%d.17.%d", i+10, j+10), Status: "approved", Online: j%4 != 0}
			v.Peers = append(v.Peers, p)
			u.echoes[p.ID] = echoState{Measured: true, Reachable: p.Online, RTT: time.Duration(j*30) * time.Millisecond}
		}
		u.views = append(u.views, v)
		u.expanded[id] = true
	}
	u.activeID = "0"
	u.wanted = true
	u.renderTree()
	u.header()
}
