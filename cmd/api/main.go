package main

import (
	"context"

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

	if err = logger.Initialize(fs, cfg.Logger); err != nil {
		panic(err)
	}

	log := logger.Get("main")
	defer func() { _ = log.Close() }()

	ctx := context.Background()

	database, err := postgres.New(ctx, cfg.Database)
	if err != nil {
		//nolint:gocritic // Fatal intentionally skips deferred logger cleanup.
		log.Fatal("initialize database", err)
	}

	handler := graph.NewHandler(database, database)
	httpServer := server.New(cfg.HTTP, handler)

	err = httpServer.Serve(ctx)

	database.Close()

	if err != nil {
		log.Fatal("HTTP server stopped", err)
	}
}
