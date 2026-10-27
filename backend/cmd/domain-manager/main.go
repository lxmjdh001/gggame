package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"starlight/backend/internal/domains"
	"syscall"
	"time"
)

func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func run() error {
	m, e := domains.New(domains.Config{Primary: env("DOMAIN_PRIMARY", "js18188.com"), IP: env("DOMAIN_IPV4", "209.74.79.73"), StateFile: env("DOMAIN_STATE", "/var/lib/starlight-domains/state.json"), NginxFile: env("DOMAIN_NGINX", "/etc/nginx/conf.d/starlight-backups.conf"), Webroot: env("DOMAIN_WEBROOT", "/var/lib/starlight-domains/acme"), CertRoot: "/etc/letsencrypt", Root: "/opt/starlight/current/www"})
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	e = m.Reconcile(ctx)
	cancel()
	if e != nil {
		return e
	}
	socket := env("DOMAIN_SOCKET", "/run/starlight-domains/control.sock")
	// RuntimeDirectory is root-owned, group-readable. Never unlink an arbitrary
	// caller-selected path; systemd removes stale sockets when stopping the unit.
	if e = os.MkdirAll(filepath.Dir(socket), 0750); e != nil {
		return e
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer l.Close()
	if e = os.Chmod(socket, 0660); e != nil {
		return e
	}
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	stop, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()
	go func() {
		<-stop.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	return srv.Serve(l)
}
func main() {
	if e := run(); e != nil && e != http.ErrServerClosed {
		slog.Error("domain manager stopped", "error", e)
		os.Exit(1)
	}
}
