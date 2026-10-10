//go:build windows

package desktop

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"linklan/internal/model"
	"linklan/internal/tunnel"
)

var commonControls = windows.NewLazySystemDLL("comctl32.dll")

type bitmapInfo struct {
	Size                   uint32
	Width, Height          int32
	Planes, Bits           uint16
	Compression, ImageSize uint32
	X, Y                   int32
	Used, Important        uint32
}

func (u *ui) rounded(dc uintptr, r rect, fill uintptr) {
	oldBrush, _, _ := gdi.NewProc("SelectObject").Call(dc, fill)
	nullPen, _, _ := gdi.NewProc("GetStockObject").Call(8)
	oldPen, _, _ := gdi.NewProc("SelectObject").Call(dc, nullPen)
	gdi.NewProc("RoundRect").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(u.s(10)), uintptr(u.s(10)))
	gdi.NewProc("SelectObject").Call(dc, oldBrush)
	gdi.NewProc("SelectObject").Call(dc, oldPen)
}

func argb(color uint32) uint32 { return 0xff000000 | (color&255)<<16 | color&0xff00 | color>>16&255 }
func (u *ui) signalIcons() {
	w, h := u.s(20), u.s(18)
	list, _, _ := commonControls.NewProc("ImageList_Create").Call(uintptr(w), uintptr(h), 0x21, 6, 0)
	if list == 0 {
		return
	}
	mask := rgb(255, 0, 255)
	for image := 0; image < 6; image++ {
		info := bitmapInfo{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, Bits: 32}
		var bits uintptr
		bitmap, _, _ := gdi.NewProc("CreateDIBSection").Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
		if bitmap == 0 {
			commonControls.NewProc("ImageList_Destroy").Call(list)
			return
		}
		pixels := unsafe.Slice((*uint32)(unsafe.Pointer(bits)), w*h)
		for i := range pixels {
			pixels[i] = argb(mask)
		}
		for bar := 0; bar < 4; bar++ {
			color := u.palette.Field
			if image > 1 && bar < image-1 {
				color = u.palette.Online
			}
			if image == 1 && bar == 0 {
				color = rgb(230, 94, 112)
			}
			left, bottom := u.s(2+bar*4), h-u.s(2)
			for y := bottom - u.s(4+bar*3); y < bottom; y++ {
				for x := left; x < left+max(1, u.s(3)); x++ {
					pixels[y*w+x] = argb(color)
				}
			}
		}
		commonControls.NewProc("ImageList_AddMasked").Call(list, bitmap, uintptr(mask))
		gdi.NewProc("DeleteObject").Call(bitmap)
	}
	old := u.images
	u.images = list
	send(u.controls[3000], 0x1109, 0, list)
	send(u.controls[3000], 0x111b, uintptr(u.s(26)), 0)
	if old != 0 {
		commonControls.NewProc("ImageList_Destroy").Call(old)
	}
}

func (u *ui) echo(peerID, networkID string) echoState {
	if networkID != u.activeID || u.engine == nil {
		return echoState{}
	}
	return u.echoes[peerID]
}
func echoTime(rtt time.Duration) string {
	if rtt < time.Millisecond {
		return "<1 мс"
	}
	return fmt.Sprintf("%d мс", rtt.Milliseconds())
}
func (u *ui) peerLine(peer model.Peer, networkID string) string {
	line := shortName(peer.Name, 16)
	if peer.Status == "pending" {
		return line + "  ·  ожидает принятия"
	}
	if peer.IP != "" {
		line += "  ·  " + peer.IP
	}
	if state := u.echo(peer.ID, networkID); state.Measured && state.Reachable {
		line += "  ·  " + echoTime(state.RTT)
	}
	return line
}
func shortName(name string, limit int) string {
	runes := []rune(name)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return name
}
func (u *ui) clearEchoes() {
	u.probeRound++
	if u.probeCancel != nil {
		u.probeCancel()
		u.probeCancel = nil
	}
	u.probing = false
	u.echoes = map[string]echoState{}
}
func (u *ui) updateEcho(peerID string) {
	for item, entry := range u.items {
		if entry.NetworkID != u.activeID || entry.PeerID != peerID {
			continue
		}
		peer, ok := u.peer(entry)
		if !ok {
			continue
		}
		image := signalImage(u.echo(peerID, entry.NetworkID))
		v := treeItem{Mask: 1 | 2 | 0x20, Handle: item, Text: wide(u.peerLine(peer, entry.NetworkID)), Image: image, SelectedImage: image}
		send(u.controls[3000], 0x113f, 0, uintptr(unsafe.Pointer(&v)))
	}
	u.header()
}

