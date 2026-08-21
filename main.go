package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/executor"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"

	docs "github.com/Hades-Scheduler/CI-Benchmarker/docs"
)

// @title           CI-Benchmarker API
// @version         2.0
// @description     Measurement instrument for comparing CI job-execution variants. Jobs are submitted over REST and completions are observed over REST: the system under test posts a terminal status callback to /v1/callback and the benchmarker stamps arrival on its own clock. Analysis is done on the raw rows from /v1/export/jobs, never on the aggregate endpoints.
// @termsOfService  https://github.com/Hades-Scheduler/CI-Benchmarker

// @contact.name    Shuaiwei Yu
// @contact.url     https://github.com/Mtze
// @contact.email   yu.shuaiwei@tum.de

// @license.name    MIT
// @license.url     https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /v1

// @schemes http https

func main() {
	if config.GetEnv("DEBUG") == "true" {
		slog.Warn("DEBUG MODE ENABLED")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	cfg := config.Load()

	slog.Info("CI-Benchmarker starting", slog.String("version", config.Version))

	// One shared HTTP client configuration for every executor, so all variants
	// are driven with identical connection and timeout behaviour.
	executor.ConfigureSharedClient(executor.ClientConfig{
		Timeout:             cfg.HTTPTimeout(),
		DialTimeout:         cfg.HTTPDialTimeout(),
		MaxIdleConns:        cfg.HTTPMaxIdleConnsPerHost,
		MaxIdleConnsPerHost: cfg.HTTPMaxIdleConnsPerHost,
		MaxConnsPerHost:     cfg.HTTPMaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
	})

	slog.Info("Opening benchmark database", slog.String("path", cfg.DBPath))
	store := persister.MustOpen(cfg.DBPath)
	defer func() {
		if err := store.Close(); err != nil {
			slog.Error("Failed to close database", slog.Any("error", err))
		}
	}()

	addr := cfg.ServerAddress
	if addr == "" {
		addr = ":8080"
	} else if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	docs.SwaggerInfo.Host = "localhost:" + strings.TrimPrefix(addr, ":")
	docs.SwaggerInfo.Schemes = []string{"http"}

	server := &http.Server{
		Addr:    addr,
		Handler: startRouter(store, cfg),
		// No write timeout: an export of a long run legitimately streams for a
		// while. Read timeouts stay short because callbacks are tiny.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("Starting server", slog.String("address", addr), slog.String("version", config.Version))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server failed", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Shut down gracefully so queued writes are drained rather than lost. A
	// benchmark run that is interrupted must still yield the rows it collected.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down, draining pending writes")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Graceful shutdown failed", slog.Any("error", err))
	}
}
