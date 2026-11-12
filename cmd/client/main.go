package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"time"

	"linklan/internal/desktop"
	"linklan/internal/model"
	"linklan/internal/tunnel"
)

func main() {
	startHidden := flag.Bool("start-hidden", false, "Start in the notification area")
	quit := flag.Bool("quit", false, "Close an existing LinkLAN instance")
	autostart := flag.String("autostart", "", "Manage startup: enable, disable or status")
	largePreview := flag.Bool("ui-preview-large", false, "Preview 50 networks and 500 participants offline")
	previewDPI := flag.Int("ui-preview-dpi", 0, "Force offline preview DPI: 96, 120, 144 or 192")
	preview := flag.Bool("ui-preview", false, "Preview the interface offline without administrator access or VPN")
	selfTest := flag.Bool("self-test", false, "Run local binary self-test")
	version := flag.Bool("version", false, "Show version")
	allowHTTP := flag.Bool("allow-http", false, "Allow insecure HTTP for isolated development networks")
	headless := flag.Bool("headless", false, "Run without GUI, using saved DPAPI profile")
	network := flag.String("network", "", "Network UUID for headless mode")
	flag.Bool("updated", false, "Internal update marker")
	flag.Parse()
	if *version {
		fmt.Println(model.Version)
		return
	}
	if *selfTest {
		if e := desktop.CheckIcon(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		block, e := aes.NewCipher(make([]byte, 32))
		if e != nil {
			os.Exit(1)
		}
		if _, e = cipher.NewGCM(block); e != nil {
			os.Exit(1)
		}
		return
	}
	if *preview || *largePreview {
		store := &tunnel.Store{Data: tunnel.Config{Theme: "dark", DeviceName: "and-home"}}
		if e := desktop.RunWithOptions(store, desktop.Options{Preview: true, StartHidden: *startHidden, LargePreview: *largePreview, PreviewDPI: *previewDPI}); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if *autostart == "status" {
		enabled, e := desktop.StartupEnabled()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println(enabled)
		return
	}
	if *autostart != "" && *autostart != "enable" && *autostart != "disable" {
		fmt.Fprintln(os.Stderr, "--autostart: enable, disable or status")
		os.Exit(2)
	}
	if !*headless && !*quit && *autostart == "" && desktop.RestoreExisting(*startHidden) {
		return
	}
	admin, e := desktop.EnsureAdmin()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if !admin {
		return
	}
	if *quit {
		if e := desktop.QuitExisting(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if *autostart != "" {
		enabled := *autostart == "enable"
		if e := desktop.ConfigureAutostart(enabled); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if store, e := tunnel.LoadStore(); e == nil {
			if e = store.Update(func(c *tunnel.Config) { c.StartAtLogon = &enabled }); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
		}
		fmt.Println("Autostart:", enabled)
		return
	}
	root, _ := os.UserConfigDir()
	logDir := filepath.Join(root, "LinkLAN")
	os.MkdirAll(logDir, 0700)
	logFile, logErr := os.OpenFile(filepath.Join(logDir, "client.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if logErr == nil {
		defer logFile.Close()
		slog.SetDefault(slog.New(slog.NewJSONHandler(logFile, nil)))
		debug.SetCrashOutput(logFile, debug.CrashOptions{})
		slog.Info("client_started", "version", model.Version)
	}
	store, e := tunnel.LoadStore()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	store.Update(func(c *tunnel.Config) { c.AllowHTTP = *allowHTTP })
	if *headless {
		cfg := store.Snapshot()
		api, e := tunnel.NewDeviceAPI(store, cfg.Server)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		ctx, c := signal.NotifyContext(context.Background(), os.Interrupt)
		defer c()
		if e = api.ConnectDevice(ctx, cfg.DeviceName); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		if cfg.FirewallAllowed {
			firewallCtx, done := context.WithTimeout(ctx, 30*time.Second)
			e = tunnel.EnsureClientFirewall(firewallCtx)
			done()
			if e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
		}
		factory := func(n model.Network) (tunnel.Adapter, error) {
			return tunnel.NewAdapterWithFirewall(n, cfg.FirewallAllowed)
		}
		engine, e := tunnel.Start(ctx, api, *network, factory, func(s string) { fmt.Println(s) })
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		defer engine.Close()
		<-engine.Done()
		return
	}
	if e = desktop.RunWithOptions(store, desktop.Options{StartHidden: *startHidden}); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
