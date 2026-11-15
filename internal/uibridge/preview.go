package uibridge

import (
	"fmt"
	"linklan/internal/model"
	"time"
)

func (c *Controller) preview() {
	c.userID = "preview-owner"
	c.deviceID = "preview-self"
	c.activeID = "preview-1"
	c.wanted = true
	count, peers := 3, 6
	if c.opts.Large {
		count, peers = 50, 10
	}
	names := []string{"chukotka-museum", "lknet-home", "LinkLAN · друзья"}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("preview-%d", i)
		name := fmt.Sprintf("Сеть %02d", i+1)
		if !c.opts.Large {
			name = names[i]
		}
		n := model.Network{ID: id, Name: name, OwnerID: "preview-owner", IP: "10.32.0.4", Subnet: "10.32.0.0/24", Status: "approved", Mode: "password", MaxMembers: 32}
		view := model.NetworkView{Network: n}
		for j := 0; j < peers; j++ {
			name := fmt.Sprintf("Компьютер-%03d · длинное имя", i*peers+j)
			if c.opts.Large && i == 0 && (j == 1 || j == 2) {
				name = "and-work"
			}
			if !c.opts.Large {
				name = []string{"and-home", "and-work", "and-work", "ivan", "whatsapp", "BOOK-RAST"}[j]
			}
			p := model.Peer{ID: fmt.Sprintf("peer-%d-%d", i, j), UserID: "preview-user", Name: name, IP: fmt.Sprintf("10.%d.0.%d", 32+i, j+2), Online: j%3 != 0, Status: "approved"}
			view.Peers = append(view.Peers, p)
			c.echoes[p.ID] = echo{true, p.Online, time.Duration(j*25) * time.Millisecond}
		}
		c.views = append(c.views, view)
	}
}
