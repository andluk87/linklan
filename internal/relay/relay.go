package relay

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"linklan/internal/model"
	"linklan/internal/protocol"
	"linklan/internal/stun"
)

type binding struct {
	addr       *net.UDPAddr
	generation string
	key        []byte
	last       time.Time
	window     protocol.ReplayWindow
	tokens     float64
	tokenAt    time.Time
}
type cachedAuth struct {
	auth model.RelayAuth
	at   time.Time
}
type sourceLimit struct {
	count int
	at    time.Time
}
type Relay struct {
	redis                   *redis.Client
	secret                  []byte
	mu                      sync.Mutex
	bindings                map[protocol.ID]*binding
	cache                   map[protocol.ID]cachedAuth
	limits                  map[string]sourceLimit
	Bandwidth               float64
	MaxConnections          int
	Bytes, Packets, Dropped atomic.Uint64
	Online                  atomic.Int64
}

func New(redisClient *redis.Client, secret []byte) (*Relay, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("RELAY_COOKIE_SECRET must be at least 32 bytes")
	}
	return &Relay{redis: redisClient, secret: secret, bindings: map[protocol.ID]*binding{}, cache: map[protocol.ID]cachedAuth{}, limits: map[string]sourceLimit{}, Bandwidth: 20 * 1024 * 1024, MaxConnections: 5000}, nil
}
func (r *Relay) getAuth(ctx context.Context, id protocol.ID) (model.RelayAuth, error) {
	now := time.Now()
	if c, ok := r.cache[id]; ok && now.Sub(c.at) < 2*time.Second {
		return c.auth, nil
	}
	var a model.RelayAuth
	b, e := r.redis.Get(ctx, "auth:"+id.String()).Bytes()
	if e != nil {
		delete(r.cache, id)
		return a, e
	}
	if e = json.Unmarshal(b, &a); e != nil {
		return a, e
	}
	r.cache[id] = cachedAuth{a, now}
	return a, nil
}
func (r *Relay) allowSource(addr *net.UDPAddr) bool {
	now := time.Now()
	ip := addr.IP.String()
	v, exists := r.limits[ip]
	if !exists && len(r.limits) >= 10000 {
		return false
	}
	if now.Sub(v.at) > time.Second {
		v = sourceLimit{at: now}
	}
	v.count++
	r.limits[ip] = v
	return v.count <= 30
}
func (r *Relay) handle(ctx context.Context, conn *net.UDPConn, b []byte, addr *net.UDPAddr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(b) < 4 {
		return
	}
	switch string(b[:4]) {
	case "LLH1":
		if len(b) != 36 || !r.allowSource(addr) {
			return
		}
		var id protocol.ID
		copy(id[:], b[4:20])
		if _, e := r.getAuth(ctx, id); e != nil {
			return
		}
		out := make([]byte, 68)
		copy(out, "LLC1")
		copy(out[4:36], b[4:36])
		copy(out[36:], protocol.Cookie(r.secret, addr.String(), id, b[20:36], time.Now().Unix()/30))
		conn.WriteToUDP(out, addr)
	case "LLA1":
		if len(b) != 100 || !r.allowSource(addr) {
			return
		}
		var id protocol.ID
		copy(id[:], b[4:20])
		if !protocol.VerifyCookie(r.secret, addr.String(), id, b[20:36], b[36:68], time.Now()) {
			return
		}
		a, e := r.getAuth(ctx, id)
		if e != nil {
			return
		}
		key, e := base64.RawURLEncoding.DecodeString(a.Key)
		if e != nil || len(key) != 32 || !protocol.CheckMAC(key, b[:68], b[68:]) {
			return
		}
		existing, exists := r.bindings[id]
		if !exists && len(r.bindings) >= r.MaxConnections {
			return
		}
		changed := !exists || existing.addr.String() != addr.String() || existing.generation != a.Generation
		if !exists || existing.generation != a.Generation {
			existing = &binding{key: key, generation: a.Generation, tokens: r.Bandwidth, tokenAt: time.Now()}
			r.bindings[id] = existing
		}
		existing.addr = addr
		existing.last = time.Now()
		raw, _ := json.Marshal(map[string]string{"Address": addr.String(), "Generation": a.Generation})
		r.redis.Set(ctx, "endpoint:"+id.String(), raw, 40*time.Second)
		if changed {
			event, _ := json.Marshal(map[string]string{"type": "endpoint_updated", "network_id": a.NetworkID})
			r.redis.Publish(ctx, "linklan:events", event)
		}
		out := make([]byte, 68)
		copy(out, "LLO1")
		copy(out[4:36], b[4:36])
		copy(out[36:], protocol.MAC(key, out[:36]))
		conn.WriteToUDP(out, addr)
		r.Online.Store(int64(len(r.bindings)))
	case "LLR1":
		if len(b) < protocol.RelayOverhead+protocol.HeaderSize+16 || len(b) > protocol.RelayOverhead+protocol.MaxFrame {
			return
		}
		var id protocol.ID
		copy(id[:], b[4:20])
		binding, ok := r.bindings[id]
		if !ok || binding.addr.String() != addr.String() || time.Since(binding.last) > 40*time.Second {
			r.Dropped.Add(1)
			return
		}
		a, e := r.getAuth(ctx, id)
		if e != nil || a.Generation != binding.generation {
			delete(r.bindings, id)
			r.Dropped.Add(1)
			return
		}
		source, seq, frame, valid := protocol.UnwrapRelay(b, binding.key)
		if !valid || source != id || !binding.window.Accept(seq) {
			r.Dropped.Add(1)
			return
		}
		h, e := protocol.Parse(frame)
		if e != nil || h.Source != id || h.Destination == id || h.Network.String() != a.NetworkID {
			r.Dropped.Add(1)
			return
		}
		target, ok := r.bindings[h.Destination]
		if !ok || time.Since(target.last) > 40*time.Second {
			r.Dropped.Add(1)
			return
		}
		ta, e := r.getAuth(ctx, h.Destination)
		if e != nil || ta.NetworkID != a.NetworkID || ta.Generation != target.generation {
			r.Dropped.Add(1)
			return
		}
		now := time.Now()
		binding.tokens += now.Sub(binding.tokenAt).Seconds() * r.Bandwidth
		if binding.tokens > r.Bandwidth {
			binding.tokens = r.Bandwidth
		}
		binding.tokenAt = now
		if binding.tokens < float64(len(frame)) {
			r.Dropped.Add(1)
			return
		}
		binding.tokens -= float64(len(frame))
		// VPN ciphertext is forwarded verbatim: relay cannot decrypt the payload.
		if n, e := conn.WriteToUDP(frame, target.addr); e == nil {
			r.Bytes.Add(uint64(n))
			r.Packets.Add(1)
		} else {
			r.Dropped.Add(1)
		}
	}
}
func (r *Relay) Serve(ctx context.Context, address string) error {
	addr, e := net.ResolveUDPAddr("udp4", address)
	if e != nil {
		return e
	}
	conn, e := net.ListenUDP("udp4", addr)
	if e != nil {
		return e
	}
	defer conn.Close()
	conn.SetReadBuffer(4 * 1024 * 1024)
	go func() { <-ctx.Done(); conn.Close() }()
	go r.maintenance(ctx)
	buf := make([]byte, 1600)
	for {
		n, a, e := conn.ReadFromUDP(buf)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		r.handle(ctx, conn, buf[:n], a)
	}
}
func (r *Relay) maintenance(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.mu.Lock()
			for id, b := range r.bindings {
				if time.Since(b.last) > 40*time.Second {
					delete(r.bindings, id)
				}
			}
			for id, a := range r.cache {
				if time.Since(a.at) > 5*time.Second {
					delete(r.cache, id)
				}
			}
			for ip, l := range r.limits {
				if time.Since(l.at) > time.Minute {
					delete(r.limits, ip)
				}
			}
			r.Online.Store(int64(len(r.bindings)))
			r.mu.Unlock()
			stats, _ := json.Marshal(map[string]any{"online": r.Online.Load(), "bytes": r.Bytes.Load(), "packets": r.Packets.Load(), "dropped": r.Dropped.Load(), "updated_at": time.Now()})
			r.redis.Set(ctx, "relay:stats", stats, 15*time.Second)
		}
	}
}
func (r *Relay) Metrics(w http.ResponseWriter, q *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE linklan_relay_bytes_total counter\nlinklan_relay_bytes_total %d\n# TYPE linklan_relay_packets_total counter\nlinklan_relay_packets_total %d\n# TYPE linklan_relay_dropped_total counter\nlinklan_relay_dropped_total %d\n# TYPE linklan_online_devices gauge\nlinklan_online_devices %d\n", r.Bytes.Load(), r.Packets.Load(), r.Dropped.Load(), r.Online.Load())
}
func ServeSTUN(ctx context.Context, address string) error {
	a, e := net.ResolveUDPAddr("udp4", address)
	if e != nil {
		return e
	}
	c, e := net.ListenUDP("udp4", a)
	if e != nil {
		return e
	}
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	buf := make([]byte, 513)
	limits := map[string]sourceLimit{}
	count := 0
	for {
		n, remote, e := c.ReadFromUDP(buf)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		now := time.Now()
		v, exists := limits[remote.IP.String()]
		if !exists && len(limits) >= 10000 {
			continue
		}
		if now.Sub(v.at) > time.Second {
			v = sourceLimit{at: now}
		}
		v.count++
		limits[remote.IP.String()] = v
		count++
		if count%1000 == 0 {
			for k, l := range limits {
				if now.Sub(l.at) > time.Minute {
					delete(limits, k)
				}
			}
		}
		if v.count > 20 || len(limits) > 10000 {
			continue
		}
		b, e := stun.Response(buf[:n], remote)
		if e == nil {
			c.WriteToUDP(b, remote)
		}
	}
}
func Sequence(b []byte) uint64 {
	if len(b) < 28 {
		return 0
	}
	return binary.BigEndian.Uint64(b[20:28])
}
