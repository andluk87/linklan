package tunnel

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"linklan/internal/model"
	"linklan/internal/protocol"
	"linklan/internal/stun"
)

type Adapter interface {
	Read([]byte) (int, error)
	Write([]byte) error
	Close() error
}
type AdapterFactory func(model.Network) (Adapter, error)
type PeerStatus struct {
	Peer      model.Peer
	Route     string
	Latency   int64
	LastError string
}
type peer struct {
	info       model.Peer
	channel    *protocol.Channel
	direct     *net.UDPAddr
	lastDirect time.Time
	probe      []byte
	sent       time.Time
	latency    int64
}
type Engine struct {
	api             *API
	session         model.TunnelSession
	self            protocol.ID
	network         protocol.ID
	private         *ecdh.PrivateKey
	key             []byte
	conn            *net.UDPConn
	relay, stun     *net.UDPAddr
	adapter         Adapter
	cancel          context.CancelFunc
	ctx             context.Context
	wg              sync.WaitGroup
	mu              sync.Mutex
	peers           map[protocol.ID]*peer
	channels        map[string]*peer
	relaySequence   uint64
	nonce           []byte
	relayReady      bool
	wake            chan struct{}
	started         time.Time
	lastSync        time.Time
	errorText       string
	OnStatus        func(string)
	closed          bool
	closeOnce       sync.Once
	stunTransaction []byte
	External        string
}

