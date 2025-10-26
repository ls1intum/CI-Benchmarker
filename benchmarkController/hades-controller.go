package benchmarkController

import (
	"log/slog"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
)

func NewHadesDockerBenchmark() gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Debug("Creating new Hades (Docker) benchmark")

		hadesHost := c.Query("host")
		benchmark := Benchmark{
			Executor:  executor.NewHadesDockerExecutor(hadesHost),
			Persister: persister.NewDBPersister(),
		}

		benchmark.HandleFunc(c)
	}
}

func NewHadesKubernetesBenchmark() gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Debug("Creating new Hades (Kubernetes) benchmark")

		hadesHost := c.Query("host")
		benchmark := Benchmark{
			Executor:  executor.NewHadesKubernetesExecutor(hadesHost),
			Persister: persister.NewDBPersister(),
		}

		benchmark.HandleFunc(c)
	}
}
