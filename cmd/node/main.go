package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gossipdataaggregation-sdcc/internal/app"
	"gossipdataaggregation-sdcc/internal/config"
)

func main() {
	cfg, err := config.LoadFromPathOrEnv(os.Getenv("APP_CONFIG_PATH"))
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("failed to bootstrap app: %v", err)
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Run(runCtx); err != nil {
		log.Fatalf("application run failed: %v", err)
	}
}
