package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/hades-scheduler/hades/shared/payload"
)

// Compile-time check to ensure HadesExecutor implements the Executor interface
var _ Executor = (*HadesExecutor)(nil)

// ExecutorType defines the type of executor
type ExecutorType string

const (
	Docker     ExecutorType = "Docker"
	Kubernetes ExecutorType = "Kubernetes"
)

// HadesExecutor submits jobs to a Hades scheduler over REST.
type HadesExecutor struct {
	executorType ExecutorType
	HadesURL     string
	// StatusCallbackURL is where Hades should POST the terminal status. It is
	// the request-side half of the measurement: without it the scheduler has
	// nowhere to report completion and the benchmarker would be back to
	// inferring it.
	StatusCallbackURL string
	client            *http.Client
}

// hadesRequest is the wire body sent to Hades.
//
// status_callback_url is a per-request field on the Hades build request
// (hades PR #507), distinct from the existing callback_url, which forwards
// aggregated logs. It is declared here rather than being taken from
// shared/payload because the pinned version of that module predates the field;
// the embedded RESTPayload flattens on marshal, so the wire shape is identical
// to what Hades expects.
type hadesRequest struct {
	payload.RESTPayload
	StatusCallbackURL string `json:"status_callback_url,omitempty"`
}

func NewHadesExecutor(hadesURL string, executorType ExecutorType) *HadesExecutor {
	return NewHadesExecutorWithCallback(hadesURL, executorType, "")
}

// NewHadesExecutorWithCallback builds an executor that asks Hades to report
// terminal status to statusCallbackURL.
func NewHadesExecutorWithCallback(hadesURL string, executorType ExecutorType, statusCallbackURL string) *HadesExecutor {
	slog.Info("Creating new HadesExecutor",
		slog.String("type", string(executorType)),
		slog.String("status_callback_url", statusCallbackURL))

	if executorType != Docker && executorType != Kubernetes {
		slog.Warn("Invalid executor type, defaulting to Docker", slog.String("executorType", string(executorType)))
		executorType = Docker
	}
	return &HadesExecutor{
		executorType:      executorType,
		HadesURL:          hadesURL,
		StatusCallbackURL: statusCallbackURL,
		client:            SharedClient(),
	}
}

func (e *HadesExecutor) Execute(ctx context.Context, jobPayload payload.RESTPayload) (uuid.UUID, error) {
	jobPayloadBytes, err := json.Marshal(hadesRequest{
		RESTPayload:       jobPayload,
		StatusCallbackURL: e.StatusCallbackURL,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal job payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.HadesURL, bytes.NewReader(jobPayloadBytes))
	if err != nil {
		return uuid.Nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("post to hades: %w", err)
	}
	// Closed before any early return. Previously the defer sat after the
	// non-200 check, so every rejected submission leaked its response body and
	// its connection, which then degraded the load generator over a long run.
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return uuid.Nil, fmt.Errorf("hades returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	var result struct {
		Message string `json:"message"`
		JobID   string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return uuid.Nil, fmt.Errorf("decode hades response: %w", err)
	}

	jobID, err := uuid.Parse(result.JobID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse job_id %q: %w", result.JobID, err)
	}

	slog.Debug("HadesExecutor scheduled job", slog.Any("jobID", jobID))
	return jobID, nil
}

func (e *HadesExecutor) Name() string {
	return fmt.Sprintf("Hades%sExecutor", string(e.executorType))
}

func (e *HadesExecutor) Variant() string {
	if e.executorType == Kubernetes {
		return "hades-k8s"
	}
	return "hades-docker"
}

func (e *HadesExecutor) TargetHost() string {
	return hostOf(e.HadesURL)
}

// hostOf reduces a target URL to its host, which is what belongs in the
// provenance columns. Falls back to the raw string for unparseable inputs so
// provenance is never silently empty.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}
