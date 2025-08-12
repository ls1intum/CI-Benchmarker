package main

import (
	_ "github.com/Mtze/CI-Benchmarker/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"log/slog"
	"time"

	"github.com/Mtze/CI-Benchmarker/MetricsController"
	"github.com/Mtze/CI-Benchmarker/benchmarkController"
	_ "github.com/Mtze/CI-Benchmarker/shared/response"
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

type JobStartTime struct {
	UUID           string `json:"uuid" env:"UUID"`
	BuildStartTime string `json:"buildStartTime" env:"BUILD_START_TIME"`
}

func startRouter() *gin.Engine {
	slog.Debug("Setting up router")
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	version := r.Group("/v1")

	// Register the route for the start of the job
	slog.Debug("Setting up routes")

	version.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	version.POST("/result", handleResult)

	version.POST("/start_time", handleStartTime)

	// Register the route for the benchmark executors
	version.POST("/benchmark/hades", benchmarkController.NewHadesBenchmark())

	version.GET("benchmark/latency/histogram", MetricsController.GetTotalLatencyHistogram)

	version.GET("benchmark/latency/metrics", MetricsController.GetTotalLatencyMetrics)

	version.GET("benchmark/queue_latency/histogram", MetricsController.GetQueueLatencyHistogram)

	version.GET("benchmark/queue_latency/metrics", MetricsController.GetQueueLatencyMetrics)

	version.GET("benchmark/build_time/histogram", MetricsController.GetBuildTimeHistogram)

	version.GET("benchmark/build_time/metrics", MetricsController.GetBuildTimeMetrics)

	return r
}

// @Summary      Receive job result
// @Description  This endpoint handles the result of a job, The last container in the pipeline should send the result to this endpoint
// @Tags         result
// @Accept       json
// @Produce      json
// @Param        resultMetadata  body  ResultMetadata  true  "Job Result Metadata"
// @Success      200  {object}  response.SimpleMessage
// @Failure 	 400  {object} 	response.ErrorMessage
// @Router       /result [post]
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

	buildCompletionTime, err := time.Parse(time.RFC3339, resultMetadata.BuildCompletionTime)
	if err != nil {
		slog.Error("Failed to parse BuildCompletionTime", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse BuildCompletionTime"})
		return
	}

	p.StoreResult(uuid, buildCompletionTime)

	c.JSON(200, gin.H{"message": "Result received"})
}

// @Summary      Receive build start time
// @Description  Submit job start time for benchmarking
// @Tags         start_time
// @Accept       json
// @Produce      json
// @Param        jobStartTime  body  JobStartTime  true  "Build Start Time"
// @Success      200  {object}  response.SimpleMessage
// @Failure      400  {object}  response.ErrorMessage
// @Router       /start_time [post]
func handleStartTime(c *gin.Context) {
	slog.Debug("Received job start time information", slog.Any("time", c.Request.Body))

	// Get the UUID and the time from the request
	var jobStartTime JobStartTime
	if err := c.ShouldBindJSON(&jobStartTime); err != nil {
		slog.Error("Failed to bind JSON", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to bind JSON"})
		return
	}

	uuid, err := uuid.Parse(jobStartTime.UUID)
	if err != nil {
		slog.Error("Failed to parse UUID", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse UUID"})
		return
	}

	buildStartTime, err := time.Parse(time.RFC3339, jobStartTime.BuildStartTime)
	if err != nil {
		slog.Error("Failed to parse BuildStartTime", slog.Any("error", err))
		c.JSON(400, gin.H{"error": "Failed to parse BuildStartTime"})
		return
	}

	p.StoreStartTime(uuid, buildStartTime)

	c.JSON(200, gin.H{"message": "Build start time received"})
}
