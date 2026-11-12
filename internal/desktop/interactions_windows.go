//go:build windows

package desktop

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

type infoTip struct {
	Header      nmHeader
	Text        *uint16
	Max         int32
	Item, Param uintptr
}

func (u *ui) fillInfoTip(l uintptr) {
	n := (*infoTip)(unsafe.Pointer(l))
	if n.Text == nil || n.Max <= 0 || n.Max > 4096 {
		return
	}
	e := u.items[n.Item]
	peer, ok := u.peer(e)
	value := ""
	if ok {
		value = peer.Name + " · " + signalDescription(peer.Online, e.NetworkID == u.activeID && (u.engine != nil || u.preview), u.echo(peer.ID, e.NetworkID))
	} else if net, ok := u.network(e.NetworkID); ok {
		value = net.Name
	}
	words := windows.StringToUTF16(value)
	buffer := unsafe.Slice(n.Text, int(n.Max))
	count := min(len(words)-1, len(buffer)-1)
	copy(buffer, words[:count])
	buffer[count] = 0
}
func (u *ui) invalidateRow(item uintptr) {
	if item == 0 {
		return
	}
	var r rect
	*(*uintptr)(unsafe.Pointer(&r)) = item
	if send(u.controls[3000], 0x1104, 0, uintptr(unsafe.Pointer(&r))) != 0 {
		call("InvalidateRect", u.controls[3000], uintptr(unsafe.Pointer(&r)), 0)
	}
}
func (u *ui) installHover() {
	proc := windows.NewCallback(func(h uintptr, msg uint32, w, l, id, data uintptr) uintptr {
		if msg == 0x200 {
			if h == u.controls[3000] {
				hit := treeHit{Point: point{int32(int16(l & 0xffff)), int32(int16(l >> 16 & 0xffff))}}
				item := send(h, 0x1111, 0, uintptr(unsafe.Pointer(&hit)))
				if item != u.hovered {
					old := u.hovered
					u.hovered = item
					u.invalidateRow(old)
					u.invalidateRow(item)
				}
			} else if u.hotButton != h {
				old := u.hotButton
				u.hotButton = h
				if old != 0 {
					call("InvalidateRect", old, 0, 0)
				}
				call("InvalidateRect", h, 0, 0)
			}
			track := struct {
				Size, Flags uint32
				Window      uintptr
				Time        uint32
			}{Size: 24, Flags: 2, Window: h}
			call("TrackMouseEvent", uintptr(unsafe.Pointer(&track)))
		}
		if msg == 0x2a3 {
			if h == u.controls[3000] {
				old := u.hovered
				u.hovered = 0
				u.invalidateRow(old)
			} else if u.hotButton == h {
				u.hotButton = 0
				call("InvalidateRect", h, 0, 0)
			}
		}
		v, _, _ := commonControls.NewProc("DefSubclassProc").Call(h, uintptr(msg), w, l)
		return v
	})
	u.hoverProc = proc
	for _, id := range []int{100, 500, 501, 502, 510, 511, 3000} {
		if h := u.controls[id]; h != 0 {
			commonControls.NewProc("SetWindowSubclass").Call(h, proc, 1, 0)
		}
	}
}
