package persister

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *DBPersister {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedRun(t *testing.T, store *DBPersister, runID string) Run {
	t.Helper()

	run := Run{
		RunID:              runID,
		Variant:            "hades-docker",
		TargetHost:         "sut-docker.example",
		WorkloadID:         "artemis-java-build",
		ConfigFingerprint:  "fingerprint",
		Priority:           3,
		RequestedJobs:      3,
		Concurrency:        64,
		RatePerSecond:      0,
		StartedAtNs:        time.Now().UnixNano(),
		BenchmarkerVersion: "test",
	}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	return run
}

// Nanosecond timestamps must survive a round-trip exactly. Under the old
// schema every value went through strftime('%s'), which floors each operand
// independently before subtracting, so a true 1.8s gap and a true 0.2s gap both
// came back as 1.
func TestNanosecondPrecisionSurvivesRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedRun(t, store, "run-precision")

	submitNs := int64(1787220000_100000000) // ...T:00.100000000
	ackNs := submitNs + 3_141592
	jobID := "job-precision"

	if err := store.RecordSubmission(ctx, Submission{
		SubmissionID: "s-0", RunID: "run-precision", Seq: 0, JobID: &jobID,
		Variant: "hades-docker", TargetHost: "sut-docker.example", WorkloadID: "w",
		ConfigFingerprint: "fp", Priority: 3,
		SubmitTimeNs: submitNs, SubmitAckTimeNs: &ackNs, SubmitStatus: SubmitStatusAccepted,
	}); err != nil {
		t.Fatalf("RecordSubmission: %v", err)
	}

	// A completion 1.8 seconds later, straddling a whole-second boundary in
	// exactly the way the old integer-second arithmetic got wrong.
	receivedNs := submitNs + 1_800_000_000
	if _, err := store.RecordCallback(ctx, Callback{
		JobID: jobID, ReceivedTimeNs: receivedNs, Source: "hades",
		Status: StatusSucceeded, RawStatus: "Succeeded", Terminal: true,
	}); err != nil {
		t.Fatalf("RecordCallback: %v", err)
	}

	rows := exportRows(t, store, "run-precision")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	row := rows[0]
	if row.SubmitTimeNs != submitNs {
		t.Errorf("SubmitTimeNs = %d, want %d", row.SubmitTimeNs, submitNs)
	}
	if row.SubmitRttNs == nil || *row.SubmitRttNs != 3_141592 {
		t.Errorf("SubmitRttNs = %v, want 3141592", row.SubmitRttNs)
	}
	if row.EndToEndNs == nil || *row.EndToEndNs != 1_800_000_000 {
		t.Errorf("EndToEndNs = %v, want exactly 1800000000 ns", row.EndToEndNs)
	}
}

// A retried callback must not double-count and must not move the measurement.
// Jenkins alone guarantees this happens: it fires the notification for both the
// COMPLETED and the FINALIZED phase of every build.
func TestRecordCallbackIsIdempotentByJobID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := int64(1_000_000_000)
	result, err := store.RecordCallback(ctx, Callback{
		JobID: "job-1", ReceivedTimeNs: first, Source: "jenkins",
		Status: StatusSucceeded, RawStatus: "SUCCESS", Phase: "COMPLETED", Terminal: true,
	})
	if err != nil {
		t.Fatalf("first RecordCallback: %v", err)
	}
	if !result.Accepted || result.Duplicate || result.DeliveryCount != 1 {
		t.Fatalf("first callback: %+v, want accepted with delivery_count 1", result)
	}

	// The same job, later, with a different arrival time.
	second := first + 5_000_000_000
	result, err = store.RecordCallback(ctx, Callback{
		JobID: "job-1", ReceivedTimeNs: second, Source: "jenkins",
		Status: StatusSucceeded, RawStatus: "SUCCESS", Phase: "FINALIZED", Terminal: true,
	})
	if err != nil {
		t.Fatalf("second RecordCallback: %v", err)
	}
	if result.Accepted {
		t.Error("a repeat terminal callback must not be accepted as a new measurement")
	}
	if !result.Duplicate || result.DeliveryCount != 2 {
		t.Errorf("second callback: %+v, want duplicate with delivery_count 2", result)
	}

	var received, lastReceived int64
	if err := store.DB().QueryRow(
		`SELECT received_time_ns, last_received_time_ns FROM job_callback WHERE job_id = 'job-1'`,
	).Scan(&received, &lastReceived); err != nil {
		t.Fatalf("read job_callback: %v", err)
	}
	if received != first {
		t.Errorf("received_time_ns = %d, want the FIRST arrival %d", received, first)
	}
	if lastReceived != second {
		t.Errorf("last_received_time_ns = %d, want %d", lastReceived, second)
	}

	// Both POSTs must still be in the audit log.
	events, err := store.CountEvents(ctx)
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if events != 2 {
		t.Errorf("job_event count = %d, want 2", events)
	}
}

func TestNonTerminalCallbackIsLoggedButNotMeasured(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result, err := store.RecordCallback(ctx, Callback{
		JobID: "job-1", ReceivedTimeNs: 1, Source: "hades",
		RawStatus: "Running", Phase: "Running", Terminal: false,
	})
	if err != nil {
		t.Fatalf("RecordCallback: %v", err)
	}
	if result.Accepted || result.Terminal {
		t.Errorf("result = %+v, want neither accepted nor terminal", result)
	}

	callbacks, err := store.CountCallbacks(ctx)
	if err != nil {
		t.Fatalf("CountCallbacks: %v", err)
	}
	if callbacks != 0 {
		t.Errorf("job_callback count = %d, want 0", callbacks)
	}

	events, err := store.CountEvents(ctx)
	if err != nil {
		t.Fatalf("CountEvents: %v", err)
	}
	if events != 1 {
		t.Errorf("job_event count = %d, want 1", events)
	}
}

