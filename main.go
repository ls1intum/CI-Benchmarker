package main

import (
	"log/slog"

	"github.com/Mtze/CI-Benchmarker/executor"
)

func main() {

	// Set debug level
	slog.SetLogLoggerLevel(slog.LevelDebug)

	he := executor.NewHadesExecutor("http://localhost:8081/build")

	uuid, err := he.Execute()
	if err != nil {
		slog.Error("Error while executing HadesExecutor", slog.Any("error", err))
	}

	slog.Info("HadesExecutor executed successfully", slog.Any("uuid", uuid))
}
