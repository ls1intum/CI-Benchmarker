package benchmarkController

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/executor"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hades-scheduler/hades/shared/payload"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeExecutor stands in for a system under test and records exactly when it
// was entered and left, so the timing contract can be asserted rather than
// assumed.
type fakeExecutor struct {
	delay   time.Duration
	failFor func(seq int) bool

	mu        sync.Mutex
	entered   []time.Time
	left      []time.Time
	inFlight  atomic.Int64
	maxFlight atomic.Int64
	calls     atomic.Int64
}

func (f *fakeExecutor) Execute(ctx context.Context, _ payload.RESTPayload) (uuid.UUID, error) {
	seq := int(f.calls.Add(1)) - 1

	current := f.inFlight.Add(1)
	for {
		peak := f.maxFlight.Load()
		if current <= peak || f.maxFlight.CompareAndSwap(peak, current) {
			break
		}
	}
	defer f.inFlight.Add(-1)

	entered := time.Now()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	left := time.Now()

	f.mu.Lock()
	f.entered = append(f.entered, entered)
	f.left = append(f.left, left)
	f.mu.Unlock()

	if f.failFor != nil && f.failFor(seq) {
		return uuid.Nil, errors.New("simulated submission failure")
	}
	return uuid.New(), nil
}

func (f *fakeExecutor) Name() string       { return "FakeExecutor" }
func (f *fakeExecutor) Variant() string    { return "fake" }
func (f *fakeExecutor) TargetHost() string { return "sut-fake.example" }

var _ executor.Executor = (*fakeExecutor)(nil)

