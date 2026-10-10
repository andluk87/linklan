package updates

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PublicKey is pinned at build time. An empty key disables installation.
var PublicKey string

type Manifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

func (m Manifest) Message() []byte {
	return []byte("LinkLAN update|" + m.Version + "|" + m.URL + "|" + m.SHA256 + "|" + strconv.FormatInt(m.Size, 10))
}
func (m Manifest) Verify(public string) error {
	p, e := base64.RawStdEncoding.DecodeString(public)
	if e != nil || len(p) != 32 {
		return fmt.Errorf("Подпись обновлений не настроена в этой сборке")
	}
	sig, e := base64.RawStdEncoding.DecodeString(m.Signature)
	h, he := hex.DecodeString(m.SHA256)
	if e != nil || he != nil || len(h) != 32 || m.Size < 1 || m.Size > 64*1024*1024 || !ed25519.Verify(p, m.Message(), sig) {
		return fmt.Errorf("Подпись пакета обновления недействительна")
	}
	return nil
}
func Newer(candidate, current string) bool {
	a, e := semver(candidate)
	if e != nil {
		return false
	}
	b, e := semver(current)
	if e != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
func semver(s string) ([3]int, error) {
	var v [3]int
	p := strings.Split(s, ".")
	if len(p) != 3 {
		return v, fmt.Errorf("invalid version")
	}
	for i := range v {
		if p[i] == "" || (len(p[i]) > 1 && p[i][0] == '0') {
			return v, fmt.Errorf("invalid version")
		}
		for _, c := range p[i] {
			if c < '0' || c > '9' {
				return v, fmt.Errorf("invalid version")
			}
		}
		n, e := strconv.Atoi(p[i])
		if e != nil || n < 0 || n > 100000 {
			return v, fmt.Errorf("invalid version")
		}
		v[i] = n
	}
	return v, nil
}
func Check(ctx context.Context, server string) (Manifest, error) {
	var m Manifest
	u, e := url.Parse(server)
	if e != nil || u.Scheme != "https" || u.Host == "" {
		return m, fmt.Errorf("Обновления загружаются только через HTTPS")
	}
	u.Path = "/downloads/latest.json"
	u.RawQuery = ""
	r, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return m, e
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}}
	resp, e := client.Do(r)
	if e != nil {
		return m, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return m, fmt.Errorf("Манифест обновлений пока не опубликован")
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&m); e != nil {
		return m, e
	}
	return m, m.Verify(PublicKey)
}
func Stage(ctx context.Context, server string, m Manifest) (string, error) {
	if e := m.Verify(PublicKey); e != nil {
		return "", e
	}
	base, e := url.Parse(server)
	if e != nil || base.Scheme != "https" {
		return "", fmt.Errorf("HTTPS required")
	}
	relative, e := url.Parse(m.URL)
	if e != nil || relative.IsAbs() || relative.Host != "" || !strings.HasPrefix(relative.Path, "/downloads/") || relative.RawQuery != "" {
		return "", fmt.Errorf("Некорректный адрес обновления")
	}
	u := base.ResolveReference(relative)
	dir, e := os.MkdirTemp("", "linklan-update-")
	if e != nil {
		return "", e
	}
	failed := true
	defer func() {
		if failed {
			os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, "update.zip")
	r, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return "", e
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != u.Host || req.URL.Scheme != "https" || len(via) > 3 {
			return fmt.Errorf("redirect rejected")
		}
		return nil
	}}
	resp, e := client.Do(r)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Ошибка загрузки обновления")
	}
	f, e := os.Create(path)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, m.Size+1))
	closeErr := f.Close()
	if e != nil || closeErr != nil || n != m.Size || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return "", fmt.Errorf("Размер или контрольная сумма обновления не совпадают")
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		return "", e
	}
	defer z.Close()
	found := map[string]bool{}
	var total uint64
	for _, item := range z.File {
		if item.Name != "LinkLAN.exe" && item.Name != "wintun.dll" {
			continue
		}
		if found[item.Name] || !item.Mode().IsRegular() || item.UncompressedSize64 > 48*1024*1024 {
			return "", fmt.Errorf("Некорректная структура пакета")
		}
		total += item.UncompressedSize64
		if total > 64*1024*1024 {
			return "", fmt.Errorf("Пакет слишком большой")
		}
		in, e := item.Open()
		if e != nil {
			return "", e
		}
		out, e := os.OpenFile(filepath.Join(dir, item.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			in.Close()
			return "", e
		}
		n, e = io.Copy(out, io.LimitReader(in, int64(item.UncompressedSize64)+1))
		out.Close()
		in.Close()
		if e != nil || uint64(n) != item.UncompressedSize64 {
			return "", fmt.Errorf("Повреждённый ZIP")
		}
		found[item.Name] = true
	}
	if !found["LinkLAN.exe"] || !found["wintun.dll"] {
		return "", fmt.Errorf("В пакете нет клиента или Wintun")
	}
	failed = false
	return dir, nil
}
