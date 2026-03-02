package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/estbndlt/fridge-flow/internal/auth"
	"github.com/estbndlt/fridge-flow/internal/config"
	"github.com/estbndlt/fridge-flow/internal/db"
	"github.com/estbndlt/fridge-flow/internal/repository"
	"github.com/estbndlt/fridge-flow/internal/service"
	"github.com/estbndlt/fridge-flow/internal/web"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("open database: %v", err)
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.Migrate(ctx, database, cfg.MigrationsDir); err != nil {
		cancel()
		logger.Fatalf("run migrations: %v", err)
	}
	cancel()

	repo := repository.New(database)
	authClient := auth.NewGoogleClient(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
	authService := service.NewAuthService(repo, authClient, cfg.SessionTTL)
	shoppingService := service.NewShoppingService(repo)

	server, err := web.NewServer(cfg, authService, shoppingService, logger)
	if err != nil {
		logger.Fatalf("create server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("fridge-flow listening on %s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("serve http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("shutdown error: %v", err)
	}
}
