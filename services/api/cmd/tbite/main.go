package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
	"github.com/takalawang/corporate-catering-system/services/api/internal/httpserver"
)

func main() {
	var roleStr string
	pflag.StringVar(&roleStr, "role", "api", "binary role: api|worker|scheduler")
	pflag.Parse()

	role, err := config.ParseRole(roleStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg.Role = role

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("role", string(role))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch role {
	case config.RoleAPI:
		srv := httpserver.New(cfg.HTTPAddr, logger)
		if err := srv.Run(ctx); err != nil {
			logger.Error("api shutdown", "err", err)
			os.Exit(1)
		}
	case config.RoleWorker:
		logger.Info("worker placeholder, waiting for shutdown signal (P0)")
		<-ctx.Done()
		logger.Info("worker shutting down")
	case config.RoleScheduler:
		logger.Info("scheduler placeholder, waiting for shutdown signal (P0)")
		<-ctx.Done()
		logger.Info("scheduler shutting down")
	}
}
