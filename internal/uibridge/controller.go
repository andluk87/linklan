package uibridge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"linklan/internal/desktop"
	"linklan/internal/model"
	"linklan/internal/tunnel"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Options struct{ Preview, Large bool }
type echo struct {
	Measured, Reachable bool
	RTT                 time.Duration
}
type Controller struct {
	mu                                             sync.Mutex
	operations                                     sync.Mutex
	engineMu                                       sync.Mutex
	startup                                        sync.WaitGroup
	ctx                                            context.Context
	cancel                                         context.CancelFunc
	store                                          *tunnel.Store
	opts                                           Options
	api                                            *tunnel.API
	engine                                         *tunnel.Engine
	views                                          []model.NetworkView
	echoes                                         map[string]echo
	userID, deviceID, activeID, lastError, message string
	revision, serverSerial, connectSerial          uint64
	busy, wanted, reconnecting, probing            bool
	connectCancel                                  context.CancelFunc
}

func New(store *tunnel.Store, opts Options) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Controller{ctx: ctx, cancel: cancel, store: store, opts: opts, echoes: map[string]echo{}}
	if opts.Preview {
		c.preview()
		return c
	}
	c.startup.Add(1)
	go func() {
		defer c.startup.Done()
		cfg := store.Snapshot()
		if e := desktop.ApplyAutostart(store, option(cfg.StartAtLogon, true)); e != nil {
			c.fail(e)
		}
	}()
	go c.loop()
	return c
}
func (c *Controller) Close() {
	c.cancel()
	c.disconnect(false)
	c.engineMu.Lock()
	c.engineMu.Unlock()
	c.startup.Wait()
}
func (c *Controller) fail(e error) {
	if e == nil {
		return
	}
	slog.Warn("ui_operation_failed", "error", e.Error())
	c.mu.Lock()
	c.lastError = e.Error()
	c.revision++
	c.mu.Unlock()
}
func (c *Controller) note(s string) { c.mu.Lock(); c.message = s; c.revision++; c.mu.Unlock() }
func (c *Controller) State() State {
	c.mu.Lock()
	cfg := c.store.Snapshot()
	views := c.views
	e := c.engine
	state := State{Version: model.Version, Revision: c.revision, Settings: publicSettings(cfg), APIReady: c.api != nil, Busy: c.busy, Connected: e != nil, Wanted: c.wanted, Reconnecting: c.reconnecting, Error: c.lastError, Message: c.message, ActiveNetworkID: c.activeID, UserID: c.userID, DeviceID: c.deviceID, Networks: []Network{}}
	if c.opts.Preview {
		state.APIReady = true
		state.Connected = c.wanted
	}
	echoes := make(map[string]echo, len(c.echoes))
	for id, x := range c.echoes {
		echoes[id] = x
	}
	c.mu.Unlock()
	routes := map[string]string{}
	if e != nil {
		for _, p := range e.Status() {
			routes[p.Peer.ID] = p.Route
		}
	}
	for _, v := range views {
		n := v.Network
		out := Network{ID: n.ID, Name: n.Name, Description: n.Description, OwnerID: n.OwnerID, Subnet: n.Subnet, IP: n.IP, Mode: n.Mode, Status: n.Status, Blocked: n.Blocked, MaxMembers: n.MaxMembers, Peers: []Peer{}}
		if n.ID == state.ActiveNetworkID || state.OwnIP == "" && n.ID == cfg.NetworkID {
			state.OwnIP = n.IP
		}
		for _, p := range v.Peers {
			if p.ID == state.DeviceID {
				continue
			}
			x := echoes[p.ID]
			if n.ID != state.ActiveNetworkID {
				x = echo{}
			}
			var rtt *int64
			if x.Measured && x.Reachable {
				ms := x.RTT.Milliseconds()
				rtt = &ms
			}
			fingerprint := ""
			if raw, err := base64.RawStdEncoding.DecodeString(p.PublicKey); err == nil && len(raw) > 0 {
				h := sha256.Sum256(raw)
				fingerprint = hex.EncodeToString(h[:12])
			}
			route := routes[p.ID]
			if n.ID != state.ActiveNetworkID {
				route = ""
			}
			out.Peers = append(out.Peers, Peer{p.ID, p.UserID, p.Name, p.IP, p.Online, p.Status, route, x.Measured, x.Reachable, rtt, fingerprint})
		}
		state.Networks = append(state.Networks, out)
	}
	return state
}
func (c *Controller) loop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	c.runOperation(func(ctx context.Context) error {
		cfg := c.store.Snapshot()
		if cfg.Server == "" {
			return nil
		}
		return c.attach(ctx, cfg.Server, cfg.DeviceName)
	})
	ticks := 0
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ticks++
			c.probe()
			if ticks%4 == 0 {
				c.mu.Lock()
				idle := !c.busy
				c.mu.Unlock()
				if idle {
					go c.runOperation(c.refresh)
				}
			}
			c.mu.Lock()
			retry := c.api == nil && !c.busy && ticks%8 == 0
			c.mu.Unlock()
			if retry {
				go c.runOperation(func(ctx context.Context) error {
					cfg := c.store.Snapshot()
					if cfg.Server == "" {
						return nil
					}
					return c.attach(ctx, cfg.Server, cfg.DeviceName)
				})
			}
		}
	}
}
func (c *Controller) runOperation(f func(context.Context) error) error {
	c.operations.Lock()
	defer c.operations.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 75*time.Second)
	defer cancel()
	if e := ctx.Err(); e != nil {
		return e
	}
	c.mu.Lock()
	c.busy = true
	c.lastError = ""
	c.revision++
	c.mu.Unlock()
	e := f(ctx)
	c.mu.Lock()
	c.busy = false
	c.revision++
	c.mu.Unlock()
	if e != nil {
		c.fail(e)
	}
	return e
}

