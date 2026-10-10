//go:build windows

package updates

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func Install(staged string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	cmd := exec.Command(filepath.Join(staged, "LinkLAN.exe"), "--self-test")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("Новая версия не прошла самопроверку")
	}
	dir := filepath.Dir(exe)
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; $target=%s; $stage=%s; Wait-Process -Id %d -Timeout 60 -ErrorAction SilentlyContinue; try { foreach($file in @('LinkLAN.exe','wintun.dll')) { Copy-Item (Join-Path $target $file) (Join-Path $target ($file+'.bak')) -Force; Copy-Item (Join-Path $stage $file) (Join-Path $target $file) -Force }; $app=Start-Process (Join-Path $target 'LinkLAN.exe') -ArgumentList '--updated' -PassThru; Start-Sleep -Seconds 10; if($app.HasExited){throw 'New version exited'} } catch { foreach($file in @('LinkLAN.exe','wintun.dll')) { $backup=Join-Path $target ($file+'.bak'); if(Test-Path $backup){Copy-Item $backup (Join-Path $target $file) -Force} }; Start-Process (Join-Path $target 'LinkLAN.exe') }; Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue`, psQuote(dir), psQuote(staged), os.Getpid())
	path := filepath.Join(staged, "install.ps1")
	if e = os.WriteFile(path, []byte(script), 0600); e != nil {
		return e
	}
	run := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
	run.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return run.Start()
}
