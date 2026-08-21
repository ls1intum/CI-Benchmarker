package executor

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hades-scheduler/hades/shared/payload"
)

// A rejected submission must not leak its response body.
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

	exec := NewHadesExecutor(server.URL, Docker)

	const attempts = 30
	for i := 0; i < attempts; i++ {
		if _, err := exec.Execute(payload.RESTPayload{}); err == nil {
			t.Fatal("expected an error for a 500 response")
		}
	}

	if opened := newConnections.Load(); opened > 5 {
		t.Errorf("server saw %d new connections for %d sequential rejected submissions; "+
			"response bodies are not being returned to the pool", opened, attempts)
	}
}

func TestHadesExecutorReturnsAcknowledgedJobID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"queued","job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f"}`))
	}))
	defer server.Close()

	jobID, err := NewHadesExecutor(server.URL, Docker).Execute(payload.RESTPayload{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if jobID.String() != "6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f" {
		t.Errorf("jobID = %s", jobID)
	}
}
