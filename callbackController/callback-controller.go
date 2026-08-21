// Package callbackController exposes the status-callback receiver.
//
// This is the single point at which a job is observed to have finished. The
// system under test POSTs here when it reaches a terminal state; the handler
// stamps arrival time from the benchmarker's own clock before doing anything
// else, so neither JSON parsing nor database work is charged to the
// measurement.
package callbackController

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/callback"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	_ "github.com/Hades-Scheduler/CI-Benchmarker/shared/response"
	"github.com/gin-gonic/gin"
)

// maxCallbackBody caps how much a system under test can push in one callback.
// The load generator is a small VM and several hundred callbacks can land at
// once, so an unbounded read is a memory hazard rather than a convenience.
const maxCallbackBody = 1 << 20 // 1 MiB

// writeTimeout bounds how long a callback may wait for its database write.
// Exceeding it is reported to the caller as a 503 so the system under test
// retries, rather than being swallowed.
const writeTimeout = 20 * time.Second

// StatusCallback documents the accepted request shapes for Swagger. The
// receiver does not bind to this struct; it parses defensively so that both
// the Hades webhook and the Jenkins notification plugin payload are accepted
// without either side having to match a Go struct exactly.
//
// @Description Terminal status notification pushed by a system under test.
type StatusCallback struct {
	// Event is the Hades discriminator. Recorded, never switched on.
	Event string `json:"event,omitempty" example:"job.completed"`
	// JobID is the id the benchmarker submitted under. Hades sends it as
	// job_id; the Jenkins notification plugin returns it in
	// build.parameters.HADES_UUID.
	JobID string `json:"job_id" example:"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"`
	// Status is the terminal state. Hades uses Succeeded/Failed/Stopped,
	// Jenkins uses SUCCESS/FAILURE/UNSTABLE/ABORTED.
	Status string `json:"status" example:"Succeeded"`
	// Reason optionally explains a non-success terminal state.
	Reason string `json:"reason,omitempty" example:"ImagePullBackOff: no such image"`
	// QueuedAt, StartedAt and FinishedAt are the system under test's view.
	// They are stored for provenance and are never used to compute latency.
	QueuedAt   string `json:"queued_at,omitempty" example:"2026-08-21T12:00:00Z"`
	StartedAt  string `json:"started_at,omitempty" example:"2026-08-21T12:00:05Z"`
	FinishedAt string `json:"finished_at,omitempty" example:"2026-08-21T12:00:41Z"`
	// DurationMs is the system under test's own elapsed time.
	DurationMs int64 `json:"duration_ms,omitempty" example:"36000"`
	// Attempt is the sender's redelivery counter; the first delivery is 1.
	Attempt int `json:"attempt,omitempty" example:"1"`
}

// CallbackResponse is what the receiver returns.
//
// @Description Outcome of a status callback.
type CallbackResponse struct {
	JobID         string `json:"job_id"`
	Accepted      bool   `json:"accepted"`
	Duplicate     bool   `json:"duplicate"`
	Terminal      bool   `json:"terminal"`
	DeliveryCount int64  `json:"delivery_count"`
	Source        string `json:"source"`
}

