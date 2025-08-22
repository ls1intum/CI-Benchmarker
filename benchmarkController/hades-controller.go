package benchmarkController

import (
	"log/slog"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
)

func NewHadesBenchmark() gin.HandlerFunc {
	return func(c *gin.Context) {
		slog.Debug("Creating new Hades benchmark")

		hadesHost := c.Query("host")
		benchmark := Benchmark{
			Executor:  executor.NewHadesExecutor(hadesHost),
			Persister: persister.NewDBPersister(),
		}

		benchmark.HandleFunc(c)
	}
}
