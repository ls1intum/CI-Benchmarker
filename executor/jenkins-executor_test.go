package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// The benchmark controller calls Execute from many goroutines at once, so the
// crumb cache is read concurrently with its own refresh. This is a -race test:
// it fails the moment the cached crumb is read without synchronisation.
func TestGetCrumbIsSafeUnderConcurrentUse(t *testing.T) {
	var issued atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issued.Add(1)
		_ = json.NewEncoder(w).Encode(crumbResp{
			Crumb:             "crumb-value",
			CrumbRequestField: "Jenkins-Crumb",
		})
	}))
	defer server.Close()

	exec := NewJenkinsExecutor(server.URL, "user", "token", "job/test", false)

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			field, value, err := exec.getCrumb(context.Background())
			if err != nil {
				t.Errorf("getCrumb: %v", err)
				return
			}
			// The pair must always be consistent, never a new field with a
			// stale value.
			if field != "Jenkins-Crumb" || value != "crumb-value" {
				t.Errorf("torn crumb: field=%q value=%q", field, value)
			}
		}()
	}
	wg.Wait()

	// The cache must actually cache: 64 concurrent callers must not each issue
	// their own crumb request.
	if n := issued.Load(); n > 8 {
		t.Errorf("issued %d crumb requests for 64 concurrent callers, want the cache to collapse them", n)
	}
}

// Jenkins with crumbs disabled must be cached too, rather than re-probed once
// per submitted job.
func TestCrumbDisabledIsCached(t *testing.T) {
	var probes atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exec := NewJenkinsExecutor(server.URL, "user", "token", "job/test", false)

	for i := 0; i < 20; i++ {
		field, value, err := exec.getCrumb(context.Background())
		if err != nil {
			t.Fatalf("getCrumb: %v", err)
		}
		if field != "" || value != "" {
			t.Errorf("crumb-disabled Jenkins returned field=%q value=%q", field, value)
		}
	}

	if n := probes.Load(); n != 1 {
		t.Errorf("probed the crumb issuer %d times, want 1", n)
	}
}
