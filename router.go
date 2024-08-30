package main

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var persister Persister

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
	// Create a db instance - ugly hack - should be refactored
	slog.Debug("Creating DB persister")
	persister = NewDBPersister()

	slog.Debug("Setting up router")
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	version := r.Group("/v1")

	// Define the route for the result
	slog.Debug("Setting up routes")
	version.POST("/result", handleResult)
	version.POST("/benchmark/hades", benchmarkHades)

	return r
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
	HadesTest(count)

	c.JSON(200, gin.H{"message": "Hades test started"})

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

	time := time.Now()

	persister.StoreResult(uuid, time)

	c.JSON(200, gin.H{"message": "Result received"})
}
