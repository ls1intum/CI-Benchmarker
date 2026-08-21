package callbackController

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newReceiver(t *testing.T) (*httptest.Server, *persister.DBPersister) {
	t.Helper()

	store, err := persister.Open(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	router := gin.New()
	router.POST("/v1/callback", NewStatusCallbackHandler(store))
	server := httptest.NewServer(router)

	t.Cleanup(func() {
		server.Close()
		_ = store.Close()
	})

	return server, store
}

func postCallback(t *testing.T, client *http.Client, url, body string) (int, CallbackResponse) {
	t.Helper()

	resp, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var decoded CallbackResponse
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

func TestReceiverRecordsTerminalCallback(t *testing.T) {
	server, store := newReceiver(t)

	before := time.Now().UnixNano()
	status, body := postCallback(t, server.Client(), server.URL+"/v1/callback",
		`{"job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f","status":"Succeeded"}`)
	after := time.Now().UnixNano()

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !body.Accepted || body.Duplicate {
		t.Errorf("response = %+v, want accepted and not duplicate", body)
	}

	// Arrival must be stamped on the benchmarker's clock, inside the window the
	// test observed, and not taken from the payload.
	var received int64
	if err := store.DB().QueryRow(`SELECT received_time_ns FROM job_callback`).Scan(&received); err != nil {
		t.Fatalf("read job_callback: %v", err)
	}
	if received < before || received > after {
		t.Errorf("received_time_ns %d outside the request window [%d, %d]", received, before, after)
	}
}

// The measurement must come from the benchmarker's clock, never from a
// timestamp the system under test supplies. A SUT with a skewed clock must not
// be able to move its own numbers.
func TestReceiverIgnoresPayloadTimestampsForMeasurement(t *testing.T) {
	server, store := newReceiver(t)

	// A payload claiming to have finished in 1970.
	status, _ := postCallback(t, server.Client(), server.URL+"/v1/callback",
		`{"job_id":"job-skewed","status":"Succeeded","finished_at":"1970-01-01T00:00:00Z"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	var received, reportedEnd int64
	if err := store.DB().QueryRow(
		`SELECT received_time_ns, COALESCE(reported_end_time_ns, -1) FROM job_callback WHERE job_id = 'job-skewed'`,
	).Scan(&received, &reportedEnd); err != nil {
		t.Fatalf("read job_callback: %v", err)
	}
	if received < time.Now().Add(-time.Minute).UnixNano() {
		t.Errorf("received_time_ns = %d, the SUT-reported time leaked into the measurement", received)
	}
	if reportedEnd != 0 {
		t.Errorf("reported_end_time_ns = %d, want the SUT's claim preserved as provenance (0 = epoch)", reportedEnd)
	}
}

func TestReceiverNonTerminalReturns202(t *testing.T) {
	server, store := newReceiver(t)

	status, body := postCallback(t, server.Client(), server.URL+"/v1/callback",
		`{"job_id":"job-1","status":"Running"}`)
	if status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", status)
	}
	if body.Terminal || body.Accepted {
		t.Errorf("response = %+v, want neither terminal nor accepted", body)
	}

	count, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if count != 0 {
		t.Errorf("job_callback count = %d, want 0", count)
	}
}

func TestReceiverMalformedPayloads(t *testing.T) {
	server, store := newReceiver(t)

	bodies := []string{
		`not json`,
		`{"status":"Succeeded"}`,
		`{"job_id":"job-1"}`,
		`[]`,
		``,
	}

	for _, body := range bodies {
		status, _ := postCallback(t, server.Client(), server.URL+"/v1/callback", body)
		if status != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, status)
		}
	}

	// No malformed payload may create a measurement.
	callbacks, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if callbacks != 0 {
		t.Errorf("job_callback count = %d, want 0", callbacks)
	}

	// But every one of them must be visible in the audit log, because an
	// unparseable callback is a lost measurement and the run must show it.
	events, err := store.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if events != len(bodies) {
		t.Errorf("job_event count = %d, want %d", events, len(bodies))
	}
}

// The receiver must absorb a burst of near-simultaneous callbacks without
// dropping any. This is the real risk: the load generator is a small VM, all
// jobs of a run finish at roughly the same time, and SQLite takes one writer.
func TestReceiverSurvivesCallbackStorm(t *testing.T) {
	const jobs = 400

	server, store := newReceiver(t)
	client := stormClient(jobs)

	var (
		okCount   atomic.Int64
		failCount atomic.Int64
		firstErr  atomic.Value
	)

	release := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			<-release // fire as close to simultaneously as the runtime allows

			body := fmt.Sprintf(`{"job_id":"job-%04d","status":"Succeeded"}`, seq)
			resp, err := client.Post(server.URL+"/v1/callback", "application/json", strings.NewReader(body))
			if err != nil {
				failCount.Add(1)
				firstErr.CompareAndSwap(nil, err.Error())
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				failCount.Add(1)
				firstErr.CompareAndSwap(nil, fmt.Sprintf("status %d", resp.StatusCode))
				return
			}
			okCount.Add(1)
		}(i)
	}

	start := time.Now()
	close(release)
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("storm: %d callbacks in %s (%.0f/s), %d ok, %d failed",
		jobs, elapsed.Round(time.Millisecond), float64(jobs)/elapsed.Seconds(),
		okCount.Load(), failCount.Load())

	if failCount.Load() != 0 {
		t.Fatalf("%d callbacks failed, first error: %v", failCount.Load(), firstErr.Load())
	}

	stored, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if stored != jobs {
		t.Fatalf("stored %d callbacks, want %d; a burst must not lose a measurement", stored, jobs)
	}
}

// The same storm with every job delivered three times, which is what a
// retrying system under test looks like. Exactly one delivery per job may be
// accepted, or the sample is inflated.
func TestReceiverStormWithDuplicatesCountsEachJobOnce(t *testing.T) {
	const (
		jobs       = 200
		deliveries = 3
	)

	server, store := newReceiver(t)
	client := stormClient(jobs * deliveries)

	var accepted atomic.Int64
	release := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < jobs; i++ {
		for d := 0; d < deliveries; d++ {
			wg.Add(1)
			go func(seq int) {
				defer wg.Done()
				<-release

				body := fmt.Sprintf(`{"job_id":"job-%04d","status":"Succeeded"}`, seq)
				resp, err := client.Post(server.URL+"/v1/callback", "application/json", strings.NewReader(body))
				if err != nil {
					t.Errorf("POST: %v", err)
					return
				}
				defer resp.Body.Close()

				var decoded CallbackResponse
				if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
					t.Errorf("decode: %v", err)
					return
				}
				if decoded.Accepted {
					accepted.Add(1)
				}
			}(i)
		}
	}

	start := time.Now()
	close(release)
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("duplicate storm: %d POSTs for %d jobs in %s, %d accepted",
		jobs*deliveries, jobs, elapsed.Round(time.Millisecond), accepted.Load())

	if accepted.Load() != jobs {
		t.Errorf("accepted %d callbacks, want exactly %d (one per job)", accepted.Load(), jobs)
	}

	stored, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if stored != jobs {
		t.Errorf("stored %d measurements, want %d", stored, jobs)
	}

	events, err := store.CountEvents(context.Background())
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if events != jobs*deliveries {
		t.Errorf("job_event count = %d, want every POST audited (%d)", events, jobs*deliveries)
	}
}

// A storm arriving while a long read is in flight.
//
// This is the failure mode the old configuration was actually exposed to:
// db.SetMaxOpenConns(1) meant writes and reads shared one connection, so a
// histogram or export query held the only connection and every callback queued
// behind it - with StoreResult discarding its error and using
// context.Background(), a callback lost there left no trace at all.
func TestReceiverStormDuringLongRead(t *testing.T) {
	const jobs = 400

	server, store := newReceiver(t)
	client := stormClient(jobs)

	// Hold a read transaction open for the duration of the storm.
	readTx, err := store.DB().Begin()
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	var ignored int
	if err := readTx.QueryRow(`SELECT COUNT(*) FROM job_callback`).Scan(&ignored); err != nil {
		t.Fatalf("read query: %v", err)
	}

	var failures atomic.Int64
	release := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			<-release

			body := fmt.Sprintf(`{"job_id":"job-%04d","status":"Succeeded"}`, seq)
			resp, err := client.Post(server.URL+"/v1/callback", "application/json", strings.NewReader(body))
			if err != nil {
				failures.Add(1)
				return
			}
			if resp.StatusCode != http.StatusOK {
				failures.Add(1)
			}
			_ = resp.Body.Close()
		}(i)
	}

	start := time.Now()
	close(release)
	wg.Wait()
	elapsed := time.Since(start)

	_ = readTx.Rollback()

	t.Logf("storm during long read: %d callbacks in %s, %d failed", jobs, elapsed.Round(time.Millisecond), failures.Load())

	if failures.Load() != 0 {
		t.Errorf("%d callbacks failed while a read was in flight", failures.Load())
	}

	stored, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if stored != jobs {
		t.Errorf("stored %d callbacks, want %d", stored, jobs)
	}
}

// A larger storm than any planned run, to establish headroom rather than to
// prove the exact planned size works. Skipped under -short.
func TestReceiverStormHeadroom(t *testing.T) {
	if testing.Short() {
		t.Skip("headroom storm skipped in short mode")
	}

	const jobs = 5000

	server, store := newReceiver(t)
	client := stormClient(512)

	var failures atomic.Int64
	release := make(chan struct{})
	var wg sync.WaitGroup

	// 512 senders rather than 5000 goroutines: the point is to saturate the
	// receiver, not to measure the client's goroutine scheduler.
	const senders = 512
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func(sender int) {
			defer wg.Done()
			<-release
			for seq := sender; seq < jobs; seq += senders {
				body := fmt.Sprintf(`{"job_id":"job-%05d","status":"Succeeded"}`, seq)
				resp, err := client.Post(server.URL+"/v1/callback", "application/json", strings.NewReader(body))
				if err != nil {
					failures.Add(1)
					continue
				}
				if resp.StatusCode != http.StatusOK {
					failures.Add(1)
				}
				_ = resp.Body.Close()
			}
		}(s)
	}

	start := time.Now()
	close(release)
	wg.Wait()
	elapsed := time.Since(start)

	stored, err := store.CountCallbacks(context.Background())
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}

	t.Logf("headroom storm: %d callbacks from %d senders in %s (%.0f/s), %d stored, %d failed",
		jobs, senders, elapsed.Round(time.Millisecond), float64(jobs)/elapsed.Seconds(), stored, failures.Load())

	if failures.Load() != 0 {
		t.Errorf("%d callbacks failed", failures.Load())
	}
	if stored != jobs {
		t.Errorf("stored %d callbacks, want %d", stored, jobs)
	}
}

func stormClient(connections int) *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        connections,
			MaxIdleConnsPerHost: connections,
			MaxConnsPerHost:     0,
		},
	}
}

// Hades delivers at-least-once from a JetStream durable consumer and retries
// on any non-2xx, so redelivery of the same job is routine. The redelivered
// copy must be recognised and must not move the measurement, and its delivery
// metadata must be recorded so the redelivery is visible afterwards.
func TestReceiverHandlesHadesRedelivery(t *testing.T) {
	server, store := newReceiver(t)

	const body = `{"event":"job.completed","job_id":"7f3a1c2b-1111-2222-3333-444455556666",` +
		`"status":"Succeeded","finished_at":"2026-08-21T12:00:41Z","attempt":%d}`

	post := func(attempt int, delivery string) CallbackResponse {
		t.Helper()

		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/callback",
			strings.NewReader(fmt.Sprintf(body, attempt)))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hades-Event", "job.completed")
		req.Header.Set("X-Hades-Job-Id", "7f3a1c2b-1111-2222-3333-444455556666")
		req.Header.Set("X-Hades-Attempt", fmt.Sprint(attempt))
		req.Header.Set("X-Hades-Delivery", delivery)

		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; a non-2xx would make Hades retry forever", resp.StatusCode)
		}

		var decoded CallbackResponse
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		return decoded
	}

	first := post(1, "delivery-a")
	if !first.Accepted {
		t.Error("first delivery must be accepted")
	}

	second := post(2, "delivery-b")
	if second.Accepted {
		t.Error("a redelivery must not be accepted as a new measurement")
	}
	if !second.Duplicate || second.DeliveryCount != 2 {
		t.Errorf("redelivery = %+v, want duplicate with delivery_count 2", second)
	}

	// The sender's own attempt counter is stored, so a run where Hades had to
	// retry is distinguishable from one where it did not.
	var attempt int64
	if err := store.DB().QueryRow(
		`SELECT delivery_attempt FROM job_callback WHERE job_id = '7f3a1c2b-1111-2222-3333-444455556666'`,
	).Scan(&attempt); err != nil {
		t.Fatalf("read delivery_attempt: %v", err)
	}
	if attempt != 1 {
		t.Errorf("delivery_attempt = %d, want the attempt of the accepted delivery (1)", attempt)
	}

	// Both deliveries are in the audit log with their delivery ids.
	var deliveries int
	if err := store.DB().QueryRow(
		`SELECT COUNT(DISTINCT delivery_id) FROM job_event WHERE delivery_id IS NOT NULL`,
	).Scan(&deliveries); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveries != 2 {
		t.Errorf("recorded %d distinct deliveries, want 2", deliveries)
	}
}

// An oversized callback must be rejected as oversized. io.LimitReader truncated
// it silently, so it arrived as a short body, failed to parse, and was filed as
// unparseable - a size problem recorded as a malformed payload, with the
// truncated bytes stored in the audit log.
func TestOversizedCallbackIsRejectedAsTooLarge(t *testing.T) {
	server, store := newReceiver(t)

	// Valid JSON, just far past the cap.
	padding := strings.Repeat("x", maxCallbackBody+1024)
	body := `{"job_id":"6f1a5b1e-6d1a-4f5f-9d0e-1a2b3c4d5e6f","status":"succeeded","note":"` + padding + `"}`

	status, _ := postCallback(t, server.Client(), server.URL+"/v1/callback", body)
	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", status, http.StatusRequestEntityTooLarge)
	}

	// It must not have been recorded as a completed measurement.
	var callbacks int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM job_callback`).Scan(&callbacks); err != nil {
		t.Fatalf("count callbacks: %v", err)
	}
	if callbacks != 0 {
		t.Errorf("an oversized body produced %d terminal callbacks, want 0", callbacks)
	}
}
