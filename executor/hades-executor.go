package executor

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

const job = `{
"name": "Example Job",
"metadata": {
  "GLOBAL": "test"
},
"timestamp": "2021-01-01T00:00:00.000Z",
"priority": 3, 
"steps": [
  {
    "id": 1,
    "name": "Clone",
    "image": "ghcr.io/ls1intum/hades/hades-clone-container:latest",
    "metadata": {
      "REPOSITORY_DIR": "/shared",
      "HADES_TEST_URL": "https://github.com/Mtze/Artemis-Java-Test.git",
      "HADES_TEST_PATH": "./example",
      "HADES_TEST_ORDER": "1",
      "HADES_ASSIGNMENT_URL": "https://github.com/Mtze/Artemis-Java-Solution.git",
      "HADES_ASSIGNMENT_PATH": "./example/assignment",
      "HADES_ASSIGNMENT_ORDER": "2"
    }
  },
  {
    "id": 2,
    "name": "Execute",
    "image": "ls1tum/artemis-maven-template:java17-18",
    "script": "set +e && cd ./shared/example || exit 0 && ./gradlew --status || exit 0 && ./gradlew clean test || exit 0"
  },
  {
    "id": 3,
    "name": "result",
    "image": "ghcr.io/ls1intum/hades/junit-result-parser:latest",
    "metadata": {
      "API_ENDPOINT": "http://host.docker.internal:8080/v1/result"
  }
  }
  ]
}`

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

func (e *HadesExecutor) Execute() (uuid.UUID, error) {
	slog.Debug("Executing HadesExecutor")

	// schedule job - send the http post request to hades
	resp, err := http.Post(e.HadesURL, "application/json", bytes.NewBufferString(job))
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
