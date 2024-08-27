package main

import (
	"log/slog"
	"sync"
	"time"

	"github.com/Mtze/CI-Benchmarker/executor"
)

const (
	hades_host = "http://localhost:8081/build"
)

// Creates an Hades executor and runs the specified number of jobs
func HadesTest(number int) {
	he := executor.NewHadesExecutor(hades_host)
	p := NewDBPersister()
	runJobs(number, he, p)
}

func runJobs(number int, e executor.Executor, persister Persister) {
	slog.Debug("Running jobs", slog.Any("number", number), slog.Any("executor", e))
	var wg sync.WaitGroup

	for i := 0; i < number; i++ {
		wg.Add(1)

		go func(p Persister) {
			defer wg.Done()
			slog.Debug("Scheduling job %d", i)
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
