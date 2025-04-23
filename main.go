package main

import (
	"github.com/joho/godotenv"
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

	if err := godotenv.Load(); err != nil {
		slog.Warn("No .env file found, using default configuration")
	}
	address := os.Getenv("SERVER_ADDRESS")
	if address == "" {
		address = ":8080" // default port
	}
	err := r.Run(":" + address)
	if err != nil {
		slog.Error("Failed to start server", slog.Any("error", err))
		return
	}
}
