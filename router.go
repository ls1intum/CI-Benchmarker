package main

import (
	"log/slog"
	"time"

	"github.com/Mtze/CI-Benchmarker/MetricsController"
	"github.com/Mtze/CI-Benchmarker/benchmarkController"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ResultMetadata struct {
	JobName                  string `json:"jobName" env:"JOB_NAME"`
	UUID                     string `json:"uuid" env:"UUID"`
	AssignmentRepoBranchName string `json:"assignmentRepoBranchName" env:"ASSIGNMENT_REPO_BRANCH_NAME" envDefault:"main"`
	IsBuildSuccessful        bool   `json:"isBuildSuccessful" env:"IS_BUILD_SUCCESSFUL"`
	AssignmentRepoCommitHash string `json:"assignmentRepoCommitHash" env:"ASSIGNMENT_REPO_COMMIT_HASH"`
	TestsRepoCommitHash      string `json:"testsRepoCommitHash" env:"TESTS_REPO_COMMIT_HASH"`
	BuildCompletionTime      string `json:"buildCompletionTime" env:"BUILD_COMPLETION_TIME"`
}

func startRouter() *gin.Engine {
	slog.Debug("Setting up router")
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	version := r.Group("/v1")

	// Register the route for the start of the job
	slog.Debug("Setting up routes")
	version.POST("/result", handleResult)

	// Register the route for the benchmark executors
	version.POST("/benchmark/hades", benchmarkController.NewHadesBenchmark().HandleFunc)

	version.GET("/histogram/queue_latency", MetricsController.GetQueueLatencyMetrics)

	version.GET("/histogram/build_time", MetricsController.GetBuildTimeHistogram)

	return r
}

// handleResult is a function to handle the result of a job
// The last container in the pipeline will send the result to this endpoint
func handleResult(c *gin.Context) {
	slog.Debug("Received result", slog.Any("result", c.Request.Body))

	// Get the UUID and the time from the request
	var resultMetadata ResultMetadata
	if err := c.ShouldBindJSON(&resultMetadata); err != nil {
		slog.Error("Failed to bind JSON", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to bind JSON"})
		return
	}

	uuid, err := uuid.Parse(resultMetadata.UUID)
	if err != nil {
		slog.Error("Failed to parse UUID", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse UUID"})
		return
	}

	currentTime := time.Now()

	buildCompletionTime, err := time.Parse(time.RFC3339, resultMetadata.BuildCompletionTime)
	if err != nil {
		slog.Error("Failed to parse BuildCompletionTime", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse BuildCompletionTime"})
		return
	}
	buildStartTime := buildCompletionTime.Add(-time.Since(time.Now()))

	p.StoreResult(uuid, buildStartTime, currentTime)

	c.JSON(200, gin.H{"message": "Result received"})
}
