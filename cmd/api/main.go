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
		log.Error("initialize database", err)

		return
	}
	defer database.Close()

	handler := graph.NewHandler(database, database)
	httpServer := server.New(cfg.HTTP, handler)

	if err = httpServer.Serve(ctx); err != nil {
		log.Error("HTTP server stopped", err)

		return
	}
}
