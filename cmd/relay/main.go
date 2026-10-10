package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"linklan/internal/relay"
)

func main() {
	ctx, c := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer c()
	o, e := redis.ParseURL(os.Getenv("REDIS_URL"))
	if e != nil {
		panic(e)
	}
	rc := redis.NewClient(o)
	defer rc.Close()
	r, e := relay.New(rc, []byte(os.Getenv("RELAY_COOKIE_SECRET")))
	if e != nil {
		panic(e)
	}
	if v, e := strconv.ParseFloat(os.Getenv("RELAY_BYTES_PER_SECOND"), 64); e == nil && v >= 1024 {
		r.Bandwidth = v
	}
	go func() {
		if e := relay.ServeSTUN(ctx, ":3478"); e != nil {
			slog.Error("stun_failed")
			c()
		}
	}()
	m := http.NewServeMux()
	m.HandleFunc("GET /metrics", r.Metrics)
	m.HandleFunc("GET /health", func(w http.ResponseWriter, q *http.Request) {
		if rc.Ping(q.Context()).Err() != nil {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte("ok"))
	})
	hs := &http.Server{Addr: ":9091", Handler: m, ReadHeaderTimeout: 5 * time.Second}
	go func() { hs.ListenAndServe() }()
	go func() { <-ctx.Done(); hs.Close() }()
	if e = r.Serve(ctx, ":51820"); e != nil {
		slog.Error("relay_failed")
		os.Exit(1)
	}
}
