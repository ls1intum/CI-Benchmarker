package main

import (
	_ "github.com/Hades-Scheduler/CI-Benchmarker/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/MetricsController"
	"github.com/Hades-Scheduler/CI-Benchmarker/benchmarkController"
	"github.com/Hades-Scheduler/CI-Benchmarker/callbackController"
	"github.com/Hades-Scheduler/CI-Benchmarker/exportController"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"
	_ "github.com/Hades-Scheduler/CI-Benchmarker/shared/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ResultMetadata is the body of the deprecated /v1/result endpoint.
//
// Deprecated: completion is now observed at /v1/callback, which the scheduler
// posts to. This endpoint required a reporter container inside the workload to
// call home, which charged that container's image pull and process start to the
// measurement and lost the job entirely whenever the reporter failed.
type ResultMetadata struct {
	JobName                  string `json:"jobName" env:"JOB_NAME"`
	UUID                     string `json:"uuid" env:"UUID"`
	AssignmentRepoBranchName string `json:"assignmentRepoBranchName" env:"ASSIGNMENT_REPO_BRANCH_NAME" envDefault:"main"`
	IsBuildSuccessful        bool   `json:"isBuildSuccessful" env:"IS_BUILD_SUCCESSFUL"`
	AssignmentRepoCommitHash string `json:"assignmentRepoCommitHash" env:"ASSIGNMENT_REPO_COMMIT_HASH"`
	TestsRepoCommitHash      string `json:"testsRepoCommitHash" env:"TESTS_REPO_COMMIT_HASH"`
	BuildCompletionTime      string `json:"buildCompletionTime" env:"BUILD_COMPLETION_TIME"`
}

// JobStartTime is the body of the deprecated /v1/start_time endpoint.
//
// Deprecated: see ResultMetadata.
type JobStartTime struct {
	UUID           string `json:"uuid" env:"UUID"`
	BuildStartTime string `json:"buildStartTime" env:"BUILD_START_TIME"`
}

const legacyWriteTimeout = 15 * time.Second

func startRouter(store *persister.DBPersister, cfg config.Config) *gin.Engine {
	slog.Debug("Setting up router")
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	version := r.Group("/v1")

	version.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	version.GET("/healthz", healthHandler(store))

	// The measurement path: the system under test posts a terminal status here
	// and the benchmarker stamps arrival on its own clock.
	version.POST("/callback", callbackController.NewStatusCallbackHandler(store))

	// Deprecated compatibility path. Writes only to the legacy job_results
	// table and never feeds an export.
	version.POST("/result", deprecatedResultHandler(store))
	version.POST("/start_time", deprecatedStartTimeHandler(store))

	benchmarkGroup := version.Group("/benchmark")
	{
		benchmarkGroup.POST("/hades-docker", benchmarkController.NewHadesDockerBenchmark(store, cfg))
		benchmarkGroup.POST("/hades-k8s", benchmarkController.NewHadesKubernetesBenchmark(store, cfg))
		benchmarkGroup.POST("/jenkins", benchmarkController.NewJenkinsBenchmark(store, cfg))

		// Deprecated aggregate endpoints, kept so old dashboards keep working.
		// They read the legacy tables and truncate to whole seconds.
		benchmarkGroup.GET("/latency/histogram", MetricsController.GetTotalLatencyHistogram)
		benchmarkGroup.GET("/latency/metrics", MetricsController.GetTotalLatencyMetrics)
		benchmarkGroup.GET("/queue_latency/histogram", MetricsController.GetQueueLatencyHistogram)
		benchmarkGroup.GET("/queue_latency/metrics", MetricsController.GetQueueLatencyMetrics)
		benchmarkGroup.GET("/build_time/histogram", MetricsController.GetBuildTimeHistogram)
		benchmarkGroup.GET("/build_time/metrics", MetricsController.GetBuildTimeMetrics)
	}

	// Raw export. Everything in the paper is generated from here.
	exportGroup := version.Group("/export")
	{
		exportGroup.GET("/jobs", exportController.NewJobExportHandler(store))
		exportGroup.GET("/callbacks", exportController.NewCallbackExportHandler(store))
		exportGroup.GET("/runs", exportController.NewRunListHandler(store))
	}

	return r
}

// healthHandler reports readiness including the applied schema version.
//
// @Summary      Health check
// @Description  Reports service health and the applied schema version.
// @Tags         ops
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /healthz [get]
func healthHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		schemaVersion, err := persister.SchemaVersion(c.Request.Context(), store.DB())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":         "ok",
			"schema_version": schemaVersion,
			"version":        config.Version,
		})
	}
}

