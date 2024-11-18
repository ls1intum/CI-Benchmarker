package main

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/gin-gonic/gin"
)

const (
	hades_host = "http://localhost:8081/build"
)

// Creates an Hades executor and runs the specified number of jobs
func HadesBenchmarkExecutor(number int) {
	he := executor.NewHadesExecutor(hades_host)
	p := NewDBPersister()
	runJobs(number, he, p)
}

func benchmarkHades(c *gin.Context) {
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
	HadesBenchmarkExecutor(count)

	c.JSON(200, gin.H{"message": "Hades test started"})

}

func runJobs(number int, e executor.Executor, persister Persister) {
	slog.Debug("Running jobs", slog.Any("number", number), slog.Any("executor", e))
	var wg sync.WaitGroup

	for i := 0; i < number; i++ {
		wg.Add(1)

		go func(p Persister) {
			defer wg.Done()
			slog.Debug("Scheduling job %d", slog.Any("i", i))
			// Execute the job
			uuid, err := e.Execute()
			if err != nil {
				slog.Error("Error while scheduling", slog.Any("error", err))
			}

			// Store the job
			slog.Debug("Storing job", slog.Any("uuid", uuid))
			p.StoreJob(uuid, time.Now())

			slog.Debug("Job send successfully", slog.Any("uuid", uuid))
		}(persister)
	}

	wg.Wait()
}
