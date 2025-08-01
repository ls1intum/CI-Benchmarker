package main

import (
	"log/slog"
	"strings"

	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/Mtze/CI-Benchmarker/shared/config"
)

// Persister handles to store the job results in the database
var p persister.Persister

func main() {
	// Set the log level to debug if the DEBUG environment variable is set to true
	if is_debug := config.GetEnv("DEBUG"); is_debug == "true" {
		slog.Warn("DEBUG MODE ENABLED")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	cfg := config.Load()

	slog.Debug("Creating DB persister")
	p = persister.NewDBPersister()

	addr := cfg.ServerAddress
	if addr == "" {
		addr = ":8080"
	} else if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	slog.Info("Starting server", slog.String("address", addr))

	r := startRouter()

	if err := r.Run(addr); err != nil {
		slog.Error("Failed to start server", slog.Any("error", err))
	}
}