func Start(parent context.Context, api *API, network string, factory AdapterFactory, status func(string)) (*Engine, error) {
	ctx, cancel := context.WithCancel(parent)
	e := &Engine{api: api, ctx: ctx, cancel: cancel, peers: map[protocol.ID]*peer{}, channels: map[string]*peer{}, wake: make(chan struct{}, 1), started: time.Now(), lastSync: time.Now(), OnStatus: status}
	cleanup := func(err error) (*Engine, error) { e.Close(); return nil, err }
	var err error
	e.private, err = ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return cleanup(err)
	}
	profile := api.Profile()
	e.self, err = protocol.ParseID(profile.DeviceID)
	if err != nil {
		return cleanup(err)
	}
	e.network, err = protocol.ParseID(network)
	if err != nil {
		return cleanup(err)
	}
	e.conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return cleanup(err)
	}
	e.conn.SetReadBuffer(4 * 1024 * 1024)
	// Discover the local IPv4 used toward the server without leaking VPN packets.
	u := strings.TrimPrefix(strings.TrimPrefix(api.Base, "https://"), "http://")
	u = strings.Split(u, "/")[0]
	host, _, splitErr := net.SplitHostPort(u)
	if splitErr != nil {
		host = u
	}
	localEndpoint := ""
	probeConn, err := net.Dial("udp4", net.JoinHostPort(host, "3478"))
	if err == nil {
		local := probeConn.LocalAddr().(*net.UDPAddr).IP
		probeConn.Close()
		if local.IsPrivate() {
			localEndpoint = net.JoinHostPort(local.String(), fmt.Sprint(e.conn.LocalAddr().(*net.UDPAddr).Port))
		}
	}
	var challenge struct{ Challenge string }
	if err = api.Call(ctx, "POST", "/devices/"+profile.DeviceID+"/challenge", map[string]string{}, &challenge); err != nil {
		return cleanup(err)
	}
	ephemeral := base64.RawStdEncoding.EncodeToString(e.private.PublicKey().Bytes())
	private, err := base64.RawStdEncoding.DecodeString(profile.PrivateKey)
	if err != nil || len(private) != 64 {
		return cleanup(fmt.Errorf("Некорректный ключ устройства"))
	}
	proof := model.SessionProof(challenge.Challenge, profile.DeviceID, network, ephemeral, localEndpoint)
	sig := base64.RawStdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(private), []byte(proof)))
	body := map[string]string{"network_id": network, "challenge": challenge.Challenge, "signature": sig, "ephemeral_key": ephemeral, "local_endpoint": localEndpoint}
	if err = api.Call(ctx, "POST", "/devices/"+profile.DeviceID+"/session", body, &e.session); err != nil {
		return cleanup(err)
	}
	e.key, err = base64.RawURLEncoding.DecodeString(e.session.RelayKey)
	if err != nil || len(e.key) != 32 {
		return cleanup(fmt.Errorf("Некорректный ключ relay"))
	}
	e.relay, err = net.ResolveUDPAddr("udp4", e.session.RelayAddress)
	if err != nil {
		return cleanup(err)
	}
	e.stun, err = net.ResolveUDPAddr("udp4", e.session.STUNAddress)
	if err != nil {
		return cleanup(err)
	}
	e.adapter, err = factory(e.session.Network)
	if err != nil {
		return cleanup(err)
	}
	if err = e.update(e.session.Peers); err != nil {
		return cleanup(err)
	}
	e.nonce = make([]byte, 16)
	rand.Read(e.nonce)
	e.wg.Add(4)
	go func() { defer e.wg.Done(); e.receive() }()
	go func() { defer e.wg.Done(); e.readAdapter() }()
	go func() { defer e.wg.Done(); e.maintain() }()
	go func() { defer e.wg.Done(); e.realtime() }()
	e.say("VPN подключён · " + e.session.Network.IP)
	return e, nil
}
func (e *Engine) say(s string) {
	if e.OnStatus != nil {
		e.OnStatus(s)
	}
}
func (e *Engine) update(peers []model.Peer) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	next := map[protocol.ID]*peer{}
	var errors []string
	for _, p := range peers {
		if p.ID == e.self.String() || !p.Online || p.Status != "approved" {
			continue
		}
		id, err := protocol.ParseID(p.ID)
		if err != nil {
			continue
		}
		pub, err := base64.RawStdEncoding.DecodeString(p.PublicKey)
		if err != nil || len(pub) != 32 {
			continue
		}
		sig, err := base64.RawStdEncoding.DecodeString(p.SessionSignature)
		if err != nil || !ed25519.Verify(pub, []byte(model.SessionProof(p.SessionChallenge, p.ID, e.network.String(), p.EphemeralKey, p.LocalEndpoint)), sig) {
			errors = append(errors, "Неверная подпись "+p.Name)
			continue
		}
		if err = e.api.Pin(p.ID, p.PublicKey); err != nil {
			errors = append(errors, err.Error())
			continue
		}
		if e.channels == nil {
			e.channels = map[string]*peer{}
		}
		cacheKey := p.ID + "|" + p.EphemeralKey
		old := e.channels[cacheKey]
		if old != nil && old.info.EphemeralKey == p.EphemeralKey {
			old.info = p
			next[id] = old
			continue
		}
		remote, err := base64.RawStdEncoding.DecodeString(p.EphemeralKey)
		if err != nil {
			continue
		}
		ch, err := protocol.NewChannel(e.network, e.self, id, e.private, remote)
		if err != nil {
			continue
		}
		if len(e.channels) >= 8192 {
			e.cancel()
			return fmt.Errorf("Ротация ключей сеанса")
		}
		current := &peer{info: p, channel: ch, latency: -1}
		next[id] = current
		e.channels[cacheKey] = current
	}
	e.peers = next
	e.session.Peers = peers
	if len(errors) > 0 {
		e.errorText = strings.Join(errors, "; ")
	}
	return nil
}
func (e *Engine) Status() []PeerStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []PeerStatus{}
	for _, info := range e.session.Peers {
		route := "Offline"
		lat := int64(-1)
		if info.ID == e.self.String() {
			route = "Это устройство"
		}
		id, _ := protocol.ParseID(info.ID)
		if p := e.peers[id]; p != nil {
			if time.Since(p.lastDirect) < 6*time.Second {
				route = "P2P"
				lat = p.latency
			} else if e.relayReady {
				route = "Relay"
				lat = p.latency
			} else {
				route = "Подключение"
			}
		}
		out = append(out, PeerStatus{info, route, lat, e.errorText})
	}
	return out
}
func (e *Engine) sendRelay(p *peer, frame []byte) {
	if !e.relayReady {
		return
	}
	e.relaySequence++
	wrapped := protocol.WrapRelay(e.self, e.relaySequence, e.key, frame)
	e.conn.WriteToUDP(wrapped, e.relay)
}
func (e *Engine) route(p *peer, kind byte, payload []byte) {
	frame, err := p.channel.Seal(kind, payload)
	if err != nil {
		e.errorText = "Требуется новый сеанс VPN"
		return
	}
	if p.direct != nil && time.Since(p.lastDirect) < 6*time.Second {
		e.conn.WriteToUDP(frame, p.direct)
	} else {
		e.sendRelay(p, frame)
	}
}
func validEndpoint(s string) (*net.UDPAddr, bool) {
	a, err := net.ResolveUDPAddr("udp4", s)
	if err != nil || a.IP == nil || a.Port < 1024 || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast() || a.IP.IsMulticast() || a.IP.IsUnspecified() {
		return nil, false
	}
	return a, true
}
func (e *Engine) probePeer(p *peer) {
	payload := make([]byte, 16)
	rand.Read(payload)
	p.probe = payload
	p.sent = time.Now()
	targets := map[string]*net.UDPAddr{}
	for _, s := range []string{p.info.Endpoint, p.info.LocalEndpoint} {
		if a, ok := validEndpoint(s); ok {
			targets[a.String()] = a
		}
	}
	if p.direct != nil {
		targets[p.direct.String()] = p.direct
	}
	for _, a := range targets {
		frame, err := p.channel.Seal(protocol.Probe, payload)
		if err == nil {
			e.conn.WriteToUDP(frame, a)
		}
	}
	// Relay probes report remote liveness; they never mark a connection as P2P.
	frame, err := p.channel.Seal(protocol.Probe, payload)
	if err == nil {
		e.sendRelay(p, frame)
	}
}
func (e *Engine) readAdapter() {
	buf := make([]byte, protocol.MTU)
	for {
		n, err := e.adapter.Read(buf)
		if err != nil {
			return
		}
		source, destination, err := protocol.IPv4(buf[:n])
		if err != nil {
			continue
		}
		own := net.ParseIP(e.session.Network.IP).To4()
		if own == nil || string(source[:]) != string(own) {
			continue
		}
		e.mu.Lock()
		for _, p := range e.peers {
			ip := net.ParseIP(p.info.IP).To4()
			if ip != nil && string(destination[:]) == string(ip) {
				e.route(p, protocol.Data, buf[:n])
				break
			}
		}
		e.mu.Unlock()
		if e.ctx.Err() != nil {
			return
		}
	}
}
func (e *Engine) receive() {
	buf := make([]byte, 1600)
	for {
		n, remote, err := e.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		b := buf[:n]
		isRelay := remote.String() == e.relay.String()
		if isRelay && len(b) == 68 && string(b[:4]) == "LLC1" && string(b[4:20]) == string(e.self[:]) && string(b[20:36]) == string(e.nonce) {
			out := make([]byte, 100)
			copy(out, "LLA1")
			copy(out[4:68], b[4:68])
			copy(out[68:], protocol.MAC(e.key, out[:68]))
			e.conn.WriteToUDP(out, e.relay)
			continue
		}
		if isRelay && len(b) == 68 && string(b[:4]) == "LLO1" && string(b[4:20]) == string(e.self[:]) && string(b[20:36]) == string(e.nonce) && protocol.CheckMAC(e.key, b[:36], b[36:]) {
			e.mu.Lock()
			e.relayReady = true
			e.mu.Unlock()
			select {
			case e.wake <- struct{}{}:
			default:
			}
			continue
		}
		if remote.String() == e.stun.String() && stun.Is(b) {
			e.mu.Lock()
			transaction := append([]byte{}, e.stunTransaction...)
			e.mu.Unlock()
			if a, err := stun.ParseResponse(b, transaction); err == nil {
				e.mu.Lock()
				e.External = a.String()
				e.mu.Unlock()
			}
			continue
		}
		h, err := protocol.Parse(b)
		if err != nil {
			continue
		}
		e.mu.Lock()
		p := e.peers[h.Source]
		if p == nil {
			e.mu.Unlock()
			continue
		}
		h, payload, err := p.channel.Open(b)
		if err != nil {
			e.mu.Unlock()
			continue
		}
		switch h.Kind {
		case protocol.Probe:
			frame, err := p.channel.Seal(protocol.ProbeAck, payload)
			if err == nil {
				if isRelay {
					e.sendRelay(p, frame)
				} else {
					e.conn.WriteToUDP(frame, remote)
				}
			}
		case protocol.ProbeAck:
			if string(payload) == string(p.probe) && !p.sent.IsZero() {
				if !isRelay {
					p.direct = remote
					p.lastDirect = time.Now()
					p.latency = time.Since(p.sent).Milliseconds()
				} else if time.Since(p.lastDirect) >= 6*time.Second {
					p.latency = time.Since(p.sent).Milliseconds()
				}
			}
		case protocol.Data:
			source, destination, err := protocol.IPv4(payload)
			peerIP := net.ParseIP(p.info.IP).To4()
			own := net.ParseIP(e.session.Network.IP).To4()
			if err == nil && peerIP != nil && own != nil && string(source[:]) == string(peerIP) && string(destination[:]) == string(own) {
				if !isRelay {
					p.direct = remote
					p.lastDirect = time.Now()
				}
				e.adapter.Write(payload)
			}
		}
		e.mu.Unlock()
	}
}
func (e *Engine) maintain() {
	probeTick := time.NewTicker(2 * time.Second)
	helloTick := time.NewTicker(10 * time.Second)
	syncTick := time.NewTicker(5 * time.Second)
	heartbeatTick := time.NewTicker(45 * time.Second)
	defer probeTick.Stop()
	defer helloTick.Stop()
	defer syncTick.Stop()
	defer heartbeatTick.Stop()
	e.hello()
	e.syncPeers()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-helloTick.C:
			e.hello()
		case <-probeTick.C:
			e.mu.Lock()
			for _, p := range e.peers {
				e.probePeer(p)
			}
			e.mu.Unlock()
		case <-syncTick.C:
			e.syncPeers()
		case <-e.wake:
			e.syncPeers()
		case <-heartbeatTick.C:
			err := e.api.Call(e.ctx, "POST", "/devices/"+e.self.String()+"/heartbeat", map[string]string{"generation": e.session.Generation}, nil)
			if err != nil {
				e.say("VPN: " + err.Error())
				if ae, ok := err.(APIError); ok && (ae.Status == 401 || ae.Status == 403 || ae.Status == 409) {
					e.cancel()
					e.conn.Close()
					e.adapter.Close()
					return
				}
			}
		}
		if time.Since(e.started) > 6*time.Hour {
			e.say("Переподключитесь для обновления ключей сеанса")
			e.cancel()
			e.conn.Close()
			e.adapter.Close()
			return
		}
	}
}
func (e *Engine) hello() {
	out := make([]byte, 36)
	copy(out, "LLH1")
	copy(out[4:20], e.self[:])
	copy(out[20:36], e.nonce)
	e.conn.WriteToUDP(out, e.relay)
	request, err := stun.Request()
	if err == nil {
		e.mu.Lock()
		e.stunTransaction = append([]byte{}, request[8:20]...)
		e.mu.Unlock()
		e.conn.WriteToUDP(request, e.stun)
	}
}
func (e *Engine) syncPeers() {
	var peers []model.Peer
	err := e.api.Call(e.ctx, "GET", "/networks/"+e.network.String()+"/members", nil, &peers)
	if err == nil {
		found := false
		for _, p := range peers {
			if p.ID == e.self.String() && p.Status == "approved" && p.IP == e.session.Network.IP {
				found = true
			}
		}
		if !found {
			e.say("Доступ устройства к сети отозван")
			e.cancel()
			e.conn.Close()
			return
		}
		if err = e.update(peers); err != nil {
			e.cancel()
			return
		}
		e.mu.Lock()
		e.lastSync = time.Now()
		e.mu.Unlock()
		return
	}
	if ae, ok := err.(APIError); ok && (ae.Status == 401 || ae.Status == 403) {
		e.cancel()
		e.conn.Close()
		return
	}
	e.mu.Lock()
	stale := time.Since(e.lastSync) > 30*time.Second
	if stale {
		e.peers = map[protocol.ID]*peer{}
		e.relayReady = false
		e.errorText = "Нет связи с управлением; трафик приостановлен"
	}
	e.mu.Unlock()
	if stale {
		e.say("Нет связи с сервером. Ожидание восстановления…")
	}
}
func (e *Engine) realtime() {
	backoff := time.Second
	for {
		if e.ctx.Err() != nil {
			return
		}
		token, err := e.api.Token(e.ctx)
		if err != nil {
			select {
			case <-e.ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		address := strings.Replace(strings.Replace(e.api.Base, "https://", "wss://", 1), "http://", "ws://", 1) + "/ws"
		d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, TLSClientConfig: e.api.http.Transport.(*http.Transport).TLSClientConfig}
		conn, _, err := d.DialContext(e.ctx, address, http.Header{"Authorization": []string{"Bearer " + token}})
		if err == nil {
			backoff = time.Second
			conn.SetReadLimit(8192)
			finished := make(chan struct{})
			go func() {
				select {
				case <-e.ctx.Done():
					conn.Close()
				case <-finished:
				}
			}()
			for {
				if _, _, err = conn.ReadMessage(); err != nil {
					break
				}
				select {
				case e.wake <- struct{}{}:
				default:
				}
			}
			close(finished)
			conn.Close()
		}
		select {
		case <-e.ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.cancel()
		if e.conn != nil {
			e.conn.Close()
		}
		if e.adapter != nil {
			e.adapter.Close()
		}
		e.wg.Wait()
		if e.session.Generation != "" {
			ctx, c := context.WithTimeout(context.Background(), 4*time.Second)
			defer c()
			e.api.Call(ctx, "DELETE", "/devices/"+e.self.String()+"/session", map[string]string{"generation": e.session.Generation}, nil)
		}
	})
}
func (e *Engine) Done() <-chan struct{} { return e.ctx.Done() }
func ProbeTimestamp(p []byte) uint64 {
	if len(p) < 8 {
		return 0
	}
	return binary.BigEndian.Uint64(p)
}
