package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/graph"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/postgres"
)

func main() {
	cfg, err := config.Load("config.yaml")
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

	handler := graph.NewHandler(database)
	mux := http.NewServeMux()
	mux.Handle("/graphql", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCtx, cancel := context.WithTimeout(request.Context(), cfg.HTTP.RequestTimeout)
		defer cancel()

		handler.ServeHTTP(writer, request.WithContext(requestCtx))
	}))

	server := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	log.Info("HTTP server starting", logger.String("address", cfg.HTTP.Addr))

	err = serve(ctx, server, cfg.HTTP.ShutdownTimeout)

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

func serve(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	err := server.Shutdown(shutdownCtx)
	if err != nil {
		closeErr := server.Close()

		<-serverErrors

		return errors.Join(fmt.Errorf("shutdown HTTP: %w", err), closeErr)
	}

	if err = <-serverErrors; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}

	return nil
}
