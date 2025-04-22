package benchmarkController

import (
	"log/slog"
	"os"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
)

var hades_host = os.Getenv("HADES_HOST")

func init() {
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