// NewStatusCallbackHandler builds the receiver bound to a persister.
//
// @Summary      Receive a job status callback
// @Description  Terminal-status receiver for every variant. Accepts the Hades status webhook (flat object, job_id + status) and the Jenkins post-build notification (nested under "build", job id in build.parameters.HADES_UUID). Arrival is timestamped on the benchmarker's clock before anything else happens, and the write is idempotent by job id, so a retried delivery bumps a counter instead of moving the measurement. Hades delivers at-least-once and retries any non-2xx, so a failed write is answered with 503 to get the measurement redelivered rather than lost.
// @Tags         callback
// @Accept       json
// @Produce      json
// @Param        statusCallback  body      StatusCallback  true  "Status callback"
// @Success      200  {object}  CallbackResponse  "Terminal status recorded, or recognised as a duplicate"
// @Success      202  {object}  CallbackResponse  "Non-terminal lifecycle notification, logged but not measured"
// @Failure      400  {object}  response.ErrorMessage
// @Failure      413  {object}  response.ErrorMessage
// @Failure      503  {object}  response.ServerErrorMessage
// @Router       /callback [post]
func NewStatusCallbackHandler(store *persister.DBPersister) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Nothing may precede this line. It is the measurement.
		receivedNs := time.Now().UnixNano()

		// MaxBytesReader rather than io.LimitReader: LimitReader truncates
		// silently, so an oversized body arrived as a short one, failed to parse
		// and was recorded as unparseable - a size problem misfiled as a
		// malformed payload, with the truncated bytes polluting the audit log.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCallbackBody)

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				slog.Warn("Callback body exceeded the limit",
					slog.Int64("limit_bytes", tooLarge.Limit))
				c.JSON(http.StatusRequestEntityTooLarge,
					gin.H{"error": "callback body too large"})
				return
			}
			slog.Error("Failed to read callback body", slog.Any("error", err))
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}

		notification, parseErr := callback.Parse(body)
		if parseErr != nil {
			// Record the unparseable POST anyway. A callback that arrived and
			// could not be understood is a missing measurement, and the run
			// should show it rather than look complete.
			recordAudit(c, store, receivedNs, string(body), parseErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": parseErr.Error()})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), writeTimeout)
		defer cancel()

		// Hades sends its delivery metadata in headers as well as in the body.
		// The header wins when both are present: it is what the sender's
		// JetStream consumer actually observed.
		deliveryAttempt := notification.DeliveryAttempt
		if header := c.GetHeader("X-Hades-Attempt"); header != "" {
			if parsed, err := strconv.ParseInt(header, 10, 64); err == nil {
				deliveryAttempt = &parsed
			}
		}

		result, err := store.RecordCallback(ctx, persister.Callback{
			JobID:              notification.JobID,
			ReceivedTimeNs:     receivedNs,
			Source:             notification.Source,
			Status:             notification.Status,
			RawStatus:          notification.RawStatus,
			Reason:             notification.Reason,
			Phase:              notification.Phase,
			Event:              notification.Event,
			Terminal:           notification.Terminal,
			ReportedQueuedNs:   notification.ReportedQueuedNs,
			ReportedStartNs:    notification.ReportedStartNs,
			ReportedEndNs:      notification.ReportedEndNs,
			ReportedDurationMs: notification.ReportedDurationMs,
			DeliveryAttempt:    deliveryAttempt,
			DeliveryID:         c.GetHeader("X-Hades-Delivery"),
			RemoteAddr:         c.ClientIP(),
			RawPayload:         string(body),
		})
		if err != nil {
			// Surfacing this is the point. The previous storage path discarded
			// write errors, so a lost measurement looked exactly like a job
			// that never finished. Hades retries any non-2xx with backoff, so
			// answering 503 here is what turns a failed write into a recovered
			// measurement rather than a hole in the data.
			slog.Error("Failed to record callback",
				slog.String("job_id", notification.JobID), slog.Any("error", err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to record callback, please retry"})
			return
		}

		response := CallbackResponse{
			JobID:         notification.JobID,
			Accepted:      result.Accepted,
			Duplicate:     result.Duplicate,
			Terminal:      result.Terminal,
			DeliveryCount: result.DeliveryCount,
			Source:        notification.Source,
		}

		if !notification.Terminal {
			// 202: understood and logged, but it does not end the job.
			c.JSON(http.StatusAccepted, response)
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

// recordAudit stores an unparseable callback in the append-only event log on a
// best-effort basis. It must not block the 400 response for long.
func recordAudit(c *gin.Context, store *persister.DBPersister, receivedNs int64, body string, parseErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status := "unparseable"
	if errors.Is(parseErr, callback.ErrNoJobID) {
		status = "missing_job_id"
	} else if errors.Is(parseErr, callback.ErrNoStatus) {
		status = "missing_status"
	}

	if _, err := store.RecordCallback(ctx, persister.Callback{
		ReceivedTimeNs: receivedNs,
		Source:         callback.SourceUnknown,
		Status:         status,
		Terminal:       false,
		RemoteAddr:     c.ClientIP(),
		RawPayload:     body,
	}); err != nil {
		slog.Error("Failed to audit malformed callback", slog.Any("error", err))
	}
}
