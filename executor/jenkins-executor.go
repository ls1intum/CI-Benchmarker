package executor

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/ls1intum/hades/shared/payload"
)

// Compile-time check to ensure JenkinsExecutor implements the Executor interface
var _ Executor = (*JenkinsExecutor)(nil)

type JenkinsExecutor struct {
	JenkinsURL    string
	User          string
	APIToken      string
	JobPath       string
	UseParameters bool
}

type crumbResp struct {
	Crumb             string `json:"crumb"`
	CrumbRequestField string `json:"crumbRequestField"`
}

func NewJenkinsExecutor(jenkinsURL string, user string, APIToken string, path string, useParameters bool) *JenkinsExecutor {
	slog.Info("Creating new JenkinsExecutor")
	return &JenkinsExecutor{
		JenkinsURL:    strings.TrimRight(jenkinsURL, "/"),
		User:          user,
		APIToken:      APIToken,
		JobPath:       path,
		UseParameters: useParameters,
	}
}

func (e *JenkinsExecutor) Name() string {
	return "JenkinsExecutor"
}

func (e *JenkinsExecutor) Execute(jobPayload payload.RESTPayload) (uuid.UUID, error) {
	slog.Debug("Executing JenkinsExecutor")

	// Create UUID for this benchmark job
	jobUUID := uuid.New()

	// Inject UUID into payload metadata
	if jobPayload.Metadata == nil {
		jobPayload.Metadata = make(map[string]string)
	}
	jobPayload.Metadata["UUID"] = jobUUID.String()

	slog.Info("Assigned UUID to jobPayload", slog.String("uuid", jobUUID.String()))

	// Validate Jenkins config
	if e.JenkinsURL == "" || e.User == "" || e.APIToken == "" || e.JobPath == "" {
		slog.Debug("JenkinsExecutor not configured properly")
		return jobUUID, errors.New("JenkinsExecutor not configured: need JenkinsURL, User, APIToken, JobPath")
	}

	// Get Jenkins crumb
	crumbField, crumbValue, err := e.getCrumb()
	if err != nil {
		slog.Debug("Error while getting Jenkins crumb")
		return jobUUID, err
	}

	var endpoint string
	var req *http.Request

	// Build request
	if e.UseParameters {
		params, err := e.payloadToParams(jobPayload)
		// NOTE: params now contain the UUID inside HADES_PAYLOAD_JSON
		if err != nil {
			slog.Debug("Error while serializing payload")
			return jobUUID, err
		}

		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/buildWithParameters"
		req, err = http.NewRequest(http.MethodPost, endpoint, strings.NewReader(params.Encode()))
		if err != nil {
			slog.Debug("Error while creating POST request to Jenkins (parameters)")
			return jobUUID, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	} else {
		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/build"
		req, err = http.NewRequest(http.MethodPost, endpoint, nil)
		if err != nil {
			slog.Debug("Error while creating POST request to Jenkins (no parameters)")
			return jobUUID, err
		}
	}

	// Set auth & crumb
	req.SetBasicAuth(e.User, e.APIToken)
	if crumbField != "" && crumbValue != "" {
		req.Header.Set(crumbField, crumbValue)
	}

	// Send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("Error while sending POST request to Jenkins")
		return jobUUID, err
	}
	defer resp.Body.Close()

	// Validate Jenkins response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		slog.Debug("JenkinsExecutor returned non-201/202 status code", slog.Int("status", resp.StatusCode))
		return jobUUID, errors.New("JenkinsExecutor returned non-201/202 status code")
	}

	return jobUUID, nil
}

func (e *JenkinsExecutor) getCrumb() (field string, value string, err error) {
	req, err := http.NewRequest(http.MethodGet, e.JenkinsURL+"/crumbIssuer/api/json", nil)
	if err != nil {
		return "", "", err
	}
	req.SetBasicAuth(e.User, e.APIToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", errors.New("failed to get Jenkins crumb")
	}

	var c crumbResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return "", "", err
	}
	return c.CrumbRequestField, c.Crumb, nil
}

func (e *JenkinsExecutor) payloadToParams(p payload.RESTPayload) (url.Values, error) {
	if p.Metadata == nil {
		p.Metadata = make(map[string]string)
	}

	jenkinsID := uuid.New().String()
	p.Metadata["jenkinsId"] = jenkinsID

	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Set("HADES_PAYLOAD_JSON", string(b))

	return values, nil
}
