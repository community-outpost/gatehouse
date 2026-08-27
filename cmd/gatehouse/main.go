package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/community-outpost/gatehouse/internal/callback"
	"github.com/community-outpost/gatehouse/internal/config"
	"github.com/community-outpost/gatehouse/internal/discovery"
	"github.com/community-outpost/gatehouse/internal/httpapi"
	"github.com/community-outpost/gatehouse/internal/mysqlstore"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := sqlx.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		logger.Error("open MySQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), cfg.MySQL.StartupTimeout.Duration)
	if err := db.PingContext(startupCtx); err != nil {
		cancelStartup()
		logger.Error("connect to MySQL", "error", err)
		os.Exit(1)
	}
	cancelStartup()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	resolver, err := discovery.New(cfg.Backends.Docker, cfg.Backends.Static, logger)
	if err != nil {
		logger.Error("configure Docker discovery", "error", err)
		os.Exit(1)
	}
	resolver.Start(ctx)

	store := mysqlstore.New(db, cfg.MySQL.UsersTable, cfg.MySQL.PendingLoginsTable, cfg.MySQL.AdvisoryLockTimeoutSeconds)
	principalResolver := mysqlstore.NewPrincipalResolver(
		db, cfg.MySQL.UsersTable, cfg.MySQL.LoginPrincipalsTable, cfg.MySQL.AdvisoryLockTimeoutSeconds)
	forwarder := callback.NewHTTPForwarder(resolver, cfg.BackendTimeout.Duration, logger)
	dispatcher := callback.NewService(store, forwarder, logger)
	api, err := httpapi.New(
		dispatcher,
		store,
		cfg.InboundAPIKey,
		cfg.MaxCallbackBodyBytes,
		cfg.TrustedProxies,
		logger,
	)
	if err != nil {
		logger.Error("configure trusted proxies", "error", err)
		os.Exit(1)
	}
	if err := configureLogin(api, cfg, principalResolver); err != nil {
		logger.Error("configure authentication", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.BackendTimeout.Duration + 10*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout.Duration)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP shutdown failed", "error", err)
		}
	}()

	logger.Info("gatehouse listening", "address", cfg.ListenAddress, "configured_static_backends", len(cfg.Backends.Static))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
}
