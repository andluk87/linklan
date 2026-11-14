//go:build windows

package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"linklan/internal/tunnel"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func startupIdentity() (string, string, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return "", "", e
	}
	sid := u.User.Sid.String()
	return "LinkLAN-" + sid, sid, nil
}
func scheduler(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	system, e := windows.GetSystemDirectory()
	if e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, filepath.Join(system, "schtasks.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Планировщик заданий Windows не ответил вовремя")
		}
		return nil, fmt.Errorf("Планировщик заданий Windows: %w", e)
	}
	return out, nil
}
func taskExists(name string) (bool, error) {
	data, e := scheduler("/Query", "/FO", "CSV", "/NH")
	if e != nil {
		return false, e
	}
	data, e = schedulerXML(data)
	if e != nil {
		return false, e
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return false, e
		}
		if len(row) > 0 && strings.EqualFold(strings.TrimPrefix(row[0], `\`), name) {
			return true, nil
		}
	}
	return false, nil
}
func schedulerXML(data []byte) ([]byte, error) {
	return normalizeSchedulerOutput(data, func(raw []byte) ([]byte, error) {
		// schtasks writes legacy text in the system OEM code page when stdout is piped.
		n, e := windows.MultiByteToWideChar(1, 0, &raw[0], int32(len(raw)), nil, 0)
		if e != nil {
			return nil, e
		}
		words := make([]uint16, n)
		_, e = windows.MultiByteToWideChar(1, 0, &raw[0], int32(len(raw)), &words[0], n)
		if e != nil {
			return nil, e
		}
		return []byte(string(utf16.Decode(words))), nil
	})
}

var startupMu sync.Mutex

func ConfigureAutostart(enable bool) error {
	startupMu.Lock()
	defer startupMu.Unlock()
	return configureAutostart(enable)
}

func ApplyAutostart(store *tunnel.Store, enable bool) error {
	startupMu.Lock()
	defer startupMu.Unlock()
	if e := configureAutostart(enable); e != nil {
		return e
	}
	return store.Update(func(c *tunnel.Config) { value := enable; c.StartAtLogon = &value })
}

func configureAutostart(enable bool) error {
	name, sid, e := startupIdentity()
	if e != nil {
		return e
	}
	exists, e := taskExists(name)
	if e != nil {
		return e
	}
	if !enable {
		if !exists {
			return nil
		}
		_, e = scheduler("/Delete", "/TN", name, "/F")
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	if exists {
		raw, e := scheduler("/Query", "/TN", name, "/XML")
		if e != nil {
			return e
		}
		var task struct {
			RegistrationInfo struct{ Description string }
			Principals       struct {
				Principal struct {
					UserID              string `xml:"UserId"`
					RunLevel, LogonType string
				}
			}
			Actions struct {
				Exec struct{ Command, Arguments string }
			}
			Settings struct{ Enabled string }
		}
		normalized, e := schedulerXML(raw)
		if e != nil {
			return e
		}
		if e = xml.Unmarshal(normalized, &task); e != nil {
			return e
		}
		ownerSID := task.Principals.Principal.UserID
		if ownerSID != sid {
			if parsed, _, _, err := windows.LookupSID("", ownerSID); err == nil {
				ownerSID = parsed.String()
			}
		}
		if !strings.HasPrefix(task.RegistrationInfo.Description, "LinkLAN:") || ownerSID != sid {
			return fmt.Errorf("Задание %s не принадлежит конфигурации LinkLAN; оно не изменено", name)
		}
		if strings.EqualFold(filepath.Clean(task.Actions.Exec.Command), filepath.Clean(exe)) && task.Actions.Exec.Arguments == "--start-hidden" && task.Principals.Principal.RunLevel == "HighestAvailable" && task.Principals.Principal.LogonType == "InteractiveToken" && task.Settings.Enabled != "false" {
			return nil
		}
	}
	doc, e := startupTaskXML(sid, exe, filepath.Dir(exe))
	if e != nil {
		return e
	}
	f, e := os.CreateTemp("", "linklan-logon-*.xml")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	words := utf16.Encode([]rune(doc))
	raw := make([]byte, 2+len(words)*2)
	raw[0], raw[1] = 0xff, 0xfe
	for i, v := range words {
		binary.LittleEndian.PutUint16(raw[2+i*2:], v)
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	_, e = scheduler("/Create", "/TN", name, "/XML", path, "/F")
	return e
}
func StartupEnabled() (bool, error) {
	name, _, e := startupIdentity()
	if e != nil {
		return false, e
	}
	return taskExists(name)
}
