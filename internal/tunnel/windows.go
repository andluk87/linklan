//go:build windows

package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"linklan/internal/model"
	"linklan/internal/protocol"
)

type wintunAdapter struct {
	mu                                           sync.Mutex
	dll                                          *syscall.DLL
	adapter, session, event                      uintptr
	receive, release, allocate, send, end, close *syscall.Proc
	closed                                       bool
	index                                        uint32
	name                                         string
	networkID                                    string
}

func NewAdapter(n model.Network) (Adapter, error) {
	return NewAdapterWithFirewall(n, false)
}
func NewAdapterWithFirewall(n model.Network, allowAll bool) (Adapter, error) {
	if _, e := protocol.ParseID(n.ID); e != nil {
		return nil, e
	}
	ip := net.ParseIP(n.IP)
	_, sub, e := net.ParseCIDR(n.Subnet)
	if e != nil || ip == nil || ip.To4() == nil || !sub.Contains(ip) {
		return nil, fmt.Errorf("Некорректный виртуальный IPv4")
	}
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	dll, e := syscall.LoadDLL(filepath.Join(filepath.Dir(exe), "wintun.dll"))
	if e != nil {
		return nil, fmt.Errorf("Нет wintun.dll рядом с LinkLAN.exe: %w", e)
	}
	a := &wintunAdapter{dll: dll, name: "LinkLAN-" + n.ID[:8], networkID: n.ID}
	cleanup := func(e error) (Adapter, error) { a.Close(); return nil, e }
	a.close = dll.MustFindProc("WintunCloseAdapter")
	a.end = dll.MustFindProc("WintunEndSession")
	a.receive = dll.MustFindProc("WintunReceivePacket")
	a.release = dll.MustFindProc("WintunReleaseReceivePacket")
	a.allocate = dll.MustFindProc("WintunAllocateSendPacket")
	a.send = dll.MustFindProc("WintunSendPacket")
	name, _ := windows.UTF16PtrFromString(a.name)
	pool, _ := windows.UTF16PtrFromString("LinkLAN")
	a.adapter, _, _ = dll.MustFindProc("WintunOpenAdapter").Call(uintptr(unsafe.Pointer(name)))
	if a.adapter == 0 {
		a.adapter, _, e = dll.MustFindProc("WintunCreateAdapter").Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(pool)), 0)
	}
	if a.adapter == 0 {
		return cleanup(fmt.Errorf("Не удалось создать Wintun: запустите клиент от администратора (%v)", e))
	}
	var luid uint64
	dll.MustFindProc("WintunGetAdapterLUID").Call(a.adapter, uintptr(unsafe.Pointer(&luid)))
	iphlp := windows.NewLazySystemDLL("iphlpapi.dll")
	result, _, _ := iphlp.NewProc("ConvertInterfaceLuidToIndex").Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&a.index)))
	if result != 0 {
		return cleanup(fmt.Errorf("Не удалось определить индекс адаптера"))
	}
	a.session, _, e = dll.MustFindProc("WintunStartSession").Call(a.adapter, 4*1024*1024)
	if a.session == 0 {
		return cleanup(fmt.Errorf("Не удалось начать сеанс Wintun: %v", e))
	}
	a.event, _, _ = dll.MustFindProc("WintunGetReadWaitEvent").Call(a.session)
	// Only this virtual interface is modified. No default route or DNS override.
	command := fmt.Sprintf(`$ErrorActionPreference='Stop'; $idx=%d; $sub='%s'; $other=Get-NetRoute -AddressFamily IPv4 -DestinationPrefix $sub -ErrorAction SilentlyContinue | Where-Object {$_.InterfaceIndex -ne $idx}; if($other){throw 'Virtual subnet overlaps an existing route'}; Get-NetIPAddress -InterfaceIndex $idx -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue; New-NetIPAddress -InterfaceIndex $idx -IPAddress '%s' -PrefixLength 24 | Out-Null; Set-NetIPInterface -InterfaceIndex $idx -AddressFamily IPv4 -NlMtuBytes %d -InterfaceMetric 5; Set-DnsClient -InterfaceIndex $idx -RegisterThisConnectionsAddress $false; `, a.index, sub.String(), ip.To4().String(), protocol.MTU)
	firewall, err := virtualFirewallScript(n, allowAll)
	if err != nil {
		return cleanup(err)
	}
	command += firewall
	if e = powerShell(command); e != nil {
		return cleanup(e)
	}
	return a, nil
}
func powerShell(command string) error {
	ctx, c := context.WithTimeout(context.Background(), 25*time.Second)
	defer c()
	return runPowerShell(ctx, command)
}
func runPowerShell(ctx context.Context, command string) error {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("Настройка Windows: %s (%v)", strings.TrimSpace(string(out)), e)
	}
	return nil
}
func EnsureClientFirewall(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return runPowerShell(ctx, clientFirewallScript(filepath.Clean(exe)))
}
func (a *wintunAdapter) Read(buf []byte) (int, error) {
	for {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return 0, fmt.Errorf("adapter closed")
		}
		var size uint32
		ptr, _, err := a.receive.Call(a.session, uintptr(unsafe.Pointer(&size)))
		if ptr != 0 {
			if int(size) > len(buf) {
				a.release.Call(a.session, ptr)
				a.mu.Unlock()
				continue
			}
			copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)))
			a.release.Call(a.session, ptr)
			a.mu.Unlock()
			return int(size), nil
		}
		event := a.event
		a.mu.Unlock()
		if err != windows.ERROR_NO_MORE_ITEMS {
			return 0, fmt.Errorf("Wintun receive: %v", err)
		}
		windows.WaitForSingleObject(windows.Handle(event), 200)
	}
}
func (a *wintunAdapter) Write(b []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return fmt.Errorf("adapter closed")
	}
	ptr, _, err := a.allocate.Call(a.session, uintptr(len(b)))
	if ptr == 0 {
		return fmt.Errorf("Wintun send: %v", err)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(b)), b)
	a.send.Call(a.session, ptr)
	return nil
}
func (a *wintunAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	a.closed = true
	if a.index != 0 {
		powerShell(fmt.Sprintf(`Remove-NetFirewallRule -Name %s,%s -ErrorAction SilentlyContinue; Remove-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue; Get-NetIPAddress -InterfaceIndex %d -AddressFamily IPv4 -ErrorAction SilentlyContinue | Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue`, psLiteral("LinkLAN-VPN-"+a.networkID+"-Inbound"), psLiteral("LinkLAN-VPN-"+a.networkID+"-Outbound"), psLiteral(a.name+" ICMP"), a.index))
	}
	if a.session != 0 {
		a.end.Call(a.session)
	}
	if a.adapter != 0 {
		a.close.Call(a.adapter)
	}
	if a.dll != nil {
		a.dll.Release()
	}
	return nil
}
func configPath() (string, error) {
	root, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	dir := filepath.Join(root, "LinkLAN")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	return filepath.Join(dir, "profile.dpapi"), nil
}
func protect(b []byte, encrypt bool) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("empty DPAPI input")
	}
	input := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	var e error
	if encrypt {
		e = windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		e = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if e != nil {
		return nil, e
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}
func LoadStore() (*Store, error) {
	path, e := configPath()
	if e != nil {
		return nil, e
	}
	c := Config{DeviceName: DefaultDeviceName(), Theme: "dark", AutoConnect: true, Profiles: map[string]Profile{}}
	b, e := os.ReadFile(path)
	if e == nil {
		clear, e := protect(b, false)
		if e != nil {
			return nil, fmt.Errorf("Не удалось расшифровать профиль DPAPI: %w", e)
		}
		if e = json.Unmarshal(clear, &c); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	s := &Store{Data: c}
	s.Save = func(c Config) error {
		b, e := json.Marshal(c)
		if e != nil {
			return e
		}
		b, e = protect(b, true)
		if e != nil {
			return e
		}
		tmp := path + ".tmp"
		if e = os.WriteFile(tmp, b, 0600); e != nil {
			return e
		}
		return windows.MoveFileEx(windows.StringToUTF16Ptr(tmp), windows.StringToUTF16Ptr(path), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	}
	return s, nil
}
func computerName() string {
	s, _ := os.Hostname()
	if s == "" {
		return "Windows PC"
	}
	return s
}
