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

	"github.com/code-corhuila/multi-tour-worker-runtime/health"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"

	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/config"
)

// The health server's limits are declared here, not left to defaults (Annex D).
const (
	readHeaderTimeout = 5 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 20 * time.Second
)

// newMux is the whole surface of the worker: /health and nothing else (Annex D).
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /health", health.Handler())
	return mux
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reprogramming-timers:", err)
		os.Exit(1)
	}
}

// run starts the worker: the configuration is read once and a bad one stops it. The job is wired in the next piece.
func run() error {
	cfg, err := config.LoadReprogramming(os.Getenv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout)
	server := &http.Server{Addr: cfg.HealthAddr, Handler: newMux(), ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout: writeTimeout, IdleTimeout: idleTimeout}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	log.Info("worker started", "job", "reprogramming-timers", "interval", cfg.Interval.String())

	var failure error
	select {
	case <-ctx.Done():
	case failure = <-serverErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && failure == nil {
		failure = err
	}
	if errors.Is(failure, http.ErrServerClosed) {
		return nil
	}
	return failure
}
