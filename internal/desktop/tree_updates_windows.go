//go:build windows

package desktop

import (
	"linklan/internal/model"
	"unsafe"
)

// Keep native item handles, selection, scroll position and expansion when only
// names, IPs or presence change. Only affected rows are invalidated.
func (u *ui) updateTreeRows(before, after []model.NetworkView) {
	changed := map[treeEntry]bool{}
	for i, v := range after {
		old := before[i]
		if old.Network.Name != v.Network.Name || old.Network.Status != v.Network.Status || old.Network.Blocked != v.Network.Blocked {
			changed[treeEntry{NetworkID: v.Network.ID}] = true
		}
		for j, p := range v.Peers {
			q := old.Peers[j]
			if p.IP != q.IP || p.Online != q.Online {
				delete(u.echoes, p.ID)
			}
			if p.Name != q.Name || p.IP != q.IP || p.Online != q.Online || p.Status != q.Status {
				changed[treeEntry{v.Network.ID, p.ID}] = true
			}
		}
	}
	for item, entry := range u.items {
		if !changed[entry] {
			continue
		}
		value := ""
		if entry.PeerID == "" {
			n, _ := u.network(entry.NetworkID)
			value = n.Name
		} else if peer, ok := u.peer(entry); ok {
			value = u.peerLine(peer, entry.NetworkID)
		}
		v := treeItem{Mask: 1, Handle: item, Text: wide(value)}
		send(u.controls[3000], 0x113f, 0, uintptr(unsafe.Pointer(&v)))
		u.invalidateRow(item)
	}
}
