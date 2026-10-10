package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"linklan/internal/model"
)

type Profile struct {
	DeviceID   string            `json:"device_id"`
	PrivateKey string            `json:"private_key"`
	Refresh    string            `json:"refresh_token"`
	PeerKeys   map[string]string `json:"peer_keys"`
}
type Config struct {
	Server, Email, DeviceName string
	Theme                     string
	AutoConnect               bool
	FirewallAllowed           bool
	NetworkID                 string
	AllowHTTP                 bool `json:"-"`
	Profiles                  map[string]Profile
}
type Store struct {
	mu   sync.Mutex
	Data Config
	Save func(Config) error
}

func (s *Store) Snapshot() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.Data)
	var c Config
	json.Unmarshal(b, &c)
	c.AllowHTTP = s.Data.AllowHTTP
	return c
}
func (s *Store) Update(f func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.Data)
	var next Config
	json.Unmarshal(b, &next)
	next.AllowHTTP = s.Data.AllowHTTP
	f(&next)
	if s.Save != nil {
		if e := s.Save(next); e != nil {
			return e
		}
	}
	s.Data = next
	return nil
}

type APIError struct {
	Status  int
	Message string
}

func (e APIError) Error() string { return e.Message }

type API struct {
	Base, profileKey string
	store            *Store
	http             *http.Client
	mu               sync.Mutex
	access           string
	expires          time.Time
	User             model.User
	deviceMode       bool
}

func NormalizeServer(server string) (string, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return "", fmt.Errorf("Укажите домен сервера, например linklan.lknet.ru")
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, e := url.Parse(server)
	if e != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/api") {
		return "", fmt.Errorf("Укажите только домен сервера и, при необходимости, порт")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("Сервер должен использовать HTTPS")
	}
	if u.Port() == "443" && u.Scheme == "https" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	u.Path, u.RawPath = "", ""
	return u.String(), nil
}

func DefaultDeviceName() string {
	name, _ := os.Hostname()
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == '|' {
			return -1
		}
		return r
	}, name))
	for len(name) > 64 {
		rr := []rune(name)
		name = string(rr[:len(rr)-1])
	}
	if name == "" {
		return "Windows PC"
	}
	return name
}

func NewAPI(store *Store, server, email string) (*API, error) {
	cfg := store.Snapshot()
	normalized, e := NormalizeServer(server)
	if e != nil {
		return nil, e
	}
	u, _ := url.Parse(normalized)
	host := u.Hostname()
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (local || cfg.AllowHTTP)) {
		return nil, fmt.Errorf("Требуется HTTPS. Для тестовой LAN используйте запуск с --allow-http")
	}
	if u.Path != "" && u.Path != "/api" {
		return nil, fmt.Errorf("Адрес сервера должен содержать только домен и порт")
	}
	u.Path = "/api"
	a := &API{Base: u.String(), profileKey: u.String() + "|" + strings.ToLower(strings.TrimSpace(email)), store: store, http: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}}}
	return a, nil
}

