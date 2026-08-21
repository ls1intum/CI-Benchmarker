package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/hades-scheduler/hades/shared/payload"
)

// Compile-time check to ensure JenkinsExecutor implements the Executor interface
var _ Executor = (*JenkinsExecutor)(nil)

type JenkinsExecutor struct {
	JenkinsURL    string
	User          string
	APIToken      string
	JobPath       string
	UseParameters bool

	client *http.Client

	// crumb is an immutable snapshot swapped in atomically, so readers never
	// observe a half-updated field/value pair. crumbMu serialises refreshes only,
	// so one slow crumb fetch does not stall every concurrent submission.
	crumb   atomic.Pointer[crumbCache]
	crumbMu sync.Mutex
}

// crumbCache is one cached Jenkins CSRF crumb. An empty field means Jenkins has
// crumbs disabled; the entry is still cached so that is not re-probed per job.
type crumbCache struct {
	field  string
	value  string
	expiry time.Time
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
		client:        SharedClient(),
	}
}

func (e *JenkinsExecutor) Name() string {
	return "JenkinsExecutor"
}

func (e *JenkinsExecutor) Execute(ctx context.Context, jobPayload payload.RESTPayload) (uuid.UUID, error) {

	// Create UUID for this benchmark job
	jobUUID := uuid.New()
	jobPayload.QueuePayload.ID = jobUUID

	slog.Debug("UUID generated:", slog.String("uuid", jobUUID.String()))

	// Validate Jenkins config
	if e.JenkinsURL == "" || e.User == "" || e.APIToken == "" || e.JobPath == "" {
		slog.Debug("JenkinsExecutor not configured properly")
		return jobUUID, errors.New("JenkinsExecutor not configured: need JenkinsURL, User, APIToken, JobPath")
	}

	// Get Jenkins crumb
	crumbField, crumbValue, err := e.getCrumb(ctx)
	if err != nil {
		slog.Debug("Error while getting Jenkins crumb")
		return jobUUID, err
	}

	var endpoint string
	var req *http.Request

	// Build request
	if e.UseParameters {
		params, err := e.payloadToParams(jobPayload)
		if err != nil {
			slog.Debug("Error while serializing payload")
			return jobUUID, err
		}

		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/buildWithParameters"
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
		if err != nil {
			slog.Debug("Error while creating POST request to Jenkins (parameters)")
			return jobUUID, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	} else {
		endpoint = e.JenkinsURL + "/" + strings.TrimLeft(e.JobPath, "/") + "/build"
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
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
	resp, err := e.client.Do(req)
	if err != nil {
		return jobUUID, fmt.Errorf("post to jenkins: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
	}()

	// Validate Jenkins response
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return jobUUID, fmt.Errorf("jenkins returned status %d, expected 201 or 202", resp.StatusCode)
	}

	return jobUUID, nil
}

func (e *JenkinsExecutor) Variant() string {
	return "jenkins"
}

func (e *JenkinsExecutor) TargetHost() string {
	return hostOf(e.JenkinsURL)
}

func (e *JenkinsExecutor) getCrumb(ctx context.Context) (field string, value string, err error) {

	now := time.Now()

	// Lock-free read of the current snapshot. The benchmark controller calls
	// Execute from many goroutines at once, so this must not be a plain field
	// read racing the refresh below.
	if c := e.crumb.Load(); c != nil && now.Before(c.expiry) {
		return c.field, c.value, nil
	}

	e.crumbMu.Lock()
	defer e.crumbMu.Unlock()

	// Another goroutine may have refreshed while this one waited.
	if c := e.crumb.Load(); c != nil && time.Now().Before(c.expiry) {
		return c.field, c.value, nil
	}

	// -----------------------------
	// Actually fetch new crumb
	// -----------------------------
	slog.Info("Fetching new Jenkins crumb...")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.JenkinsURL+"/crumbIssuer/api/json", nil)
	if err != nil {
		return "", "", err
	}
	req.SetBasicAuth(e.User, e.APIToken)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Jenkins has crumbs disabled. Cache that fact so it is not re-probed
		// once per submitted job.
		e.crumb.Store(&crumbCache{expiry: now.Add(30 * time.Minute)})
		return "", "", nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", errors.New("failed to get Jenkins crumb")
	}

	var c crumbResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return "", "", err
	}

	// Published as one immutable value, so a reader cannot see a new field with
	// an old value.
	fresh := &crumbCache{
		field:  c.CrumbRequestField,
		value:  c.Crumb,
		expiry: now.Add(45 * time.Minute), // safer than 1h, Jenkins default
	}
	e.crumb.Store(fresh)

	slog.Info("Fetched new crumb", "expires_at", fresh.expiry.String())

	return fresh.field, fresh.value, nil
}

func (e *JenkinsExecutor) payloadToParams(p payload.RESTPayload) (url.Values, error) {
	if p.Metadata == nil {
		p.Metadata = make(map[string]string)
	}

	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Set("HADES_PAYLOAD_JSON", string(b))
	values.Set("HADES_UUID", p.ID.String())

	return values, nil
}
