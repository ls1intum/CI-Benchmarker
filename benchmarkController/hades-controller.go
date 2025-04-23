package benchmarkController

import (
	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/joho/godotenv"
	"log/slog"
	"os"
)

var hades_host string

func init() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		slog.Warn("No .env file found or failed to load it")
	}

	hades_host = os.Getenv("HADES_HOST")
	if hades_host == "" {
		slog.Error("Environment variable HADES_HOST is not set")
		panic("HADES_HOST is required but not set")
	}
}

func NewHadesBenchmark() Benchmark {
	slog.Debug("Creating new Hades benchmark")
	return Benchmark{
		Executor:  executor.NewHadesExecutor(hades_host),
		Persister: persister.NewDBPersister(),
	}
}
