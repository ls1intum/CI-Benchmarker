package executor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hades-scheduler/hades/shared/payload"
)

// A rejected submission must not leak its response body. The original code put
// the defer after the non-200 early return, so every rejection leaked a body
// and its connection, degrading the load generator over a long run.
//
// The check is indirect but exact: a response body that is drained and closed
// returns its connection to the idle pool, so sequential requests reuse one
// connection. A leaked body cannot be reused, so each request opens a new one.
func TestHadesExecutorClosesBodyOnRejection(t *testing.T) {
	var newConnections atomic.Int64

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"queue full"}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	// A private client so this test's connection accounting is not disturbed
	// by other tests sharing the process-wide client.
	exec := NewHadesExecutor(server.URL, Docker)
	exec.client = NewClient(DefaultClientConfig())

	const attempts = 50
	for i := 0; i < attempts; i++ {
		if _, err := exec.Execute(context.Background(), payload.RESTPayload{}); err == nil {
			t.Fatal("expected an error for a 500 response")
		}
	}

	if opened := newConnections.Load(); opened > 5 {
		t.Errorf("server saw %d new connections for %d sequential rejected submissions; "+
			"response bodies are not being returned to the pool", opened, attempts)
	}
}

func TestHadesExecutorVariantAndHost(t *testing.T) {
	docker := NewHadesExecutor("https://sut-docker.example:8080/api/v1/build", Docker)
	if docker.Variant() != "hades-docker" {
		t.Errorf("Variant = %q", docker.Variant())
	}
	if docker.TargetHost() != "sut-docker.example:8080" {
		t.Errorf("TargetHost = %q", docker.TargetHost())
	}

	k8s := NewHadesExecutor("https://sut-k8s.example/api/v1/build", Kubernetes)
	if k8s.Variant() != "hades-k8s" {
		t.Errorf("Variant = %q", k8s.Variant())
	}

	jenkins := NewJenkinsExecutor("https://sut-jenkins.example/", "u", "t", "job/x", false)
	if jenkins.Variant() != "jenkins" {
		t.Errorf("Variant = %q", jenkins.Variant())
	}
	if jenkins.TargetHost() != "sut-jenkins.example" {
		t.Errorf("TargetHost = %q", jenkins.TargetHost())
	}
}

func TestHadesExecutorReturnsAcknowledgedJobID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"queued","job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}`))
	}))
	defer server.Close()

	jobID, err := NewHadesExecutor(server.URL, Docker).Execute(context.Background(), payload.RESTPayload{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if jobID.String() != "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f" {
		t.Errorf("jobID = %s", jobID)
	}
}

func TestHadesExecutorHonoursContextCancellation(t *testing.T) {
	// The handler hangs until the test releases it, so cancellation has to come
	// from the caller's context rather than from the server finishing.
	handlerRelease := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-handlerRelease:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(handlerRelease)
		server.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := NewHadesExecutor(server.URL, Docker).Execute(ctx, payload.RESTPayload{})
	if err == nil {
		t.Fatal("expected a context deadline error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Execute took %s; it must honour the caller's deadline, not the client timeout", elapsed)
	}
}

// Hades needs somewhere to report terminal status. status_callback_url is a
// separate request field from callback_url, which forwards logs, and both can
// be set independently.
func TestHadesExecutorSendsStatusCallbackURL(t *testing.T) {
	received := make(chan map[string]any, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		received <- body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}`))
	}))
	defer server.Close()

	exec := NewHadesExecutorWithCallback(server.URL, Docker, "https://bench.example/v1/callback")
	if _, err := exec.Execute(context.Background(), payload.RESTPayload{
		Priority:     1,
		QueuePayload: payload.QueuePayload{Name: "job", CallbackURL: "https://bench.example/logs"},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	body := <-received

	if got := body["status_callback_url"]; got != "https://bench.example/v1/callback" {
		t.Errorf("status_callback_url = %v", got)
	}
	// The log callback must survive alongside it, not be replaced by it.
	if got := body["callback_url"]; got != "https://bench.example/logs" {
		t.Errorf("callback_url = %v, the two fields are independent", got)
	}
	// The payload must still flatten to the shape Hades expects.
	if got := body["name"]; got != "job" {
		t.Errorf("name = %v; the embedded payload did not flatten", got)
	}
	if got := body["priority"]; got != float64(1) {
		t.Errorf("priority = %v", got)
	}
}

// With no callback target set, the field must be omitted rather than sent
// empty, so a Hades that does not know the field is unaffected.
func TestHadesExecutorOmitsEmptyStatusCallbackURL(t *testing.T) {
	received := make(chan map[string]any, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		received <- body
		_, _ = w.Write([]byte(`{"job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}`))
	}))
	defer server.Close()

	if _, err := NewHadesExecutor(server.URL, Docker).Execute(context.Background(), payload.RESTPayload{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, present := (<-received)["status_callback_url"]; present {
		t.Error("status_callback_url must be omitted when unset")
	}
}
