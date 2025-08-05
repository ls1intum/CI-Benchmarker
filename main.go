package main

import (
	"log/slog"
	"strings"

	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/Mtze/CI-Benchmarker/shared/config"

	docs "github.com/Mtze/CI-Benchmarker/docs"
)

// @title           CI-Benchmarker API
// @version         1.0
// @description     Benchmark system collecting CI latency, build time and metrics.
// @termsOfService  https://github.com/Mtze/CI-Benchmarker

// @contact.name    Shuaiwei Yu
// @contact.url     https://github.com/Mtze
// @contact.email   yu.shuaiwei@tum.de

// @license.name    MIT
// @license.url     https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /v1

// @schemes http https

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

	port := strings.TrimPrefix(addr, ":")
	docs.SwaggerInfo.Host = "localhost:" + port
	docs.SwaggerInfo.Schemes = []string{"http"}

	r := startRouter()

	if err := r.Run(addr); err != nil {
		slog.Error("Failed to start server", slog.Any("error", err))
	}
}
