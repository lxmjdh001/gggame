package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"starlight/backend/internal/accounts"
	"starlight/backend/internal/engine"
	"starlight/backend/internal/httpapi"
	"starlight/backend/internal/store"
	"starlight/gameapi/pkg/apikey"
	"starlight/gameapi/pkg/client"
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
	listener, err := net.Listen("tcp", env("BACKEND_ADDR", "127.0.0.1:8082"))
	if err != nil {
		return err
	}
	defer listener.Close()
	db, err := store.Open(env("BACKEND_DB", "data/games.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	accountPath := env("ACCOUNTS_DB", "data/accounts.db")
	accountDB, err := accounts.Open(accountPath)
	if err != nil {
		return err
	}
	defer accountDB.Close()
	password := os.Getenv("ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		b := make([]byte, 20)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		password = hex.EncodeToString(b)
	}
	adminName := env("ADMIN_USERNAME", "admin")
	created, err := accountDB.Bootstrap(adminName, password)
	if err != nil {
		return err
	}
	if created && generated {
		path := filepath.Join(filepath.Dir(accountPath), "admin-bootstrap.txt")
		content := fmt.Sprintf("管理员首次登录\n用户名: %s\n密码: %s\n登录后请在个人中心修改密码。修改后本文件内容不再有效。\n", adminName, password)
		if err = os.WriteFile(path, []byte(content), 0600); err != nil {
			return err
		}
		logger.Info("administrator initialized; credentials saved locally", "path", path)
	}
	accountHTTP := &accounts.HTTP{Store: accountDB, Draws: db, SecureCookie: os.Getenv("COOKIE_SECURE") == "true", ResultAvailable: func(session, issue string) bool {
		r, e := db.Get(session, issue)
		return e == nil && r.Origin == "scheduled"
	}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	token, err := apikey.Load(env("GAME_API_TOKEN_FILE", "../game-api/data/source-api.token"), false)
	if err != nil {
		return err
	}
	engine := &engine.Engine{Store: db, Source: client.New(env("GAME_API_URL", "http://127.0.0.1:8081"), token), Logger: logger}
	if err = engine.Seed(time.Now()); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); engine.Run(runCtx) }()
	go func() { defer wg.Done(); accountDB.RunSettlement(runCtx, db, logger) }()
	srv := &http.Server{Handler: (&httpapi.API{Store: db, Accounts: accountHTTP}).Handler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ch := make(chan error, 1)
	go func() { ch <- srv.Serve(listener) }()
	logger.Info("game backend started", "address", listener.Addr().String(), "sessions", 10)
	select {
	case <-ctx.Done():
	case err = <-ch:
	}
	cancel()
	shutdownCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	shutdownErr := srv.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = srv.Close()
	}
	wg.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return shutdownErr
}
func main() {
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}