func newTestBenchmark(t *testing.T, exec executor.Executor) (Benchmark, *persister.DBPersister) {
	t.Helper()

	store, err := persister.Open(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	return Benchmark{
		Executor:  exec,
		Persister: store,
		Config:    config.Config{DefaultConcurrency: 64},
	}, store
}

func runBenchmark(t *testing.T, b Benchmark, query string) RunResponse {
	t.Helper()

	router := gin.New()
	router.POST("/v1/benchmark/fake", b.HandleFunc)

	body := `{"priority":3,"name":"test-workload","steps":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/benchmark/fake?"+query, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response RunResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

// submit_time must be read BEFORE the executor is entered and submit_ack_time
// AFTER it returns. The old code read a single time.Now() after Execute
// returned and stored it as the job's creation time, so queue latency
// under-reported by exactly the submission round-trip.
func TestSubmitTimeIsTakenBeforeExecute(t *testing.T) {
	const submissionDelay = 120 * time.Millisecond

	exec := &fakeExecutor{delay: submissionDelay}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark, "count=1&concurrency=1")

	rows := exportRows(t, store, response.RunID)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	row := rows[0]

	exec.mu.Lock()
	entered, left := exec.entered[0], exec.left[0]
	exec.mu.Unlock()

	if row.SubmitTimeNs > entered.UnixNano() {
		t.Errorf("submit_time_ns %d is after the executor was entered at %d",
			row.SubmitTimeNs, entered.UnixNano())
	}
	if row.SubmitAckTimeNs == nil || *row.SubmitAckTimeNs < left.UnixNano() {
		t.Errorf("submit_ack_time_ns %v is before the executor returned at %d",
			row.SubmitAckTimeNs, left.UnixNano())
	}

	// The round-trip must be visible as its own quantity, not folded away.
	if row.SubmitRttNs == nil {
		t.Fatal("submit_rtt_ns missing")
	}
	if *row.SubmitRttNs < int64(submissionDelay) {
		t.Errorf("submit_rtt_ns = %d, want at least the %s the executor took",
			*row.SubmitRttNs, submissionDelay)
	}
}

// Every attempt must be recorded, including the ones that failed. Previously
// the goroutine returned before storing anything, so failed submissions
// vanished and no denominator recorded that they had been attempted.
func TestFailedSubmissionsAreRecorded(t *testing.T) {
	exec := &fakeExecutor{failFor: func(seq int) bool { return seq%2 == 0 }}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark, "count=10&concurrency=1")

	if response.RequestedJobs != 10 {
		t.Errorf("RequestedJobs = %d, want 10", response.RequestedJobs)
	}
	if response.SubmittedJobs+response.FailedJobs != 10 {
		t.Errorf("submitted+failed = %d, want 10", response.SubmittedJobs+response.FailedJobs)
	}
	if response.FailedJobs != 5 {
		t.Errorf("FailedJobs = %d, want 5", response.FailedJobs)
	}

	rows := exportRows(t, store, response.RunID)
	if len(rows) != 10 {
		t.Fatalf("exported %d rows, want all 10 attempts", len(rows))
	}

	var failed int
	for _, row := range rows {
		if row.SubmitStatus == persister.SubmitStatusFailed {
			failed++
			if row.SubmitError == nil || *row.SubmitError == "" {
				t.Errorf("seq %d failed with no recorded error", row.Seq)
			}
			if row.JobID != nil {
				t.Errorf("seq %d failed but carries a job id", row.Seq)
			}
		}
	}
	if failed != 5 {
		t.Errorf("exported %d failed rows, want 5", failed)
	}

	runs, err := store.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].RequestedJobs != 10 || runs[0].FailedJobs != 5 {
		t.Errorf("run tallies = %d requested / %d failed, want 10/5", runs[0].RequestedJobs, runs[0].FailedJobs)
	}
}

// Concurrency is shaped by the controller, identically for every variant,
// rather than by whatever connection limit an executor happened to configure.
func TestConcurrencyCapIsRespected(t *testing.T) {
	const cap = 4

	exec := &fakeExecutor{delay: 20 * time.Millisecond}
	benchmark, _ := newTestBenchmark(t, exec)

	runBenchmark(t, benchmark, fmt.Sprintf("count=40&concurrency=%d", cap))

	if peak := exec.maxFlight.Load(); peak > cap {
		t.Errorf("peak in-flight submissions = %d, want at most %d", peak, cap)
	}
	if peak := exec.maxFlight.Load(); peak < 2 {
		t.Errorf("peak in-flight submissions = %d, expected the run to actually overlap", peak)
	}
}

// The pacer is open-loop: release times come from a fixed schedule, so a slow
// system under test cannot slow the offered load down and hide its own
// queueing.
func TestRatePacingSpreadsSubmissions(t *testing.T) {
	const (
		count = 20
		rate  = 100.0 // per second, so ~190ms for 20 submissions
	)

	exec := &fakeExecutor{}
	benchmark, _ := newTestBenchmark(t, exec)

	start := time.Now()
	runBenchmark(t, benchmark, fmt.Sprintf("count=%d&rate=%.0f&concurrency=64", count, rate))
	elapsed := time.Since(start)

	expected := time.Duration(float64(count-1)/rate*float64(time.Second)) - 20*time.Millisecond
	if elapsed < expected {
		t.Errorf("run took %s, want at least %s at %v/s", elapsed, expected, rate)
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.entered) != count {
		t.Fatalf("executor entered %d times, want %d", len(exec.entered), count)
	}
}

func TestRunRecordsProvenance(t *testing.T) {
	exec := &fakeExecutor{}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark,
		"count=2&run_id=paper-run-7&commit_hash=abc123&workload_id=java-build&notes=cold+cache")

	if response.RunID != "paper-run-7" {
		t.Errorf("RunID = %q, want the supplied one", response.RunID)
	}
	if response.ConfigFingerprint == "" {
		t.Error("ConfigFingerprint is empty")
	}

	runs, err := store.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	run := runs[0]

	if run.Variant != "fake" || run.TargetHost != "sut-fake.example" {
		t.Errorf("variant/host = %q/%q", run.Variant, run.TargetHost)
	}
	if run.WorkloadID != "java-build" {
		t.Errorf("WorkloadID = %q", run.WorkloadID)
	}
	if run.CommitHash == nil || *run.CommitHash != "abc123" {
		t.Errorf("CommitHash = %v", run.CommitHash)
	}
	if run.Notes == nil || *run.Notes != "cold cache" {
		t.Errorf("Notes = %v", run.Notes)
	}

	for _, row := range exportRows(t, store, response.RunID) {
		if row.Variant != "fake" || row.TargetHost != "sut-fake.example" {
			t.Errorf("row %d lost provenance: %+v", row.Seq, row)
		}
		if row.ConfigFingerprint != response.ConfigFingerprint {
			t.Errorf("row %d fingerprint = %q, want %q", row.Seq, row.ConfigFingerprint, response.ConfigFingerprint)
		}
	}
}

// The fingerprint must describe the configuration, not one instance of it, so
// two identically configured runs are recognisably comparable.
func TestConfigFingerprintIsStableAcrossRuns(t *testing.T) {
	exec := &fakeExecutor{}
	benchmark, _ := newTestBenchmark(t, exec)

	first := runBenchmark(t, benchmark, "count=3&concurrency=8")
	second := runBenchmark(t, benchmark, "count=3&concurrency=8")
	different := runBenchmark(t, benchmark, "count=3&concurrency=16")

	if first.ConfigFingerprint != second.ConfigFingerprint {
		t.Error("identically configured runs produced different fingerprints")
	}
	if first.ConfigFingerprint == different.ConfigFingerprint {
		t.Error("a different concurrency must change the fingerprint")
	}
}

func TestInvalidQueryParametersAreRejected(t *testing.T) {
	exec := &fakeExecutor{}
	benchmark, _ := newTestBenchmark(t, exec)

	router := gin.New()
	router.POST("/v1/benchmark/fake", benchmark.HandleFunc)

	for _, query := range []string{"count=0", "count=abc", "concurrency=-1", "rate=-5", "rate=fast"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/benchmark/fake?"+query,
			strings.NewReader(`{"priority":3,"name":"w","steps":[]}`))
		req.Header.Set("Content-Type", "application/json")

		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", query, recorder.Code)
		}
	}

	if exec.calls.Load() != 0 {
		t.Errorf("executor was called %d times for invalid requests", exec.calls.Load())
	}
}

func exportRows(t *testing.T, store *persister.DBPersister, runID string) []persister.JobRow {
	t.Helper()

	var rows []persister.JobRow
	if err := store.ExportJobs(context.Background(), runID, func(row persister.JobRow) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("ExportJobs: %v", err)
	}
	return rows
}

// The Hades status callback does not carry priority - Hades never persists it
// in its KV bucket, so it cannot be recovered from the callback. The burst
// experiment needs priority to measure inversion, so it must be captured
// client-side at submit time and stored on the submission row.
func TestPriorityIsRecordedAtSubmitTime(t *testing.T) {
	exec := &fakeExecutor{}
	benchmark, store := newTestBenchmark(t, exec)

	router := gin.New()
	router.POST("/v1/benchmark/fake", benchmark.HandleFunc)

	req := httptest.NewRequest(http.MethodPost, "/v1/benchmark/fake?count=3&run_id=priority-run",
		strings.NewReader(`{"priority":1,"name":"low-priority-workload","steps":[]}`))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	rows := exportRows(t, store, "priority-run")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for _, row := range rows {
		if row.Priority != 1 {
			t.Errorf("seq %d priority = %d, want the submitted priority 1", row.Seq, row.Priority)
		}
	}

	runs, err := store.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].Priority != 1 {
		t.Errorf("run priority = %d, want 1", runs[0].Priority)
	}
}

// The recorded schedule must be the schedule that was asked for: exactly
// 1/rate apart, independent of how long any individual submission took. Without
// this, a run cannot show that the load it claims to have offered is the load it
// actually offered.
func TestScheduledReleaseIsRecordedAtTheRequestedRate(t *testing.T) {
	const (
		count = 12
		rate  = 200.0 // per second -> 5ms apart
	)

	exec := &fakeExecutor{delay: 3 * time.Millisecond}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark,
		fmt.Sprintf("count=%d&rate=%.0f&concurrency=64", count, rate))

	rows := exportRows(t, store, response.RunID)
	if len(rows) != count {
		t.Fatalf("exported %d rows, want %d", len(rows), count)
	}

	step := int64(float64(time.Second) / rate)
	var origin int64
	for _, row := range rows {
		if row.ScheduledReleaseNs == nil {
			t.Fatalf("seq %d has no scheduled_release_ns on a paced run", row.Seq)
		}
		if row.Seq == 0 {
			origin = *row.ScheduledReleaseNs
			continue
		}

		want := origin + int64(row.Seq)*step
		// The offset is computed in float nanoseconds, so allow a rounding
		// nanosecond either way - but nothing resembling a real delay.
		if diff := *row.ScheduledReleaseNs - want; diff > 2 || diff < -2 {
			t.Errorf("seq %d scheduled at %+d ns relative to the fixed schedule, want it on the schedule",
				row.Seq, diff)
		}
	}
}

// Schedule slip is the whole point of recording the schedule: when the
// concurrency cap binds, the run no longer offers the rate it was asked for, and
// that has to be visible in the exported data rather than silently folded into
// the latency of the system under test.
func TestScheduleSlipIsVisibleWhenTheConcurrencyCapBinds(t *testing.T) {
	const (
		count = 10
		rate  = 500.0 // per second -> 2ms apart, so ~18ms of schedule
		delay = 40 * time.Millisecond
	)

	// One slot and a slow system under test: the cap cannot keep up with the
	// requested rate, so the run must report that it fell behind.
	exec := &fakeExecutor{delay: delay}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark,
		fmt.Sprintf("count=%d&rate=%.0f&concurrency=1", count, rate))

	rows := exportRows(t, store, response.RunID)
	if len(rows) != count {
		t.Fatalf("exported %d rows, want %d", len(rows), count)
	}

	var last persister.JobRow
	for _, row := range rows {
		if row.ScheduleSlipNs == nil {
			t.Fatalf("seq %d has no schedule_slip_ns", row.Seq)
		}
		if *row.ScheduleSlipNs < 0 {
			t.Errorf("seq %d slipped %d ns, i.e. it went out before it was due",
				row.Seq, *row.ScheduleSlipNs)
		}
		if row.Seq > last.Seq {
			last = row
		}
	}

	// The last submission waits behind count-1 serialised delays, minus the
	// schedule it was already entitled to consume. Assert it is unmistakably
	// large rather than pinning an exact figure.
	minSlip := int64(count-2) * int64(delay)
	if *last.ScheduleSlipNs < minSlip {
		t.Errorf("last submission slipped only %v, want at least %v - the cap was throttling the offered rate and the data must show it",
			time.Duration(*last.ScheduleSlipNs), time.Duration(minSlip))
	}
}

// An unpaced run has no schedule, so there is nothing to slip against. Recording
// a zero would be a fabricated measurement.
func TestUnpacedRunsRecordNoSchedule(t *testing.T) {
	exec := &fakeExecutor{}
	benchmark, store := newTestBenchmark(t, exec)

	response := runBenchmark(t, benchmark, "count=4&concurrency=4")

	for _, row := range exportRows(t, store, response.RunID) {
		if row.ScheduledReleaseNs != nil {
			t.Errorf("seq %d recorded a scheduled release of %d on an unpaced run",
				row.Seq, *row.ScheduledReleaseNs)
		}
		if row.ScheduleSlipNs != nil {
			t.Errorf("seq %d recorded a slip of %d on an unpaced run",
				row.Seq, *row.ScheduleSlipNs)
		}
	}
}

// A client that disconnects mid-run must still leave a usable run row. Before
// this was handled, `run` returned before wg.Wait() and before FinishRun, so
// benchmark_run kept finished_at NULL and 0/0 tallies while job_submission went
// on gaining rows from goroutines that were still going - the summary
// contradicted the exported rows, and a shutdown could race those writes.
func TestCancelledRunStillRecordsItsTallies(t *testing.T) {
	const (
		count = 40
		rate  = 50.0 // per second, so the run cannot finish before the cancel
	)

	exec := &fakeExecutor{delay: 5 * time.Millisecond}
	benchmark, store := newTestBenchmark(t, exec)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	runID := "cancelled-run"
	_, err := benchmark.run(ctx, payload.RESTPayload{Priority: 3}, runOptions{
		runID:         runID,
		count:         count,
		concurrency:   4,
		ratePerSecond: rate,
		workloadID:    "w",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}

	runs, err := store.ListRuns(context.Background())
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	var run *persister.Run
	for i := range runs {
		if runs[i].RunID == runID {
			run = &runs[i]
		}
	}
	if run == nil {
		t.Fatal("cancelled run was never recorded")
	}

	if run.FinishedAtNs == nil {
		t.Error("finished_at_ns is NULL on a cancelled run; the row is unusable")
	}

	// The tallies must describe the rows that actually exist.
	rows := exportRows(t, store, runID)
	if got := run.SubmittedJobs + run.FailedJobs; got != len(rows) {
		t.Errorf("run tallies say %d submissions, but %d rows were exported", got, len(rows))
	}
	if len(rows) == 0 {
		t.Error("expected the cancelled run to have submitted something before the cancel")
	}
	if len(rows) >= count {
		t.Errorf("exported %d rows for a run cancelled partway through %d", len(rows), count)
	}
}
