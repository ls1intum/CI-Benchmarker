// Package exportController serves the raw per-job records.
//
// This is the only endpoint that should ever feed a figure or a statistic in a
// paper. The aggregate metrics endpoints compute integer means over values that
// have already been truncated to whole seconds, offer no p95, p99 or confidence
// interval, and silently drop incomplete jobs. Statistics belong in the
// analysis scripts, applied to these rows.
package exportController

import (
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	_ "github.com/Hades-Scheduler/CI-Benchmarker/shared/response"
	"github.com/gin-gonic/gin"
)

// jobCSVHeader documents the exported columns. Every *_ns column is epoch
// nanoseconds or an exact nanosecond difference.
var jobCSVHeader = []string{
	"run_id", "seq", "submission_id", "job_id", "variant", "target_host",
	"workload_id", "config_fingerprint", "priority", "commit_hash",
	"submit_time_ns", "submit_ack_time_ns", "submit_status", "submit_error",
	"callback_received_time_ns", "callback_status", "callback_raw_status",
	"callback_reason", "callback_source", "callback_event",
	"callback_delivery_count", "callback_delivery_attempt",
	"reported_queued_time_ns", "reported_start_time_ns", "reported_end_time_ns",
	"reported_duration_ms",
	"submit_rtt_ns", "end_to_end_ns", "completed",
}

// NewJobExportHandler streams per-job rows as JSONL or CSV.
//
// @Summary      Export raw per-job records
// @Description  Streams one row per submission attempt, joined to its terminal callback, as JSON Lines (default) or CSV. Jobs that never called back are included with completed=false; excluding them is what produced survivorship bias in the previous design. All analysis and every figure should be generated from this endpoint.
// @Tags         export
// @Produce      plain
// @Param        run_id  query  string  false  "Restrict to one run; omit to export every run"
// @Param        format  query  string  false  "jsonl or csv"  Enums(jsonl, csv)  default(jsonl)
// @Success      200  {string}  string  "JSONL or CSV stream"
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /export/jobs [get]
func NewJobExportHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Query("run_id")
		format := c.DefaultQuery("format", "jsonl")

		switch format {
		case "csv":
			exportJobsCSV(c, store, runID)
		case "jsonl", "":
			exportJobsJSONL(c, store, runID)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "format must be 'jsonl' or 'csv'"})
		}
	}
}

func exportJobsJSONL(c *gin.Context, store *persister.DBPersister, runID string) {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Status(http.StatusOK)

	encoder := json.NewEncoder(c.Writer)
	err := store.ExportJobs(c.Request.Context(), runID, func(row persister.JobRow) error {
		return encoder.Encode(row)
	})
	if err != nil {
		// The status line is already out, so the only honest signal left is to
		// append an error record and log it. Analysis scripts must treat a
		// trailing "_export_error" object as a truncated export.
		slog.Error("Job export failed", slog.String("run_id", runID), slog.Any("error", err))
		_ = encoder.Encode(map[string]string{"_export_error": err.Error()})
	}
}

func exportJobsCSV(c *gin.Context, store *persister.DBPersister, runID string) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="jobs.csv"`)
	c.Status(http.StatusOK)

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	if err := writer.Write(jobCSVHeader); err != nil {
		slog.Error("Failed to write CSV header", slog.Any("error", err))
		return
	}

	err := store.ExportJobs(c.Request.Context(), runID, func(row persister.JobRow) error {
		return writer.Write([]string{
			row.RunID,
			strconv.Itoa(row.Seq),
			row.SubmissionID,
			derefString(row.JobID),
			row.Variant,
			row.TargetHost,
			row.WorkloadID,
			row.ConfigFingerprint,
			strconv.Itoa(row.Priority),
			derefString(row.CommitHash),
			strconv.FormatInt(row.SubmitTimeNs, 10),
			formatInt64(row.SubmitAckTimeNs),
			row.SubmitStatus,
			derefString(row.SubmitError),
			formatInt64(row.CallbackReceivedTimeNs),
			derefString(row.CallbackStatus),
			derefString(row.CallbackRawStatus),
			derefString(row.CallbackReason),
			derefString(row.CallbackSource),
			derefString(row.CallbackEvent),
			formatInt64(row.CallbackDeliveryCount),
			formatInt64(row.CallbackDeliveryAttempt),
			formatInt64(row.ReportedQueuedTimeNs),
			formatInt64(row.ReportedStartTimeNs),
			formatInt64(row.ReportedEndTimeNs),
			formatInt64(row.ReportedDurationMs),
			formatInt64(row.SubmitRttNs),
			formatInt64(row.EndToEndNs),
			strconv.FormatBool(row.Completed),
		})
	})
	if err != nil {
		slog.Error("Job export failed", slog.String("run_id", runID), slog.Any("error", err))
	}
}

// NewCallbackExportHandler streams stored callbacks.
//
// @Summary      Export raw callbacks
// @Description  Streams every recorded terminal callback as JSON Lines, including ones that never matched a submission. Set only_unmatched=true to list just the orphans, which is worth checking after every run.
// @Tags         export
// @Produce      plain
// @Param        run_id          query  string  false  "Restrict to one run"
// @Param        only_unmatched  query  bool    false  "Only callbacks with no matching submission"  default(false)
// @Success      200  {string}  string  "JSONL stream"
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /export/callbacks [get]
func NewCallbackExportHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Query("run_id")
		onlyUnmatched := c.DefaultQuery("only_unmatched", "false") == "true"

		c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
		c.Status(http.StatusOK)

		encoder := json.NewEncoder(c.Writer)
		err := store.ExportCallbacks(c.Request.Context(), runID, onlyUnmatched, func(row persister.CallbackRow) error {
			return encoder.Encode(row)
		})
		if err != nil {
			slog.Error("Callback export failed", slog.Any("error", err))
			_ = encoder.Encode(map[string]string{"_export_error": err.Error()})
		}
	}
}

// NewRunListHandler returns the recorded runs.
//
// @Summary      List benchmark runs
// @Description  Returns every recorded run with its provenance, newest first.
// @Tags         export
// @Produce      json
// @Success      200  {array}   persister.Run
// @Failure      500  {object}  response.ServerErrorMessage
// @Router       /export/runs [get]
func NewRunListHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		runs, err := store.ListRuns(c.Request.Context())
		if err != nil {
			slog.Error("Failed to list runs", slog.Any("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list runs"})
			return
		}
		if runs == nil {
			runs = []persister.Run{}
		}
		c.JSON(http.StatusOK, runs)
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}
