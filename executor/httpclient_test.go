package executor

import (
	"net/http"
	"testing"
	"time"
)

// Every executor must be driven by the same client. A comparison between
// variants is only meaningful if the offered load is identical, and previously
// it was not: Hades used http.DefaultClient while Jenkins used a private
// client with a different timeout and a hard connection cap.
func TestAllExecutorsShareOneClient(t *testing.T) {
	hades := NewHadesExecutor("http://sut-docker.example/api/v1/build", Docker)
	k8s := NewHadesExecutor("http://sut-k8s.example/api/v1/build", Kubernetes)
	jenkins := NewJenkinsExecutor("http://sut-jenkins.example", "user", "token", "job/x", false)

	if hades.client != k8s.client || hades.client != jenkins.client {
		t.Error("executors do not share one HTTP client")
	}
	if hades.client != SharedClient() {
		t.Error("executors are not using the shared client")
	}
}

// MaxConnsPerHost is a hard cap: request N+1 blocks waiting for a free
// connection and the wait counts against the client timeout, so it both
// throttles the offered load and manufactures submission failures. Concurrency
// must be shaped by the benchmark pacer instead.
func TestDefaultClientHasNoHardConnectionCap(t *testing.T) {
	cfg := DefaultClientConfig()
	if cfg.MaxConnsPerHost != 0 {
		t.Errorf("MaxConnsPerHost = %d, want 0 (unlimited)", cfg.MaxConnsPerHost)
	}
	if cfg.MaxIdleConnsPerHost < 100 {
		t.Errorf("MaxIdleConnsPerHost = %d, too low to keep connections warm under load", cfg.MaxIdleConnsPerHost)
	}
	if cfg.Timeout == 0 {
		t.Error("Timeout is 0; a submission that never returns would hang the run forever")
	}
}

func TestNewClientAppliesConfig(t *testing.T) {
	client := NewClient(ClientConfig{
		Timeout:             7 * time.Second,
		DialTimeout:         3 * time.Second,
		MaxIdleConns:        11,
		MaxIdleConnsPerHost: 12,
		MaxConnsPerHost:     13,
		IdleConnTimeout:     14 * time.Second,
	})

	if client.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport has type %T", client.Transport)
	}
	if transport.MaxIdleConnsPerHost != 12 || transport.MaxConnsPerHost != 13 {
		t.Errorf("transport caps = %d idle / %d max", transport.MaxIdleConnsPerHost, transport.MaxConnsPerHost)
	}
}
