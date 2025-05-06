package benchmarkController

import (
	"github.com/ls1intum/hades/shared/payload"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Mtze/CI-Benchmarker/executor"
	"github.com/Mtze/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// Benchmark represents a benchmarking process that includes an executor to run the benchmarks,
// a persister to save the results, and a job counter to keep track of the number of jobs executed.
type Benchmark struct {
	Executor   executor.Executor
	Persister  persister.Persister
	JobCounter int
}

// HandleFunc is a handler function that receives a request to start a benchmark. It parses the
// number of jobs to run from the request, runs the benchmark, and returns a response to the client.
// The function assumes that the Executor and Persister fields of the Benchmark struct are already
// initialized.
func (b Benchmark) HandleFunc(c *gin.Context) {
	var restPayload payload.RESTPayload
	if err := c.ShouldBind(&restPayload); err != nil {
		log.WithError(err).Error("Failed to bind JSON")
		c.String(http.StatusBadRequest, "Failed to bind JSON")
		return
	}

	slog.Info("Received request to start benchmark")

	// Get the number of jobs to run
	countStr := c.DefaultQuery("count", "1")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		slog.Error("Failed to parse count", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse count"})
		return
	}

	// Run the benchmark
	slog.Debug("Running jobs", slog.Any("count", count))
	b.JobCounter = count
	b.run(restPayload)

	c.JSON(200, gin.H{"message": "Benchmark started"})
}

// run executes the benchmark jobs concurrently. It logs the start of job execution,
// schedules each job, executes it using the provided executor, and stores the job
// result using the persister. It waits for all jobs to complete before returning.
//
// The function logs various stages of job execution, including the start of job
// scheduling, any errors encountered during execution, and the successful storage
// of job results.
func (b Benchmark) run(payload payload.RESTPayload) {
	slog.Info("Running jobs", slog.Any("number", b.JobCounter), slog.Any("executor", b.Executor))
	var wg sync.WaitGroup

	for i := 0; i < b.JobCounter; i++ {
		wg.Add(1)

		go func(p persister.Persister) {
			defer wg.Done()
			slog.Debug("Scheduling job %d", slog.Any("i", i))
			// Execute the job
			uuid, err := b.Executor.Execute(payload)
			if err != nil {
				slog.Error("Error while scheduling", slog.Any("error", err))
			}

			// Store the job
			slog.Debug("Storing job", slog.Any("uuid", uuid))
			p.StoreJob(uuid, time.Now(), b.Executor.Name())

			slog.Debug("Job send successfully", slog.Any("uuid", uuid))
		}(b.Persister)
	}

	wg.Wait()
}
