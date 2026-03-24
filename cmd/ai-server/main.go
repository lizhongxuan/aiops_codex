package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	"github.com/lizhongxuan/aiops-codex/internal/config"
	"github.com/lizhongxuan/aiops-codex/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	app := server.New(cfg)
	if err := app.Start(ctx); err != nil {
		log.Fatalf("failed to start app: %v", err)
	}
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("server exited with error: %v", err)
	}
}
