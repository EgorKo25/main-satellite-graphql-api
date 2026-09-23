//go:build integration

package integrationtests

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/ory/dockertest/v4"
)

func New(ctx context.Context) (server *Server, err error) {
	pool, err := dockertest.NewPool(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("connect to Docker: %w", err)
	}

	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()

			err = errors.Join(err, pool.Close(cleanupCtx))
		}
	}()

	resource, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("17.9-bookworm"),
		dockertest.WithEnv([]string{
			"POSTGRES_USER=graphql_test",
			"POSTGRES_PASSWORD=graphql_test",
			"POSTGRES_DB=postgres",
		}),
		dockertest.WithoutReuse(),
		dockertest.WithHostConfig(func(cfg *container.HostConfig) {
			cfg.Tmpfs = map[string]string{"/var/lib/postgresql/data": "rw"}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("start test PostgreSQL: %w", err)
	}

	hostPort := resource.GetHostPort("5432/tcp")
	if host := os.Getenv("DOCKERTEST_HOST"); host != "" {
		hostPort = net.JoinHostPort(host, resource.GetPort("5432/tcp"))
	}

	databaseURL := "postgres://graphql_test:graphql_test@" + hostPort + "/postgres?sslmode=disable"

	if err = pool.Retry(ctx, time.Minute, func() error {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		connection, connectErr := pgx.Connect(attemptCtx, databaseURL)
		if connectErr != nil {
			return fmt.Errorf("connect to test PostgreSQL: %w", connectErr)
		}

		if connectErr = connection.Close(attemptCtx); connectErr != nil {
			return fmt.Errorf("close readiness connection: %w", connectErr)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("wait for test PostgreSQL: %w", err)
	}

	return &Server{URL: databaseURL, pool: pool}, nil
}

type Server struct {
	URL  string
	pool dockertest.ClosablePool
}

func (server *Server) Close(ctx context.Context) error {
	if err := server.pool.Close(ctx); err != nil {
		return fmt.Errorf("remove test PostgreSQL: %w", err)
	}

	return nil
}
