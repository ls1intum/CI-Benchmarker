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

type JenkinsExecutor struct {
	Executor
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

	if e.JenkinsURL == "" || e.User == "" || e.APIToken == "" || e.JobPath == "" {
		slog.Debug("JenkinsExecutor not configured properly")
		return uuid.UUID{}, errors.New("JenkinsExecutor not configured: need JenkinsURL, User, APIToken, JobPath")
	}

	crumbField, crumbValue, err := e.getCrumb()
	if err != nil {
		slog.Debug("Error while getting Jenkins crumb")
		return uuid.UUID{}, err
	}

	var endpoint string
	var req *http.Request

	if e.UseParameters {
		params, err := e.payloadToParams(jobPayload)
		if err != nil {
			slog.Debug("Error while serializing payload")
			return uuid.UUID{}, err
		}
		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/buildWithParameters"

		req, err = http.NewRequest(http.MethodPost, endpoint, strings.NewReader(params.Encode()))
		if err != nil {
			slog.Debug("Error while creating POST request to Jenkins (parameters)")
			return uuid.UUID{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/build"

		var err error
		req, err = http.NewRequest(http.MethodPost, endpoint, nil)
		if err != nil {
			slog.Debug("Error while creating POST request to Jenkins (no parameters)")
			return uuid.UUID{}, err
		}
	}

	req.SetBasicAuth(e.User, e.APIToken)
	if crumbField != "" && crumbValue != "" {
		req.Header.Set(crumbField, crumbValue)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("Error while sending POST request to Jenkins")
		return uuid.UUID{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		slog.Debug("JenkinsExecutor returned non-201/202 status code", slog.Int("status", resp.StatusCode))
		return uuid.UUID{}, errors.New("JenkinsExecutor returned non-201/202 status code")
	}

	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		slog.Debug("JenkinsExecutor missing Location header")
		return uuid.UUID{}, errors.New("JenkinsExecutor response missing Location header (queue item url)")
	}

	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.TrimRight(loc, "/")))
	slog.Info("JenkinsExecutor queued successfully", slog.String("queue_url", loc), slog.Any("jobID", id))

	return id, nil
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
	values := url.Values{}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	values.Set("HADES_PAYLOAD_JSON", string(b))
	return values, nil
}
