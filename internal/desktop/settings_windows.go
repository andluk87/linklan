//go:build windows

package desktop

import (
	"linklan/internal/tunnel"
	"strings"
	"unsafe"
)

func (u *ui) openSettings() {
	if u.dialog != nil {
		call("SetForegroundWindow", u.dialog.Window)
		return
	}
	x, y := u.dialogBounds(435, 380, 45)
	h := call("CreateWindowExW", 0, uintptr(unsafe.Pointer(wide("LinkLANDialogV2"))), uintptr(unsafe.Pointer(wide(tr("Настройки")))), 0x82080000, uintptr(x), uintptr(y), uintptr(u.s(435)), uintptr(u.s(380)), u.window, 0, u.instance, 0)
	d := &clientDialog{Window: h, Kind: "server", Controls: map[int]uintptr{}}
	u.dialog = d
	cfg := u.store.Snapshot()
	for i, label := range []string{tr("Тема"), tr("Язык"), tr("Дополнительно"), tr("Уведомления"), tr("Сервер"), tr("Администрирование")} {
		u.add(h, d.Controls, 1100+i, "BUTTON", label, 5, 8+i*36, 145, 32, 0xb)
	}
	add := func(id int, kind, title string, y, height int, style uintptr) {
		u.add(h, d.Controls, id, kind, title, 164, y, 252, height, style)
	}
	add(12, "STATIC", tr("Выберите цветовую схему"), 14, 34, 0)
	for i, label := range []string{tr("Светлая"), tr("Тёмная"), tr("Автоматически")} {
		add(1200+i, "BUTTON", label, 60+i*38, 32, 0xb)
	}
	add(13, "STATIC", tr("Язык интерфейса"), 14, 30, 0)
	add(1210, "BUTTON", tr("Русский"), 58, 32, 0xb)
	add(1211, "BUTTON", "English", 96, 32, 0xb)
	for _, c := range []struct {
		id, y int
		text  string
	}{{1041, 18, tr("Запускать при входе в Windows")}, {1042, 84, tr("Запускать свёрнутым в трей")}, {1040, 150, tr("Подключать последнюю сеть")}, {1220, 18, tr("Уведомления на рабочем столе")}, {1221, 84, tr("Разрешить звук")}, {1222, 150, tr("Соблюдать режим «Фокус»")}} {
		add(c.id, "BUTTON", c.text, c.y, 54, 0xb)
	}
	add(10, "STATIC", tr("Домен или адрес сервера"), 14, 24, 0)
	add(1000, "EDIT", cfg.Server, 42, 30, 0x00800080)
	add(11, "STATIC", tr("Имя компьютера"), 90, 24, 0)
	add(1001, "EDIT", cfg.DeviceName, 118, 30, 0x00800080)
	send(d.Controls[1000], 0xc5, 256, 0)
	send(d.Controls[1001], 0xc5, 64, 0)
	add(1250, "STATIC", "", 165, 65, 0)
	add(1, "BUTTON", tr("Сохранить"), 238, 32, 0xb)
	add(1111, "STATIC", tr("Управление пользователями, сетями, устройствами и подключениями.\n\nДля входа нужен токен администратора сервера."), 18, 145, 0)
	add(1110, "BUTTON", tr("Открыть админ-панель"), 184, 36, 0xb)
	u.add(h, d.Controls, 2, "BUTTON", tr("Закрыть"), 329, 306, 88, 30, 0xb)
	u.settingsPage(0)
	u.themeWindow(h)
	call("EnableWindow", u.window, 0)
	call("ShowWindow", h, 1)
	call("SetFocus", d.Controls[1200])
}
func (u *ui) settingsPage(page int) {
	d := u.dialog
	if d == nil || d.Kind != "server" || d.First || page < 0 || page > 5 {
		return
	}
	d.Page = page
	groups := [][]int{{12, 1200, 1201, 1202}, {13, 1210, 1211}, {1040, 1041, 1042}, {1220, 1221, 1222}, {10, 11, 1000, 1001, 1250, 1}, {1110, 1111}}
	for i, ids := range groups {
		for _, id := range ids {
			show := uintptr(0)
			if i == page {
				show = 5
			}
			call("ShowWindow", d.Controls[id], show)
		}
	}
	for i := 1100; i <= 1105; i++ {
		call("InvalidateRect", d.Controls[i], 0, 1)
	}
	call("RedrawWindow", d.Window, 0, 0, 0x185)
}
func (u *ui) settingsAction(id int) bool {
	d := u.dialog
	if d == nil || d.Kind != "server" || d.First {
		return false
	}
	if id == 1 && d.Page != 4 {
		u.closeDialog()
		return true
	}
	if id >= 1100 && id <= 1105 {
		u.settingsPage(id - 1100)
		return true
	}
	if id >= 1200 && id <= 1202 {
		theme := []string{"light", "dark", "auto"}[id-1200]
		if e := u.store.Update(func(c *tunnel.Config) { c.Theme = theme }); e != nil {
			notice(d.Window, tr("Настройки"), e.Error())
			return true
		}
		u.setTheme(theme)
		u.themeWindow(u.window)
		u.themeWindow(d.Window)
		return true
	}
	if id == 1210 || id == 1211 {
		language := "ru"
		if id == 1211 {
			language = "en"
		}
		if e := u.store.Update(func(c *tunnel.Config) { c.Language = language }); e != nil {
			notice(d.Window, tr("Настройки"), e.Error())
			return true
		}
		u.applyLanguage(language)
		return true
	}
	if id == 1041 {
		if u.startupPending {
			return true
		}
		cfg := u.store.Snapshot()
		enable := !boolSetting(cfg.StartAtLogon, true)
		if u.preview {
			u.store.Update(func(c *tunnel.Config) { c.StartAtLogon = &enable })
			call("InvalidateRect", d.Controls[id], 0, 1)
			return true
		}
		u.startupPending = true
		call("EnableWindow", d.Controls[id], 0)
		u.startupWait.Add(1)
		go func() {
			defer u.startupWait.Done()
			e := ApplyAutostart(u.store, enable)
			u.post(func() {
				u.startupPending = false
				if u.dialog == d {
					call("EnableWindow", d.Controls[id], 1)
					call("InvalidateRect", d.Controls[id], 0, 1)
				}
				if e != nil {
					u.lastError = e.Error()
					notice(u.window, tr("Автозагрузка"), e.Error())
				}
			})
		}()
		return true
	}
	if id == 1040 || id == 1042 || id >= 1220 && id <= 1222 {
		e := u.store.Update(func(c *tunnel.Config) {
			switch id {
			case 1040:
				c.AutoConnect = !c.AutoConnect
			case 1042:
				v := !boolSetting(c.StartInTray, true)
				c.StartInTray = &v
			case 1220:
				v := !boolSetting(c.NotifyDesktop, true)
				c.NotifyDesktop = &v
			case 1221:
				v := !boolSetting(c.NotifySound, true)
				c.NotifySound = &v
			case 1222:
				v := !boolSetting(c.RespectFocus, true)
				c.RespectFocus = &v
			}
		})
		if e != nil {
			notice(d.Window, tr("Настройки"), e.Error())
		}
		call("InvalidateRect", d.Controls[id], 0, 1)
		return true
	}
	return false
}
func (u *ui) saveServerSettings() {
	d := u.dialog
	if d == nil || u.busy {
		return
	}
	if u.preview {
		notice(d.Window, tr("Предпросмотр"), tr("В предпросмотре подключение к серверу не выполняется."))
		return
	}
	normalized, e := tunnel.NormalizeServer(strings.TrimSpace(text(d.Controls[1000])))
	if e != nil {
		set(d.Controls[1250], tr(e.Error()))
		return
	}
	name := strings.TrimSpace(text(d.Controls[1001]))
	if len(name) < 1 || len(name) > 64 || strings.ContainsAny(name, "|\r\n\x00") {
		set(d.Controls[1250], tr("Укажите имя до 64 байт"))
		return
	}
	cfg := u.store.Snapshot()
	if normalized != cfg.Server && u.wanted {
		if call("MessageBoxW", d.Window, uintptr(unsafe.Pointer(wide(tr("Смена сервера переподключит VPN. Продолжить?")))), uintptr(unsafe.Pointer(wide("LinkLAN"))), 0x124) != 6 {
			return
		}
	}
	u.attach(normalized, name)
}
func (u *ui) applyLanguage(language string) {
	setLanguage(language)
	for _, h := range []int{500, 501, 502} {
		call("DestroyWindow", u.controls[h])
		delete(u.controls, h)
	}
	for _, m := range u.menus {
		call("DestroyMenu", m)
	}
	u.menu()
	u.refreshFonts()
	u.installHover()
	u.layout()
	u.header()
	set(u.controls[1009], tr("Создайте сеть или присоединитесь к существующей."))
	if u.dialog != nil {
		d := u.dialog
		set(d.Window, tr("Настройки"))
		for id, caption := range d.Captions {
			set(d.Controls[id], relocalize(caption))
		}
		call("RedrawWindow", d.Window, 0, 0, 0x185)
	}
}
func (u *ui) paintDialogChrome(h, dc uintptr, r rect) {
	bar := rect{0, 0, r.Right, int32(u.s(32))}
	call("FillRect", dc, uintptr(unsafe.Pointer(&bar)), u.panelBrush)
	u.label(dc, text(h), rect{int32(u.s(12)), 0, r.Right - int32(u.s(38)), int32(u.s(32))}, u.font, u.palette.Text, 4|0x8000)
	if u.dialog != nil && u.dialog.Kind == "server" && !u.dialog.First {
		sidebar := rect{0, int32(u.s(32)), int32(u.s(150)), r.Bottom}
		call("FillRect", dc, uintptr(unsafe.Pointer(&sidebar)), u.panelBrush)
	}
	if u.dialog != nil {
		for id := range u.dialog.Edits {
			child := u.dialog.Controls[id]
			if call("IsWindowVisible", child) == 0 {
				continue
			}
			var field rect
			call("GetWindowRect", child, uintptr(unsafe.Pointer(&field)))
			call("MapWindowPoints", 0, h, uintptr(unsafe.Pointer(&field)), 2)
			field.Left -= int32(u.s(2))
			field.Right += int32(u.s(2))
			field.Top -= int32(u.s(2))
			field.Bottom += int32(u.s(2))
			border := brush(u.palette.Selection)
			u.roundedRadius(dc, field, border, 6)
			gdi.NewProc("DeleteObject").Call(border)
			field.Left += int32(u.s(1))
			field.Right -= int32(u.s(1))
			field.Top += int32(u.s(1))
			field.Bottom -= int32(u.s(1))
			u.roundedRadius(dc, field, u.fieldBrush, 5)
		}
	}

}
func (u *ui) drawSettingsControl(d *drawItem) bool {
	id := int(d.ID)
	if id == 1199 {
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)), u.panelBrush)
		if d.State&1 != 0 || u.hotButton == d.Window {
			b := brush(u.palette.Hover)
			call("FillRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)), b)
			gdi.NewProc("DeleteObject").Call(b)
		}
		u.label(d.DC, "×", d.Rect, u.font, u.palette.Text, 1|4)
		return true
	}
	if u.dialog != nil && u.dialog.Kind == "create" && (id == 1300 || id == 1301) {
		r := d.Rect
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&d.Rect)), u.backgroundBrush)
		selected := id == 1301 && u.dialog.CreateInvite || id == 1300 && !u.dialog.CreateInvite
		color := u.palette.Offline
		if selected {
			color = u.palette.Accent
		}
		b := brush(color)
		circle := rect{0, int32(u.s(8)), int32(u.s(16)), int32(u.s(24))}
		u.roundedRadius(d.DC, circle, b, 8)
		gdi.NewProc("DeleteObject").Call(b)
		if selected {
			dot := rect{int32(u.s(5)), int32(u.s(13)), int32(u.s(11)), int32(u.s(19))}
			u.roundedRadius(d.DC, dot, u.panelBrush, 3)
		}
		r.Left += int32(u.s(24))
		u.label(d.DC, text(d.Window), r, u.smallFont, u.palette.Text, 4)
		return true
	}
	if u.dialog == nil || u.dialog.Kind != "server" || u.dialog.First {
		return false
	}
	cfg := u.store.Snapshot()
	r := d.Rect
	if id >= 1100 && id <= 1105 {
		call("FillRect", d.DC, uintptr(unsafe.Pointer(&r)), u.panelBrush)
		if u.hotButton == d.Window && id-1100 != u.dialog.Page {
			b := brush(u.palette.Hover)
			u.roundedRadius(d.DC, r, b, 4)
			gdi.NewProc("DeleteObject").Call(b)
		}
		if id-1100 == u.dialog.Page {
			b := brush(u.palette.Selection)
			u.roundedRadius(d.DC, r, b, 4)
			gdi.NewProc("DeleteObject").Call(b)
		}
		u.drawSectionIcon(d.DC, id-1100, 8, 8)
		r.Left += int32(u.s(28))
		font := u.smallFont
		if id == 1105 && !englishUI.Load() {
			font = u.tinyFont
		}
		u.label(d.DC, text(d.Window), r, font, u.palette.Text, 4|0x8000)
		return true
	}
	radio := id >= 1200 && id <= 1202 || id == 1210 || id == 1211
	toggle := id == 1040 || id == 1041 || id == 1042 || id >= 1220 && id <= 1222
	if !radio && !toggle {
		return false
	}
	call("FillRect", d.DC, uintptr(unsafe.Pointer(&r)), u.backgroundBrush)
	enabled := false
	switch id {
	case 1200:
		enabled = cfg.Theme == "light" || cfg.Theme == ""
	case 1201:
		enabled = cfg.Theme == "dark"
	case 1202:
		enabled = cfg.Theme == "auto"
	case 1210:
		enabled = cfg.Language != "en"
	case 1211:
		enabled = cfg.Language == "en"
	case 1040:
		enabled = cfg.AutoConnect
	case 1041:
		enabled = boolSetting(cfg.StartAtLogon, true)
	case 1042:
		enabled = boolSetting(cfg.StartInTray, true)
	case 1220:
		enabled = boolSetting(cfg.NotifyDesktop, true)
	case 1221:
		enabled = boolSetting(cfg.NotifySound, true)
	case 1222:
		enabled = boolSetting(cfg.RespectFocus, true)
	}
	color := u.palette.Offline
	if enabled {
		color = u.palette.Accent
	}
	b := brush(color)
	if radio {
		old, _, _ := gdi.NewProc("SelectObject").Call(d.DC, b)
		null, _, _ := gdi.NewProc("GetStockObject").Call(8)
		oldPen, _, _ := gdi.NewProc("SelectObject").Call(d.DC, null)
		defer gdi.NewProc("SelectObject").Call(d.DC, oldPen)
		gdi.NewProc("Ellipse").Call(d.DC, uintptr(u.s(2)), uintptr(u.s(8)), uintptr(u.s(18)), uintptr(u.s(24)))
		gdi.NewProc("SelectObject").Call(d.DC, old)
		if enabled {
			white := brush(u.palette.Panel)
			inner := rect{int32(u.s(7)), int32(u.s(13)), int32(u.s(13)), int32(u.s(19))}
			u.roundedRadius(d.DC, inner, white, 3)
			gdi.NewProc("DeleteObject").Call(white)
		}
		r.Left += int32(u.s(28))
	} else {
		track := rect{0, int32(u.s(4)), int32(u.s(36)), int32(u.s(24))}
		u.roundedRadius(d.DC, track, b, 10)
		knob := rect{int32(u.s(3)), int32(u.s(7)), int32(u.s(17)), int32(u.s(21))}
		if enabled {
			knob.Left += int32(u.s(16))
			knob.Right += int32(u.s(16))
		}
		white := brush(themeColor(0xFFFFFF))
		u.roundedRadius(d.DC, knob, white, 7)
		gdi.NewProc("DeleteObject").Call(white)
		r.Left += int32(u.s(46))
	}
	gdi.NewProc("DeleteObject").Call(b)
	u.label(d.DC, text(d.Window), r, u.font, u.palette.Text, 0x10)
	return true
}
func (u *ui) drawSectionIcon(dc uintptr, index, x, y int) {
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, uintptr(max(1, u.s(1))), uintptr(u.palette.Name))
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	hollow, _, _ := gdi.NewProc("GetStockObject").Call(5)
	oldB, _, _ := gdi.NewProc("SelectObject").Call(dc, hollow)
	line := func(x1, y1, x2, y2 int) {
		gdi.NewProc("MoveToEx").Call(dc, uintptr(u.s(x+x1)), uintptr(u.s(y+y1)), 0)
		gdi.NewProc("LineTo").Call(dc, uintptr(u.s(x+x2)), uintptr(u.s(y+y2)))
	}
	switch index {
	case 0:
		gdi.NewProc("Ellipse").Call(dc, uintptr(u.s(x)), uintptr(u.s(y)), uintptr(u.s(x+15)), uintptr(u.s(y+15)))
		line(7, 0, 7, 15)
		line(7, 3, 12, 3)
		line(7, 6, 14, 6)
		line(7, 9, 14, 9)
		line(7, 12, 12, 12)
	case 1:
		line(1, 14, 7, 0)
		line(7, 0, 13, 14)
		line(4, 8, 10, 8)
		line(0, 16, 16, 16)
	case 2:
		for i := 0; i < 3; i++ {
			line(0, 2+i*5, 16, 2+i*5)
			line(3+i*5, i*5, 3+i*5, 4+i*5)
		}
	case 3:
		line(3, 5, 3, 12)
		line(13, 5, 13, 12)
		line(3, 12, 0, 15)
		line(0, 15, 16, 15)
		line(16, 15, 13, 12)
		line(3, 5, 8, 1)
		line(8, 1, 13, 5)
		line(6, 17, 10, 17)
	case 4:
		for i := 0; i < 3; i++ {
			gdi.NewProc("Rectangle").Call(dc, uintptr(u.s(x)), uintptr(u.s(y+i*6)), uintptr(u.s(x+16)), uintptr(u.s(y+i*6+5)))
			line(3, i*6+2, 5, i*6+2)
		}
	case 5:
		line(8, 0, 16, 4)
		line(16, 4, 14, 12)
		line(14, 12, 8, 17)
		line(8, 17, 2, 12)
		line(2, 12, 0, 4)
		line(0, 4, 8, 0)
		line(5, 8, 8, 11)
		line(8, 11, 12, 6)
	}
	gdi.NewProc("SelectObject").Call(dc, oldB)
	gdi.NewProc("SelectObject").Call(dc, old)
	gdi.NewProc("DeleteObject").Call(pen)
}
