//go:build windows

package desktop

func (u *ui) settingsPage(page int) {
	d := u.dialog
	if d == nil || d.Kind != "server" || d.First {
		return
	}
	d.Page = page
	groups := [][]int{{10, 1000, 11, 1001}, {12, 1030}, {1040, 1041}, {1110, 1111}}
	for i, ids := range groups {
		for _, id := range ids {
			show := uintptr(0)
			if i == page {
				show = 5
			}
			call("ShowWindow", d.Controls[id], show)
		}
	}
	positions := map[int][4]int{10: {172, 14, 310, 22}, 1000: {172, 40, 310, 28}, 11: {172, 82, 310, 22}, 1001: {172, 108, 310, 28}, 12: {172, 14, 310, 22}, 1030: {172, 40, 310, 100}, 1040: {172, 20, 310, 42}, 1041: {172, 72, 310, 42}}
	for id, p := range positions {
		call("MoveWindow", d.Controls[id], uintptr(u.s(p[0])), uintptr(u.s(p[1])), uintptr(u.s(p[2])), uintptr(u.s(p[3])), 1)
	}
	for i := 1100; i <= 1103; i++ {
		call("InvalidateRect", d.Controls[i], 0, 1)
	}
	call("InvalidateRect", d.Window, 0, 1)
}
