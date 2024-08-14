package main

import (
	"log/slog"
	"sync"

	"github.com/Mtze/CI-Benchmarker/executor"
)

func main() {

	// Set debug level
	slog.SetLogLoggerLevel(slog.LevelDebug)

	he := executor.NewHadesExecutor("http://localhost:8081/build")

	runBuilds(3, he)

	slog.Info("Finished")
}

func runBuilds(number int, e executor.Executor) {
	var wg sync.WaitGroup

	for i := 0; i < number; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			uuid, err := e.Execute()
			if err != nil {
				slog.Error("Error while scheduling", slog.Any("error", err))
			}
			slog.Debug("Job send successfully", slog.Any("uuid", uuid))
		}()
	}

	wg.Wait()
}
