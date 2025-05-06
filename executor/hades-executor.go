package executor

import (
	"bytes"
	"encoding/json"
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

func NewHadesExecutor(hadesURL string) *HadesExecutor {
	slog.Info("Creating new HadesExecutor")
	return &HadesExecutor{
		HadesURL: hadesURL,
	}
}

func (e *HadesExecutor) Name() string {
	return "HadesExecutor"
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
