package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/server"
	"github.com/spf13/afero"
)

var configPath = "./config/config.yaml"

func main() {
	fs := afero.NewOsFs()

	cfg, err := config.Load(fs, configPath)
	if err != nil {
		panic(err)
	}

	if err = logger.Initialize(cfg.Logger); err != nil {
		panic(err)
	}

	log := logger.Get("main")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	database, err := postgres.New(ctx, cfg.Database)
	if err != nil {
		stop()
		log.Fatal("initialize database", err)
	}

	handler := graph.NewHandler(database, database)
	httpServer := server.New(cfg.HTTP, handler)

	err = httpServer.Serve(ctx)

	database.Close()
	stop()

	if err != nil {
		log.Fatal("HTTP server stopped", err)
	}

	if err = log.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "close logger:", err)
		os.Exit(1)
	}
}
