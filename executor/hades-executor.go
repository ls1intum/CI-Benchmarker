package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/ls1intum/hades/shared/payload"
)

// Compile-time check to ensure HadesDockerExecutor and HadesKubernetesExecutor implement the Executor interface
var _ Executor = (*HadesExecutor)(nil)

// ExecutorType defines the type of executor
type ExecutorType string

const (
	Docker     ExecutorType = "Docker"
	Kubernetes ExecutorType = "Kubernetes"
)

// HadesExecutor is the executor for Hades
type HadesExecutor struct {
	executorType ExecutorType
	HadesURL     string
}

func (e *HadesExecutor) Execute(jobPayload payload.RESTPayload) (uuid.UUID, error) {
	slog.Debug("Executing HadesExecutor")

	jobPayloadBytes, err := json.Marshal(jobPayload)
	if err != nil {
		slog.Debug("Error while marshalling job payload")
		return uuid.UUID{}, err
	}

	// schedule job - send the http post request to hades
	resp, err := http.Post(e.HadesURL, "application/json", bytes.NewBufferString(string(jobPayloadBytes)))
	if err != nil {
		slog.Debug("Error while sending POST request to Hades")
		return uuid.UUID{}, err
	}
	if resp.StatusCode != http.StatusOK {
		slog.Debug(fmt.Sprintf("HadesExecutor returned status code %d", resp.StatusCode))
		return uuid.UUID{}, fmt.Errorf("HadesExecutor returned non-200 status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	slog.Debug("HadesExecutor response", slog.Any("response", resp))

	// Read the response body
	var result struct {
		Message string `json:"message"`
		JobID   string `json:"job_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("Error while decoding response body")
		return uuid.UUID{}, err
	}

	slog.Debug("HadesExecutor response", slog.Any("result", result))

	// Parse the job_id
	jobID, err := uuid.Parse(result.JobID)
	if err != nil {
		return uuid.UUID{}, err
	}

	slog.Info("HadesExecutor scheduled successfully", slog.Any("jobID", jobID))

	return jobID, nil
}

func NewHadesExecutor(hadesURL string, executorType ExecutorType) *HadesExecutor {
	slog.Info("Creating new HadesExecutor")
	if executorType != Docker && executorType != Kubernetes {
		slog.Warn("Invalid executor type, defaulting to Docker", slog.String("executorType", string(executorType)))
		executorType = Docker
	}
	return &HadesExecutor{
		executorType: executorType,
		HadesURL:     hadesURL,
	}
}

func (e *HadesExecutor) Name() string {
	return fmt.Sprintf("Hades%sExecutor", string(e.executorType))
}
