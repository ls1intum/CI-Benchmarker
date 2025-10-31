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

// HadesExecutor is the executor for Hades
type HadesExecutor struct {
	Executor
	HadesURL string
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

type HadesDockerExecutor struct{ *HadesExecutor }

func NewHadesDockerExecutor(hadesURL string) *HadesDockerExecutor {
	slog.Info("Creating new HadesDockerExecutor")
	return &HadesDockerExecutor{
		HadesExecutor: &HadesExecutor{
			HadesURL: hadesURL,
		},
	}
}

func (e *HadesDockerExecutor) Name() string {
	return "HadesDockerExecutor"
}

type HadesKubernetesExecutor struct{ *HadesExecutor }

func NewHadesKubernetesExecutor(hadesURL string) *HadesKubernetesExecutor {
	slog.Info("Creating new HadesKubernetesExecutor")
	return &HadesKubernetesExecutor{
		HadesExecutor: &HadesExecutor{
			HadesURL: hadesURL,
		},
	}
}

func (e *HadesKubernetesExecutor) Name() string {
	return "HadesKubernetesExecutor"
}