func (u *ui) probePeers() {
	if u.probing || u.engine == nil {
		return
	}
	network, ok := u.network(u.activeID)
	if !ok {
		return
	}
	var peers []model.Peer
	for _, view := range u.views {
		if view.Network.ID != network.ID {
			continue
		}
		for _, peer := range visiblePeers(view.Peers, u.deviceID()) {
			if peer.Status == "approved" && peer.IP != "" {
				peers = append(peers, peer)
			}
		}
	}
	if len(peers) == 0 {
		return
	}
	u.probing = true
	u.probeRound++
	round, serial := u.probeRound, u.connectSerial
	ctx, cancel := context.WithCancel(u.ctx)
	u.probeCancel = cancel
	go func() {
		defer cancel()
		jobs := make(chan model.Peer, len(peers))
		for _, peer := range peers {
			jobs <- peer
		}
		close(jobs)
		var workers sync.WaitGroup
		for i := 0; i < min(8, len(peers)); i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for peer := range jobs {
					if ctx.Err() != nil {
						return
					}
					rtt, err := tunnel.PingIPv4(ctx, network.IP, peer.IP, 750*time.Millisecond)
					if ctx.Err() != nil {
						return
					}
					state := echoState{Measured: true, Reachable: err == nil, RTT: rtt}
					u.post(func() {
						if round != u.probeRound || serial != u.connectSerial || u.engine == nil {
							return
						}
						u.echoes[peer.ID] = state
						u.updateEcho(peer.ID)
					})
				}
			}()
		}
		workers.Wait()
		u.post(func() {
			if round == u.probeRound {
				u.probing = false
				u.probeCancel = nil
			}
		})
	}()
}

// Consent belongs to the application UI. The user's accepted choice is stored
// in the existing DPAPI profile; only this application and virtual subnet get rules.
func (u *ui) requestFirewall(force bool, next func()) {
	if u.busy {
		return
	}
	allowed := u.store.Snapshot().FirewallAllowed
	if !allowed || force {
		body := "Разрешить LinkLAN доступ через брандмауэр Windows?\n\nБудут разрешены входящие и исходящие подключения LinkLAN, а также ICMP, TCP и UDP для игр и сервисов внутри виртуальной сети.\n\nРазрешение будет сохранено для следующих запусков."
		if call("MessageBoxW", u.window, uintptr(unsafe.Pointer(wide(body))), uintptr(unsafe.Pointer(wide("LinkLAN — брандмауэр"))), 0x124) != 6 {
			if !force && next != nil {
				next()
			}
			return
		}
	}
	u.busy = true
	set(u.controls[999], "Настройка доступа через брандмауэр…")
	go func() {
		ctx, cancel := context.WithTimeout(u.ctx, 30*time.Second)
		defer cancel()
		err := tunnel.EnsureClientFirewall(ctx)
		if err == nil {
			err = u.store.Update(func(c *tunnel.Config) { c.FirewallAllowed = true })
		}
		u.post(func() {
			u.busy = false
			if err != nil {
				set(u.controls[999], err.Error())
				notice(u.window, "Брандмауэр", err.Error())
			} else {
				set(u.controls[999], "Доступ через брандмауэр разрешён")
			}
			if next != nil && (!force || err == nil) {
				next()
			}
		})
	}()
}
