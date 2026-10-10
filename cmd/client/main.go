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
		block, e := aes.NewCipher(make([]byte, 32))
		if e != nil {
			os.Exit(1)
		}
		if _, e = cipher.NewGCM(block); e != nil {
			os.Exit(1)
		}
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
	if e = desktop.Run(store); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