// Rename updates the existing authenticated identity without replacing the API
// instance or cancelling a running VPN session.
func (c *Controller) rename(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 64 || strings.ContainsAny(name, "|\r\n\x00") {
		return fmt.Errorf("Имя компьютера: 1–64 байта, без управляющих символов")
	}
	c.mu.Lock()
	a := c.api
	c.mu.Unlock()
	if a != nil {
		if e := a.ConnectDevice(ctx, name); e != nil {
			return e
		}
	}
	if e := c.store.Update(func(cfg *tunnel.Config) { cfg.DeviceName = name }); e != nil {
		return e
	}
	c.mu.Lock()
	c.revision++
	c.mu.Unlock()
	return nil
}
func (c *Controller) attach(ctx context.Context, server, name string) error {
	normalized, e := tunnel.NormalizeServer(server)
	if e != nil {
		return e
	}
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 64 || strings.ContainsAny(name, "|\r\n\x00") {
		return fmt.Errorf("Укажите имя до 64 байт")
	}
	a, e := tunnel.NewDeviceAPI(c.store, normalized)
	if e != nil {
		return e
	}
	if e = a.ConnectDevice(ctx, name); e != nil {
		return e
	}
	var views []model.NetworkView
	if e = a.Call(ctx, "GET", "/networks/state?device_id="+a.Profile().DeviceID, nil, &views); e != nil {
		return e
	}
	if e = c.store.Update(func(cfg *tunnel.Config) { cfg.Server = normalized; cfg.DeviceName = name }); e != nil {
		return e
	}
	c.disconnect(false)
	c.mu.Lock()
	c.api = a
	c.views = views
	c.deviceID = a.Profile().DeviceID
	c.userID = a.User.ID
	c.serverSerial++
	c.revision++
	c.mu.Unlock()
	cfg := c.store.Snapshot()
	if cfg.AutoConnect {
		for _, v := range views {
			if v.Network.ID == cfg.NetworkID && v.Network.Status == "approved" && !v.Network.Blocked {
				return c.connect(v.Network.ID)
			}
		}
	}
	return nil
}
func (c *Controller) refresh(ctx context.Context) error {
	c.mu.Lock()
	a, serial := c.api, c.serverSerial
	c.mu.Unlock()
	if a == nil {
		return nil
	}
	var views []model.NetworkView
	if e := a.Call(ctx, "GET", "/networks/state?device_id="+a.Profile().DeviceID, nil, &views); e != nil {
		return e
	}
	c.mu.Lock()
	if serial != c.serverSerial {
		c.mu.Unlock()
		return nil
	}
	c.views = views
	c.revision++
	active, wanted := c.activeID, c.wanted
	c.mu.Unlock()
	if wanted {
		allowed := false
		for _, v := range views {
			if v.Network.ID == active && v.Network.Status == "approved" && !v.Network.Blocked {
				allowed = true
			}
		}
		if !allowed {
			c.disconnect(true)
			return fmt.Errorf("Доступ к сети прекращён")
		}
	}
	return nil
}
func (c *Controller) disconnect(save bool) {
	c.mu.Lock()
	c.connectSerial++
	if c.connectCancel != nil {
		c.connectCancel()
		c.connectCancel = nil
	}
	c.engine = nil
	c.wanted = false
	c.reconnecting = false
	c.activeID = ""
	c.echoes = map[string]echo{}
	c.revision++
	c.mu.Unlock()
	if save {
		if e := c.store.Update(func(cfg *tunnel.Config) { cfg.AutoConnect = false }); e != nil {
			c.fail(e)
		}
	}
}
func (c *Controller) connect(id string) error {
	c.mu.Lock()
	a := c.api
	views := c.views
	if c.activeID == id && c.wanted {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if a == nil {
		return fmt.Errorf("Укажите сервер в настройках")
	}
	var n model.Network
	for _, v := range views {
		if v.Network.ID == id {
			n = v.Network
		}
	}
	if n.ID == "" || n.Blocked || n.Status != "approved" {
		return fmt.Errorf("Выберите доступную сеть")
	}
	if e := c.store.Update(func(cfg *tunnel.Config) { cfg.NetworkID = id; cfg.AutoConnect = true }); e != nil {
		return e
	}
	c.disconnect(false)
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	c.connectCancel = cancel
	c.wanted = true
	c.activeID = id
	c.lastError = ""
	serial := c.connectSerial
	c.revision++
	c.mu.Unlock()
	allow := c.store.Snapshot().FirewallAllowed
	go func() {
		c.engineMu.Lock()
		defer c.engineMu.Unlock()
		backoff := 2 * time.Second
		for ctx.Err() == nil {
			engine, e := tunnel.Start(ctx, a, id, func(n model.Network) (tunnel.Adapter, error) { return tunnel.NewAdapterWithFirewall(n, allow) }, c.note)
			if e == nil {
				c.mu.Lock()
				if serial == c.connectSerial && ctx.Err() == nil {
					c.engine = engine
					c.reconnecting = false
					c.lastError = ""
					c.revision++
				}
				c.mu.Unlock()
				<-engine.Done()
				engine.Close()
				c.mu.Lock()
				if serial == c.connectSerial {
					c.engine = nil
					c.reconnecting = true
					c.echoes = map[string]echo{}
					c.revision++
				}
				c.mu.Unlock()
				backoff = 2 * time.Second
			} else if ctx.Err() == nil {
				c.mu.Lock()
				current := serial == c.connectSerial
				c.mu.Unlock()
				if current {
					c.fail(e)
				}
				if ae, ok := e.(tunnel.APIError); ok && (ae.Status == 401 || ae.Status == 403) {
					if current {
						c.disconnect(false)
					}
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
	return nil
}
func (c *Controller) probe() {
	c.mu.Lock()
	if c.probing || c.engine == nil {
		c.mu.Unlock()
		return
	}
	active, serial := c.activeID, c.connectSerial
	var source string
	var peers []model.Peer
	for _, v := range c.views {
		if v.Network.ID == active {
			source = v.Network.IP
			peers = append(peers, v.Peers...)
		}
	}
	c.probing = true
	own := c.deviceID
	c.mu.Unlock()
	go func() {
		defer func() { c.mu.Lock(); c.probing = false; c.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(c.ctx, 12*time.Second)
		defer cancel()
		jobs := make(chan model.Peer, len(peers))
		for _, p := range peers {
			if p.ID != own && p.Online && p.IP != "" {
				jobs <- p
			}
		}
		close(jobs)
		var workers sync.WaitGroup
		for i := 0; i < 8; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for p := range jobs {
					rtt, e := tunnel.PingIPv4(ctx, source, p.IP, 1500*time.Millisecond)
					c.mu.Lock()
					if serial == c.connectSerial {
						c.echoes[p.ID] = echo{true, e == nil, rtt}
						c.revision++
					}
					c.mu.Unlock()
				}
			}()
		}
		workers.Wait()
	}()
}
func (c *Controller) preferences(p Preferences) error {
	if p.Theme != nil && *p.Theme != "light" && *p.Theme != "dark" && *p.Theme != "auto" {
		return fmt.Errorf("Некорректная тема")
	}
	if p.Language != nil && *p.Language != "ru" && *p.Language != "en" {
		return fmt.Errorf("Некорректный язык")
	}
	if p.Window != nil && (p.Window.Width < 250 || p.Window.Height < 350 || p.Window.Width > 5000 || p.Window.Height > 5000) {
		return fmt.Errorf("Некорректный размер окна")
	}
	if p.StartAtLogon != nil && !c.opts.Preview {
		if e := desktop.ApplyAutostart(c.store, *p.StartAtLogon); e != nil {
			return e
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.store.Update(func(cfg *tunnel.Config) {
		if p.Theme != nil {
			cfg.Theme = *p.Theme
		}
		if p.Language != nil {
			cfg.Language = *p.Language
		}
		if p.AutoConnect != nil {
			cfg.AutoConnect = *p.AutoConnect
		}
		if p.StartAtLogon != nil {
			v := *p.StartAtLogon
			cfg.StartAtLogon = &v
		}
		if p.StartInTray != nil {
			v := *p.StartInTray
			cfg.StartInTray = &v
		}
		if p.NotifyDesktop != nil {
			v := *p.NotifyDesktop
			cfg.NotifyDesktop = &v
		}
		if p.NotifySound != nil {
			v := *p.NotifySound
			cfg.NotifySound = &v
		}
		if p.RespectFocus != nil {
			v := *p.RespectFocus
			cfg.RespectFocus = &v
		}
		if p.Window != nil {
			cfg.Window = *p.Window
			cfg.WindowDIP = true
			cfg.WindowLayoutVersion = 1
		}
		if p.Expanded != nil {
			cfg.ExpandedNetworks = p.Expanded
		}
	})
	if e == nil {
		c.revision++
	}
	return e
}
func (c *Controller) call(ctx context.Context, method, path string, body, out any) error {
	c.mu.Lock()
	a := c.api
	c.mu.Unlock()
	if a == nil {
		return fmt.Errorf("Укажите сервер в настройках")
	}
	return a.Call(ctx, method, path, body, out)
}
func (c *Controller) Handle(method string, raw json.RawMessage) (any, error) {
	var b struct{ Name, Password, Confirm, Mode, Invite, Server, DeviceName, NetworkID, PeerID string }
	if len(raw) > 0 && string(raw) != "null" {
		if e := json.Unmarshal(raw, &b); e != nil {
			return nil, e
		}
	}
	switch method {
	case "state":
		return c.State(), nil
	case "settings":
		var p Preferences
		if e := json.Unmarshal(raw, &p); e != nil {
			return nil, e
		}
		if e := c.preferences(p); e != nil {
			return nil, e
		}
		return c.State(), nil
	case "rename":
		e := c.runOperation(func(ctx context.Context) error { return c.rename(ctx, b.DeviceName) })
		if e != nil {
			return nil, e
		}
		return c.State(), nil
	case "shutdown":
		return true, nil
	}
	if c.opts.Preview {
		switch method {
		case "connect":
			c.mu.Lock()
			c.wanted = true
			c.activeID = b.NetworkID
			c.revision++
			c.mu.Unlock()
			return c.State(), nil
		case "disconnect":
			c.disconnect(false)
			return c.State(), nil
		case "refresh":
			return c.State(), nil
		}
		return nil, fmt.Errorf("Это автономный предпросмотр: подключения не выполняются")
	}
	var result any
	e := c.runOperation(func(ctx context.Context) error {
		switch method {
		case "server":
			return c.attach(ctx, b.Server, b.DeviceName)
		case "refresh":
			return c.refresh(ctx)
		case "connect":
			return c.connect(b.NetworkID)
		case "disconnect":
			c.disconnect(true)
			return nil
		case "firewall":
			if e := tunnel.EnsureClientFirewall(ctx); e != nil {
				return e
			}
			if e := c.store.Update(func(cfg *tunnel.Config) { cfg.FirewallAllowed = true }); e != nil {
				return e
			}
			c.mu.Lock()
			id, wanted := c.activeID, c.wanted
			c.mu.Unlock()
			if wanted {
				c.disconnect(false)
				return c.connect(id)
			}
			return nil
		case "create", "join":
			c.mu.Lock()
			a, device := c.api, c.deviceID
			c.mu.Unlock()
			if a == nil {
				return fmt.Errorf("Укажите сервер в настройках")
			}
			var n model.Network
			if method == "create" {
				b.Name = strings.TrimSpace(b.Name)
				if len(b.Name) < 2 || len(b.Name) > 80 {
					return fmt.Errorf("Имя сети: 2–80 байт")
				}
				if b.Mode != "invite" {
					b.Mode = "password"
					if len(b.Password) < 12 || len(b.Password) > 256 || b.Password != b.Confirm {
						return fmt.Errorf("Введите пароль от 12 символов и одинаковое подтверждение")
					}
				} else {
					b.Password = ""
				}
				if e := a.Call(ctx, "POST", "/networks", map[string]any{"name": b.Name, "password": b.Password, "mode": b.Mode, "max_members": 32, "device_id": device}, &n); e != nil {
					return e
				}
			} else {
				if b.Invite != "" {
					var invite struct {
						ID string `json:"network_id"`
					}
					if e := a.Call(ctx, "GET", "/invite/"+url.PathEscape(b.Invite), nil, &invite); e != nil {
						return e
					}
					b.Name = invite.ID
				}
				if b.Name == "" {
					return fmt.Errorf("Введите имя сети или код приглашения")
				}
				if e := a.Call(ctx, "POST", "/networks/"+url.PathEscape(b.Name)+"/join", map[string]string{"device_id": device, "password": b.Password, "token": b.Invite}, &n); e != nil {
					return e
				}
			}
			if e := c.refresh(ctx); e != nil {
				return e
			}
			result = n.ID
			if n.Status == "approved" {
				return c.connect(n.ID)
			}
			return nil
		case "invite":
			var v struct{ Token string }
			if e := c.call(ctx, "POST", "/invite", map[string]any{"network_id": b.NetworkID, "uses": 10, "hours": 24}, &v); e != nil {
				return e
			}
			result = v.Token
			return nil
		case "leave", "delete":
			if b.NetworkID == "" {
				return fmt.Errorf("Выберите сеть")
			}
			c.mu.Lock()
			active, device := c.activeID, c.deviceID
			c.mu.Unlock()
			if active == b.NetworkID {
				c.disconnect(true)
			}
			verb, path, body := "DELETE", "/networks/"+url.PathEscape(b.NetworkID), any(nil)
			if method == "leave" {
				verb = "POST"
				path += "/leave"
				body = map[string]string{"device_id": device}
			}
			if e := c.call(ctx, verb, path, body, nil); e != nil {
				return e
			}
			return c.refresh(ctx)

		case "remove-member", "block-member", "approve-member":
			verb := "POST"
			path := "/networks/" + url.PathEscape(b.NetworkID) + "/members/" + url.PathEscape(b.PeerID)
			if method == "remove-member" {
				verb = "DELETE"
			} else if method == "block-member" {
				path += "/block"
			} else {
				path += "/approve"
			}
			if e := c.call(ctx, verb, path, map[string]string{}, nil); e != nil {
				return e
			}
			return c.refresh(ctx)
		default:
			return fmt.Errorf("Неизвестная команда интерфейса")
		}
	})
	if e != nil {
		return nil, e
	}
	if result != nil {
		return result, nil
	}
	return c.State(), nil
}
