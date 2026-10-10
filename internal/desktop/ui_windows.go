//go:build windows

package desktop

import (
	"fmt"
	"golang.org/x/sys/windows"
	"net/url"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var user = windows.NewLazySystemDLL("user32.dll")
var kernel = windows.NewLazySystemDLL("kernel32.dll")
var gdi = windows.NewLazySystemDLL("gdi32.dll")

const appMessage = 0x8001

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}

func wide(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

//go:uintptrescapes
func call(name string, args ...uintptr) uintptr {
	v, _, _ := user.NewProc(name).Call(args...)
	return v
}
func text(h uintptr) string {
	n := call("GetWindowTextLengthW", h)
	b := make([]uint16, n+1)
	call("GetWindowTextW", h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return windows.UTF16ToString(b)
}
func set(h uintptr, s string) { call("SetWindowTextW", h, uintptr(unsafe.Pointer(wide(s)))) }

//go:uintptrescapes
func send(h uintptr, msg uint32, w, l uintptr) uintptr {
	return call("SendMessageW", h, uintptr(msg), w, l)
}
func notice(h uintptr, title, body string) {
	call("MessageBoxW", h, uintptr(unsafe.Pointer(wide(body))), uintptr(unsafe.Pointer(wide(title))), 0)
}
func EnsureAdmin() (bool, error) {
	shell := windows.NewLazySystemDLL("shell32.dll")
	v, _, _ := shell.NewProc("IsUserAnAdmin").Call()
	if v != 0 {
		return true, nil
	}
	exe, e := os.Executable()
	if e != nil {
		return false, e
	}
	var args []string
	for _, arg := range os.Args[1:] {
		args = append(args, syscall.EscapeArg(arg))
	}
	v, _, _ = shell.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(wide("runas"))), uintptr(unsafe.Pointer(wide(exe))), uintptr(unsafe.Pointer(wide(strings.Join(args, " ")))), 0, 1)
	if v <= 32 {
		return false, fmt.Errorf("Для Wintun требуется запуск от имени администратора")
	}
	return false, nil
}
func clipboard(h uintptr, value string) error {
	if call("OpenClipboard", h) == 0 {
		return fmt.Errorf("Буфер обмена занят")
	}
	defer call("CloseClipboard")
	call("EmptyClipboard")
	b, _ := windows.UTF16FromString(value)
	memory, _, _ := kernel.NewProc("GlobalAlloc").Call(2, uintptr(len(b)*2))
	if memory == 0 {
		return fmt.Errorf("Недостаточно памяти")
	}
	ptr, _, _ := kernel.NewProc("GlobalLock").Call(memory)
	if ptr == 0 {
		return fmt.Errorf("Буфер обмена недоступен")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(b)), b)
	kernel.NewProc("GlobalUnlock").Call(memory)
	if call("SetClipboardData", 13, memory) == 0 {
		kernel.NewProc("GlobalFree").Call(memory)
		return fmt.Errorf("Не удалось скопировать")
	}
	return nil
}
func urlPath(s string) string { return url.PathEscape(s) }
