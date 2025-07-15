package main

import (
	"log/slog"
	"os"

	"github.com/Mtze/CI-Benchmarker/persister"
)

// Persister handles to store the job results in the database
var p persister.Persister

func main() {
	// Set the log level to debug if the DEBUG environment variable is set to true
	if is_debug := os.Getenv("DEBUG"); is_debug == "true" {
		slog.Warn("DEBUG MODE ENABLED")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	slog.Debug("Creating DB persister")
	p = persister.NewDBPersister()

	r := startRouter()

	err := r.Run(":8093")
	if err != nil {
		slog.Error("Failed to start server", slog.Any("error", err))
		return
	}
}
