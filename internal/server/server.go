package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/EgorKo25/main-satellite-graphql-api/internal/logger"
)

func New(cfg config.HTTP, handler http.Handler) *Server {
	mux := http.NewServeMux()
	mux.Handle("/graphql", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), cfg.RequestTimeout)
		defer cancel()

		handler.ServeHTTP(writer, request.WithContext(ctx))
	}))

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.Addr,
			Handler:           mux,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		shutdownTimeout: cfg.ShutdownTimeout,
	}
}

type Server struct {
	httpServer      *http.Server
	shutdownTimeout time.Duration
}

func (server *Server) Serve(ctx context.Context) error {
	logger.Get("server").Info("HTTP server starting", logger.String("address", server.httpServer.Addr))

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), server.shutdownTimeout)
	defer cancel()

	err := server.httpServer.Shutdown(shutdownCtx)
	if err != nil {
		closeErr := server.httpServer.Close()

		<-serverErrors

		return errors.Join(fmt.Errorf("shutdown HTTP: %w", err), closeErr)
	}

	if err = <-serverErrors; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}

	return nil
}
