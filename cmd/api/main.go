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

	"github.com/fairwix/MinecraftSkins-Go/internal/config"
	"github.com/fairwix/MinecraftSkins-Go/internal/httpapi"
	"github.com/fairwix/MinecraftSkins-Go/internal/postgres"
	"github.com/fairwix/MinecraftSkins-Go/internal/ratestore"
	"github.com/fairwix/MinecraftSkins-Go/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := pgxpool.New(startupCtx, cfg.DatabaseURL)
	if err == nil {
		err = pool.Ping(startupCtx)
	}
	cancelStartup()
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repository := postgres.NewRepository(pool)
	rates := ratestore.New(cfg.CoinGeckoBaseURL, logger)
	skins := service.NewSkinService(repository, rates)
	purchases := service.NewPurchaseService(repository, rates)
	api := httpapi.New(skins, purchases, rates, repository, logger, httpapi.AuthConfig{
		AdminAPIKey: cfg.AdminAPIKey, DemoUserAPIKey: cfg.DemoUserAPIKey, DemoUserID: cfg.DemoUserID,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.HTTPAddr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-shutdownCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		_ = server.Close()
	}
}
