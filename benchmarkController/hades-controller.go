package benchmarkController

import (
	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/Mtze/CI-Benchmarker/shared/config"
	"log/slog"
)

func NewHadesBenchmark() Benchmark {
	slog.Debug("Creating new Hades benchmark")
	cfg := config.Load()
	return Benchmark{
		Executor:  executor.NewHadesExecutor(cfg.HadesHost),
		Persister: persister.NewDBPersister(),
	}
}
