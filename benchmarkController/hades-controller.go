package benchmarkController

import (
	"log/slog"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
)

const (
	//TODO: read this from config
	hades_host = "http://localhost:8081/build"
)

func NewHadesBenchmark() Benchmark {
	slog.Debug("Creating new Hades benchmark")
	return Benchmark{
		Executor:  executor.NewHadesExecutor(hades_host),
		Persister: persister.NewDBPersister(),
	}
}