// NewDeviceAPI uses one identity per server. The old profile on the same
// server is copied once, so an upgrade retains devices, networks and ownership.
func NewDeviceAPI(store *Store, server string) (*API, error) {
	a, e := NewAPI(store, server, "")
	if e != nil {
		return nil, e
	}
	a.deviceMode = true
	a.profileKey = "device|" + a.Base
	cfg := store.Snapshot()
	if cfg.Profiles[a.profileKey].PrivateKey == "" && cfg.Email != "" {
		legacy := cfg.Profiles[a.Base+"|"+strings.ToLower(strings.TrimSpace(cfg.Email))]
		if legacy.PrivateKey != "" {
			if e = a.putProfile(func(p *Profile) { *p = legacy }); e != nil {
				return nil, e
			}
		}
	}
	return a, nil
}
func (a *API) ConnectDevice(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deviceAccess(ctx, name)
}
func (a *API) deviceAccess(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultDeviceName()
	}
	if len(name) > 64 || strings.ContainsAny(name, "|\r\n\x00") {
		return fmt.Errorf("Имя компьютера: 1–64 байта, без управляющих символов")
	}
	p := a.Profile()
	if p.PrivateKey == "" {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		if e = a.putProfile(func(p *Profile) { p.PrivateKey = base64.RawStdEncoding.EncodeToString(key) }); e != nil {
			return e
		}
		p = a.Profile()
	}
	key, e := base64.RawStdEncoding.DecodeString(p.PrivateKey)
	if e != nil || len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("Повреждён сохранённый ключ устройства")
	}
	private := ed25519.PrivateKey(key)
	public := base64.RawStdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))
	var challenge struct{ Challenge string }
	if e = a.raw(ctx, "POST", "/auth/device/challenge", "", map[string]string{"public_key": public}, &challenge); e != nil {
		if ae, ok := e.(APIError); ok && ae.Status == 404 {
			return fmt.Errorf("Обновите сервер LinkLAN до версии 0.2.0: он ещё не поддерживает подключение без аккаунта")
		}
		return e
	}
	if len(challenge.Challenge) != 43 {
		return fmt.Errorf("Некорректный ответ сервера при подключении устройства")
	}
	signature := base64.RawStdEncoding.EncodeToString(ed25519.Sign(private, []byte(model.DeviceProof(challenge.Challenge, public, name, model.Version))))
	var access model.DeviceAccess
	if e = a.raw(ctx, "POST", "/auth/device", "", map[string]string{"name": name, "public_key": public, "challenge": challenge.Challenge, "signature": signature, "version": model.Version}, &access); e != nil {
		return e
	}
	if access.Device.ID == "" || access.Device.PublicKey != public || access.Device.UserID != access.User.ID || access.Access == "" || access.Refresh == "" {
		return fmt.Errorf("Сервер вернул некорректные данные устройства")
	}
	if e = a.putProfile(func(p *Profile) { p.DeviceID = access.Device.ID; p.Refresh = access.Refresh }); e != nil {
		return e
	}
	a.access, a.expires, a.User = access.Access, time.Now().Add(13*time.Minute), access.User
	return nil
}
func (a *API) Profile() Profile { c := a.store.Snapshot(); return c.Profiles[a.profileKey] }
func (a *API) putProfile(f func(*Profile)) error {
	return a.store.Update(func(c *Config) {
		if c.Profiles == nil {
			c.Profiles = map[string]Profile{}
		}
		p := c.Profiles[a.profileKey]
		f(&p)
		c.Profiles[a.profileKey] = p
	})
}
func (a *API) raw(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, a.Base+path, reader)
	if e != nil {
		return e
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := a.http.Do(r)
	if e != nil {
		return fmt.Errorf("Не удалось связаться с сервером: %w", e)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if e != nil {
		return e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var v struct{ Error string }
		json.Unmarshal(b, &v)
		if v.Error == "" {
			v.Error = fmt.Sprintf("Сервер вернул HTTP %d", resp.StatusCode)
		}
		return APIError{resp.StatusCode, v.Error}
	}
	if out != nil {
		if e = json.Unmarshal(b, out); e != nil {
			return fmt.Errorf("Некорректный ответ сервера")
		}
	}
	return nil
}
func (a *API) renew(ctx context.Context) error {
	p := a.Profile()
	if p.Refresh == "" {
		if a.deviceMode {
			return a.deviceAccess(ctx, a.store.Snapshot().DeviceName)
		}
		return APIError{401, "Войдите в аккаунт"}
	}
	var t model.Tokens
	if e := a.raw(ctx, "POST", "/auth/refresh", "", map[string]string{"refresh_token": p.Refresh}, &t); e != nil {
		if ae, ok := e.(APIError); ok && ae.Status == 401 && a.deviceMode {
			return a.deviceAccess(ctx, a.store.Snapshot().DeviceName)
		}
		return e
	}
	a.access = t.Access
	a.expires = time.Now().Add(13 * time.Minute)
	a.User = t.User
	return a.putProfile(func(p *Profile) { p.Refresh = t.Refresh })
}
func (a *API) Call(ctx context.Context, method, path string, body, out any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.access == "" || time.Now().After(a.expires) {
		if e := a.renew(ctx); e != nil {
			return e
		}
	}
	e := a.raw(ctx, method, path, a.access, body, out)
	if ae, ok := e.(APIError); ok && ae.Status == 401 {
		if e = a.renew(ctx); e != nil {
			return e
		}
		return a.raw(ctx, method, path, a.access, body, out)
	}
	return e
}
func (a *API) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.access == "" || time.Now().After(a.expires) {
		if e := a.renew(ctx); e != nil {
			return "", e
		}
	}
	return a.access, nil
}
func (a *API) Authenticate(ctx context.Context, email, password, name string, register bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	path := "/auth/login"
	body := map[string]string{"email": email, "password": password}
	if register {
		path = "/auth/register"
		body["name"] = name
	}
	var t model.Tokens
	if e := a.raw(ctx, "POST", path, "", body, &t); e != nil {
		return e
	}
	a.access = t.Access
	a.expires = time.Now().Add(13 * time.Minute)
	a.User = t.User
	if e := a.putProfile(func(p *Profile) { p.Refresh = t.Refresh }); e != nil {
		return e
	}
	return nil
}
func (a *API) RegisterDevice(ctx context.Context, name string) (model.Device, error) {
	p := a.Profile()
	if p.PrivateKey == "" {
		_, key, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return model.Device{}, e
		}
		if e = a.putProfile(func(p *Profile) { p.PrivateKey = base64.RawStdEncoding.EncodeToString(key) }); e != nil {
			return model.Device{}, e
		}
		p = a.Profile()
	}
	key, e := base64.RawStdEncoding.DecodeString(p.PrivateKey)
	if e != nil || len(key) != ed25519.PrivateKeySize {
		return model.Device{}, fmt.Errorf("Повреждён локальный ключ устройства")
	}
	pub := base64.RawStdEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey))
	signature := ed25519.Sign(ed25519.PrivateKey(key), []byte("LinkLAN registration|"+name+"|"+pub))
	var d model.Device
	e = a.Call(ctx, "POST", "/devices/register", map[string]string{"name": name, "public_key": pub, "signature": base64.RawStdEncoding.EncodeToString(signature), "version": model.Version}, &d)
	if e != nil {
		return d, e
	}
	e = a.putProfile(func(p *Profile) { p.DeviceID = d.ID })
	return d, e
}
func (a *API) Logout(ctx context.Context) error {
	e := a.Call(ctx, "POST", "/auth/logout", map[string]string{}, nil)
	a.mu.Lock()
	a.access = ""
	a.mu.Unlock()
	a.putProfile(func(p *Profile) { p.Refresh = "" })
	return e
}
func (a *API) Forgot(ctx context.Context, email string) error {
	var v any
	return a.raw(ctx, "POST", "/auth/forgot", "", map[string]string{"email": email}, &v)
}
func (a *API) Reset(ctx context.Context, token, password string) error {
	return a.raw(ctx, "POST", "/auth/reset", "", map[string]string{"token": token, "password": password}, nil)
}
func (a *API) Pin(id, key string) error {
	p := a.Profile()
	if old := p.PeerKeys[id]; old != "" && old != key {
		return fmt.Errorf("Ключ устройства %s изменился: соединение отклонено", id)
	}
	if p.PeerKeys[id] == key {
		return nil
	}
	return a.putProfile(func(p *Profile) {
		if p.PeerKeys == nil {
			p.PeerKeys = map[string]string{}
		}
		p.PeerKeys[id] = key
	})
}