// A submitted job that never calls back must still appear in the export.
// Dropping it, as `WHERE r.start_time IS NOT NULL` did, biases the sample
// toward fast and successful jobs.
func TestExportIncludesJobsWithoutCallbacks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedRun(t, store, "run-1")

	completedID := "job-completed"
	pendingID := "job-pending"
	failureMessage := "connection refused"

	submissions := []Submission{
		{SubmissionID: "s-0", RunID: "run-1", Seq: 0, JobID: &completedID, SubmitTimeNs: 100, SubmitStatus: SubmitStatusAccepted},
		{SubmissionID: "s-1", RunID: "run-1", Seq: 1, JobID: &pendingID, SubmitTimeNs: 200, SubmitStatus: SubmitStatusAccepted},
		{SubmissionID: "s-2", RunID: "run-1", Seq: 2, SubmitTimeNs: 300, SubmitStatus: SubmitStatusFailed, SubmitError: &failureMessage},
	}
	for _, s := range submissions {
		s.Variant, s.TargetHost, s.WorkloadID, s.ConfigFingerprint = "hades-docker", "sut", "w", "fp"
		if err := store.RecordSubmission(ctx, s); err != nil {
			t.Fatalf("RecordSubmission %s: %v", s.SubmissionID, err)
		}
	}

	if _, err := store.RecordCallback(ctx, Callback{
		JobID: completedID, ReceivedTimeNs: 5_000, Source: "hades",
		Status: StatusSucceeded, RawStatus: "Succeeded", Terminal: true,
	}); err != nil {
		t.Fatalf("RecordCallback: %v", err)
	}

	rows := exportRows(t, store, "run-1")
	if len(rows) != 3 {
		t.Fatalf("exported %d rows, want all 3 submission attempts", len(rows))
	}

	if !rows[0].Completed || rows[0].EndToEndNs == nil || *rows[0].EndToEndNs != 4_900 {
		t.Errorf("row 0 = %+v, want completed with end_to_end 4900", rows[0])
	}
	if rows[1].Completed {
		t.Error("row 1 must be exported as not completed, not omitted")
	}
	if rows[2].SubmitStatus != SubmitStatusFailed {
		t.Errorf("row 2 submit_status = %q, want %q", rows[2].SubmitStatus, SubmitStatusFailed)
	}
	if rows[2].SubmitError == nil || *rows[2].SubmitError != failureMessage {
		t.Errorf("row 2 submit_error = %v, want the recorded failure", rows[2].SubmitError)
	}
}

// A callback for a job the benchmarker has no submission for must still be
// stored, and must be findable.
func TestUnmatchedCallbacksAreRetained(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.RecordCallback(ctx, Callback{
		JobID: "ghost-job", ReceivedTimeNs: 42, Source: "hades",
		Status: StatusSucceeded, RawStatus: "Succeeded", Terminal: true,
	}); err != nil {
		t.Fatalf("RecordCallback: %v", err)
	}

	var orphans []CallbackRow
	if err := store.ExportCallbacks(ctx, "", true, func(row CallbackRow) error {
		orphans = append(orphans, row)
		return nil
	}); err != nil {
		t.Fatalf("ExportCallbacks: %v", err)
	}

	if len(orphans) != 1 {
		t.Fatalf("got %d unmatched callbacks, want 1", len(orphans))
	}
	if orphans[0].Matched {
		t.Error("orphan must report Matched=false")
	}
}

func TestFinishRunRecordsTallies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedRun(t, store, "run-tally")

	finishedAt := time.Now().UnixNano()
	if err := store.FinishRun(ctx, "run-tally", finishedAt, 397, 3); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	runs, err := store.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0].SubmittedJobs != 397 || runs[0].FailedJobs != 3 {
		t.Errorf("tallies = %d submitted / %d failed, want 397/3", runs[0].SubmittedJobs, runs[0].FailedJobs)
	}
	if runs[0].FinishedAtNs == nil || *runs[0].FinishedAtNs != finishedAt {
		t.Errorf("FinishedAtNs = %v, want %d", runs[0].FinishedAtNs, finishedAt)
	}
}

// The writer must survive many goroutines writing at once. SQLite allows one
// writer, so this used to be enforced with SetMaxOpenConns(1) and every loser
// surfaced as SQLITE_BUSY.
func TestConcurrentWritesDoNotDropRows(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedRun(t, store, "run-concurrent")

	const total = 500

	var wg sync.WaitGroup
	errs := make(chan error, total)

	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			jobID := fmt.Sprintf("job-%d", seq)
			errs <- store.RecordSubmission(ctx, Submission{
				SubmissionID: fmt.Sprintf("s-%d", seq), RunID: "run-concurrent", Seq: seq,
				JobID: &jobID, Variant: "hades-docker", TargetHost: "sut", WorkloadID: "w",
				ConfigFingerprint: "fp", Priority: 3,
				SubmitTimeNs: int64(seq), SubmitStatus: SubmitStatusAccepted,
			})
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent RecordSubmission: %v", err)
		}
	}

	rows := exportRows(t, store, "run-concurrent")
	if len(rows) != total {
		t.Errorf("stored %d rows, want %d", len(rows), total)
	}
}

func exportRows(t *testing.T, store *DBPersister, runID string) []JobRow {
	t.Helper()

	var rows []JobRow
	if err := store.ExportJobs(context.Background(), runID, func(row JobRow) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("ExportJobs: %v", err)
	}
	return rows
}