// deprecatedResultHandler backs POST /v1/result.
//
// @Summary      [Deprecated] Receive job result from inside the workload
// @Description  Deprecated. Use POST /v1/callback instead. This endpoint requires a reporter container inside the job to call home, which charges that container's image pull and process start to the measurement and loses the job entirely if the reporter fails. It writes only to the legacy job_results table and never feeds /v1/export.
// @Tags         deprecated
// @Accept       json
// @Produce      json
// @Param        resultMetadata  body  ResultMetadata  true  "Job Result Metadata"
// @Success      200  {object}  response.SimpleMessage
// @Failure      400  {object}  response.ErrorMessage
// @Failure      503  {object}  response.ServerErrorMessage
// @Deprecated
// @Router       /result [post]
func deprecatedResultHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		var resultMetadata ResultMetadata
		if err := c.ShouldBindJSON(&resultMetadata); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to bind JSON"})
			return
		}

		id, err := uuid.Parse(resultMetadata.UUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse UUID"})
			return
		}

		completionTime, err := parseReportedTime(resultMetadata.BuildCompletionTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse BuildCompletionTime"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), legacyWriteTimeout)
		defer cancel()

		if err := store.StoreResult(ctx, id, completionTime); err != nil {
			slog.Error("Failed to store deprecated result", slog.Any("error", err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to store result"})
			return
		}

		c.Header("Deprecation", "true")
		c.Header("Link", `</v1/callback>; rel="successor-version"`)
		c.JSON(http.StatusOK, gin.H{"message": "Result received"})
	}
}

// deprecatedStartTimeHandler backs POST /v1/start_time.
//
// @Summary      [Deprecated] Receive build start time from inside the workload
// @Description  Deprecated. Use POST /v1/callback instead. Writes only to the legacy job_results table and never feeds /v1/export.
// @Tags         deprecated
// @Accept       json
// @Produce      json
// @Param        jobStartTime  body  JobStartTime  true  "Build Start Time"
// @Success      200  {object}  response.SimpleMessage
// @Failure      400  {object}  response.ErrorMessage
// @Failure      503  {object}  response.ServerErrorMessage
// @Deprecated
// @Router       /start_time [post]
func deprecatedStartTimeHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		var jobStartTime JobStartTime
		if err := c.ShouldBindJSON(&jobStartTime); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to bind JSON"})
			return
		}

		id, err := uuid.Parse(jobStartTime.UUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse UUID"})
			return
		}

		startTime, err := parseReportedTime(jobStartTime.BuildStartTime)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse BuildStartTime"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), legacyWriteTimeout)
		defer cancel()

		if err := store.StoreStartTime(ctx, id, startTime); err != nil {
			slog.Error("Failed to store deprecated start time", slog.Any("error", err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to store start time"})
			return
		}

		c.Header("Deprecation", "true")
		c.Header("Link", `</v1/callback>; rel="successor-version"`)
		c.JSON(http.StatusOK, gin.H{"message": "Build start time received"})
	}
}

// parseReportedTime accepts RFC3339 with or without fractional seconds.
func parseReportedTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, &time.ParseError{Layout: time.RFC3339, Value: value}
}
