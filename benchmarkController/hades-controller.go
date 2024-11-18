package benchmarkController

import (
	"log/slog"
	"strconv"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
)

const (
	hades_host = "http://localhost:8081/build"
)

func BenchmarkHades(c *gin.Context) {
	slog.Debug("Received request to start Hades test")
	// Get number of jobs to run
	countStr := c.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		slog.Error("Failed to parse count", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse count"})
		return
	}

	// Run the Hades test
	slog.Debug("Running Hades jobs", slog.Any("count", count))
	runHadesBenchmark(count)

	c.JSON(200, gin.H{"message": "Hades test started"})

}

// Creates an Hades executor and runs the specified number of jobs
func runHadesBenchmark(number int) {
	hadesBenchmark := Benchmark{
		Executor:   executor.NewHadesExecutor(hades_host),
		Persister:  persister.NewDBPersister(),
		JobCounter: number,
	}
	hadesBenchmark.run()
}
