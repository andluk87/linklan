package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"linklan/internal/backend"
)

func main() {
	admin := flag.String("make-admin", "", "Promote an existing registered email; exits without starting HTTP")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *admin != "" {
		db, e := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
		if e != nil {
			slog.Error("database_unavailable")
			os.Exit(1)
		}
		defer db.Close()
		result, e := db.Exec(ctx, `UPDATE users SET admin=true WHERE email=lower($1)`, *admin)
		if e != nil || result.RowsAffected() != 1 {
			slog.Error("registered_user_not_found")
			os.Exit(1)
		}
		slog.Info("admin_promoted")
		return
	}
	startup, c := context.WithTimeout(ctx, 30*time.Second)
	defer c()
	s, e := backend.New(startup, backend.EnvConfig())
	if e != nil {
		slog.Error("startup_failed", "error", e)
		os.Exit(1)
	}
	defer s.Close()
	httpServer := &http.Server{Addr: ":8080", Handler: s.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		httpServer.Shutdown(shutdown)
	}()
	slog.Info("api_started", "address", ":8080")
	if e = httpServer.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("api_stopped")
		os.Exit(1)
	}
}
