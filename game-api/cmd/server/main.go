package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"starlight/gameapi/internal/generator"
	"starlight/gameapi/internal/httpapi"
	"starlight/gameapi/internal/scheduler"
	"starlight/gameapi/internal/storage"
	"starlight/gameapi/pkg/apikey"
	"sync"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := env("GAME_API_ADDR", "127.0.0.1:8081")
	token, err := apikey.Load(env("GAME_API_TOKEN_FILE", "data/source-api.token"), true)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	store, err := storage.Open(env("GAME_API_DB", "data/draws.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	var wg sync.WaitGroup
	wg.Add(1)
	worker := scheduler.Daily{Store: store, Generator: generator.New(), Logger: logger}
	go func() { defer wg.Done(); worker.Run(workerCtx) }()
	server := &http.Server{Handler: (&httpapi.API{Store: store, Token: token, Logger: logger}).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	logger.Info("game API started", "address", listener.Addr().String(), "next_scheduled_at", scheduler.NextDay(time.Now()), "groups", 8)
	select {
	case <-ctx.Done():
	case err = <-result:
	}
	cancelWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	wg.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
func main() {
	if err := run(); err != nil {
		slog.Error("game API stopped", "error", err)
		os.Exit(1)
	}
}
