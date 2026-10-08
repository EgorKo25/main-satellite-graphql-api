package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EgorKo25/main-satellite-graphql-api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGraphQLRequestContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		cancelParent bool
		wantStatus   int
		wantErr      error
	}{
		{
			name:       "request deadline",
			path:       "/graphql",
			wantStatus: http.StatusNoContent,
			wantErr:    context.DeadlineExceeded,
		},
		{
			name:         "parent cancellation",
			path:         "/graphql",
			cancelParent: true,
			wantStatus:   http.StatusNoContent,
			wantErr:      context.Canceled,
		},
		{name: "unknown path", path: "/unknown", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			parent, cancelParent := context.WithCancel(ctx)
			defer cancelParent()

			if test.cancelParent {
				cancelParent()
			}

			var (
				requestErr error
				deadline   time.Time
			)

			server := New(config.HTTP{RequestTimeout: 20 * time.Millisecond},
				http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					deadline, _ = request.Context().Deadline()

					select {
					case <-request.Context().Done():
						requestErr = request.Context().Err()
					case <-ctx.Done():
					}

					writer.WriteHeader(http.StatusNoContent)
				}))
			request := httptest.NewRequestWithContext(parent, http.MethodPost, test.path, nil)
			recorder := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(recorder, request)

			require.Equal(t, test.wantStatus, recorder.Code)
			require.ErrorIs(t, requestErr, test.wantErr)

			if test.wantErr != nil {
				parentDeadline, ok := parent.Deadline()
				require.True(t, ok)
				require.False(t, deadline.IsZero())
				require.True(t, deadline.Before(parentDeadline))
			}
		})
	}
}

func TestServeWaitsForActiveRequest(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	serveCtx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		serveErr       error
		requestStopped <-chan struct{}
	)

	address := make(chan string, 1)
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	shutdownStarted := make(chan struct{})
	serverStopped := make(chan struct{})
	server := New(config.HTTP{
		Addr:            "127.0.0.1:0",
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started <- request.Context()

		select {
		case <-release:
			writer.WriteHeader(http.StatusNoContent)
		case <-request.Context().Done():
			writer.WriteHeader(http.StatusRequestTimeout)
		}
	}))
	server.httpServer.BaseContext = func(listener net.Listener) context.Context {
		address <- listener.Addr().String()

		return ctx
	}
	server.httpServer.RegisterOnShutdown(func() { close(shutdownStarted) })

	go func() {
		defer close(serverStopped)

		serveErr = server.Serve(serveCtx)
	}()

	t.Cleanup(func() {
		stop()
		cancel()
		require.NoError(t, server.httpServer.Close())

		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()

		for _, stopped := range []<-chan struct{}{serverStopped, requestStopped} {
			if stopped == nil {
				continue
			}

			select {
			case <-stopped:
			case <-cleanupCtx.Done():
				require.NoError(t, cleanupCtx.Err(), "wait for HTTP workers")
			}
		}
	})

	var addr string

	select {
	case addr = <-address:
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "wait for listener")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/graphql", nil)
	require.NoError(t, err)

	var (
		requestErr error
		status     int
	)

	completed := make(chan struct{})
	requestStopped = completed

	go func() {
		defer close(completed)

		response, responseErr := http.DefaultClient.Do(request)
		requestErr = responseErr

		if responseErr == nil {
			status = response.StatusCode
			requestErr = response.Body.Close()
		}
	}()

	var requestCtx context.Context

	select {
	case requestCtx = <-started:
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "wait for active request")
	}

	stop()

	select {
	case <-shutdownStarted:
	case <-ctx.Done():
		require.NoError(t, ctx.Err(), "wait for shutdown")
	}

	require.NoError(t, requestCtx.Err())

	select {
	case <-serverStopped:
		require.FailNow(t, "Serve returned while the request was active")
	default:
	}

	close(release)

	for _, stopped := range []<-chan struct{}{completed, serverStopped} {
		select {
		case <-stopped:
		case <-ctx.Done():
			require.NoError(t, ctx.Err(), "wait for graceful shutdown")
		}
	}

	require.NoError(t, requestErr)
	require.Equal(t, http.StatusNoContent, status)
	require.NoError(t, serveErr)
}

func TestServeReturnsListenError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	server := New(config.HTTP{Addr: listener.Addr().String(), ShutdownTimeout: time.Second}, http.NotFoundHandler())
	err = server.Serve(ctx)

	var operationErr *net.OpError

	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, "listen", operationErr.Op)
}
