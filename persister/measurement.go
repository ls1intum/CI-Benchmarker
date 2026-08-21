package persister

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SubmitStatusAccepted marks a submission the system under test acknowledged.
const SubmitStatusAccepted = "accepted"

// SubmitStatusFailed marks a submission that never reached the system under
// test. These rows are recorded deliberately: without them a run of N jobs that
// only managed M submissions is indistinguishable from a run of M.
const SubmitStatusFailed = "failed"

// Terminal job statuses, normalised across systems under test.
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusStopped   = "stopped"
)

// Run describes one benchmark invocation.
type Run struct {
	RunID              string  `json:"run_id"`
	Variant            string  `json:"variant"`
	TargetHost         string  `json:"target_host"`
	WorkloadID         string  `json:"workload_id"`
	ConfigFingerprint  string  `json:"config_fingerprint"`
	CommitHash         *string `json:"commit_hash,omitempty"`
	Priority           int     `json:"priority"`
	RequestedJobs      int     `json:"requested_jobs"`
	Concurrency        int     `json:"concurrency"`
	RatePerSecond      float64 `json:"rate_per_second"`
	StartedAtNs        int64   `json:"started_at_ns"`
	FinishedAtNs       *int64  `json:"finished_at_ns,omitempty"`
	SubmittedJobs      int     `json:"submitted_jobs"`
	FailedJobs         int     `json:"failed_jobs"`
	BenchmarkerVersion string  `json:"benchmarker_version"`
	Notes              *string `json:"notes,omitempty"`
}

// Submission is one attempt to hand a job to the system under test.
type Submission struct {
	SubmissionID      string
	RunID             string
	Seq               int
	JobID             *string
	Variant           string
	TargetHost        string
	WorkloadID        string
	ConfigFingerprint string
	Priority          int
	CommitHash        *string
	// ScheduledReleaseNs is when the open-loop pacer intended this submission
	// to go out. SubmitTimeNs - ScheduledReleaseNs is the schedule slip, which
	// is the only way to tell after the fact whether the offered load was the
	// load that was requested. NULL for unpaced runs, which have no schedule.
	ScheduledReleaseNs *int64
	// SubmitTimeNs is taken immediately BEFORE the executor call, so queue
	// latency includes the submission round-trip instead of hiding it.
	SubmitTimeNs int64
	// SubmitAckTimeNs is taken immediately after the executor call returns.
	// SubmitAckTimeNs - SubmitTimeNs is the submission round-trip, reported
	// separately so it can be inspected rather than silently folded into or
	// out of queue latency.
	SubmitAckTimeNs *int64
	SubmitStatus    string
	SubmitError     *string
}

// Callback is a status notification pushed back by a system under test.
type Callback struct {
	JobID string
	// ReceivedTimeNs is stamped from the benchmarker's own clock the moment the
	// request reaches the handler. It is the only completion timestamp used for
	// measurement; anything the system under test reports about its own clock
	// is kept as provenance only.
	ReceivedTimeNs int64
	Source         string
	Status         string
	RawStatus      string
	Reason         string
	Phase          string
	Event          string
	Terminal       bool
	// Reported* come from the system under test and are provenance only.
	ReportedQueuedNs   *int64
	ReportedStartNs    *int64
	ReportedEndNs      *int64
	ReportedDurationMs *int64
	// DeliveryAttempt is the sender's own redelivery counter. DeliveryID is the
	// sender's unique id for this delivery attempt (X-Hades-Delivery).
	DeliveryAttempt *int64
	DeliveryID      string
	RemoteAddr      string
	RawPayload      string
}

// CallbackResult reports what the receiver did with a callback.
type CallbackResult struct {
	// Accepted is true only for the callback that established the
	// authoritative terminal observation for this job.
	Accepted bool `json:"accepted"`
	// Duplicate is true when a terminal callback for this job had already been
	// recorded. The stored measurement is left untouched.
	Duplicate bool `json:"duplicate"`
	// DeliveryCount counts every terminal callback seen for this job.
	DeliveryCount int64 `json:"delivery_count"`
	// Terminal is false for lifecycle notifications that do not end the job
	// (Jenkins STARTED, Hades Running, ...). Those are logged, not measured.
	Terminal bool `json:"terminal"`
}

// JobRow is one exported per-job record: the submission joined to its terminal
// callback. Every duration is an exact int64 nanosecond subtraction.
type JobRow struct {
	RunID             string  `json:"run_id"`
	Seq               int     `json:"seq"`
	SubmissionID      string  `json:"submission_id"`
	JobID             *string `json:"job_id"`
	Variant           string  `json:"variant"`
	TargetHost        string  `json:"target_host"`
	WorkloadID        string  `json:"workload_id"`
	ConfigFingerprint string  `json:"config_fingerprint"`
	Priority          int     `json:"priority"`
	CommitHash        *string `json:"commit_hash"`

	// ScheduledReleaseNs is when the open-loop pacer intended this submission
	// to go out; NULL on unpaced runs. See ScheduleSlipNs.
	ScheduledReleaseNs *int64  `json:"scheduled_release_ns"`
	SubmitTimeNs       int64   `json:"submit_time_ns"`
	SubmitAckTimeNs    *int64  `json:"submit_ack_time_ns"`
	SubmitStatus       string  `json:"submit_status"`
	SubmitError        *string `json:"submit_error"`

	CallbackReceivedTimeNs  *int64  `json:"callback_received_time_ns"`
	CallbackStatus          *string `json:"callback_status"`
	CallbackRawStatus       *string `json:"callback_raw_status"`
	CallbackReason          *string `json:"callback_reason"`
	CallbackSource          *string `json:"callback_source"`
	CallbackEvent           *string `json:"callback_event"`
	CallbackDeliveryCount   *int64  `json:"callback_delivery_count"`
	CallbackDeliveryAttempt *int64  `json:"callback_delivery_attempt"`
	ReportedQueuedTimeNs    *int64  `json:"reported_queued_time_ns"`
	ReportedStartTimeNs     *int64  `json:"reported_start_time_ns"`
	ReportedEndTimeNs       *int64  `json:"reported_end_time_ns"`
	ReportedDurationMs      *int64  `json:"reported_duration_ms"`

	// SubmitRttNs is submit_ack_time_ns - submit_time_ns.
	SubmitRttNs *int64 `json:"submit_rtt_ns"`
	// ScheduleSlipNs is submit_time_ns - scheduled_release_ns: how far behind
	// its own schedule the load generator was when this job went out. It is
	// exported so that the offered load can be verified rather than assumed.
	// Consistently large slip means the concurrency cap or the generator itself
	// was the bottleneck, and the run understates the load it claims to apply.
	ScheduleSlipNs *int64 `json:"schedule_slip_ns"`
	// EndToEndNs is callback_received_time_ns - submit_time_ns: the complete
	// observed latency from the instant before submission to the instant the
	// terminal status arrived. Both endpoints are on the benchmarker's clock,
	// so it needs no clock synchronisation between hosts.
	EndToEndNs *int64 `json:"end_to_end_ns"`
	// Completed is false for jobs that were submitted but never called back.
	// Such rows are exported anyway; dropping them is what produced
	// survivorship bias in the previous design.
	Completed bool `json:"completed"`
}

// CallbackRow is one exported callback, including ones that never matched a
// submission.
type CallbackRow struct {
	JobID                string  `json:"job_id"`
	RunID                *string `json:"run_id"`
	ReceivedTimeNs       int64   `json:"received_time_ns"`
	LastReceivedTimeNs   int64   `json:"last_received_time_ns"`
	Source               string  `json:"source"`
	Status               string  `json:"status"`
	RawStatus            *string `json:"raw_status"`
	Event                *string `json:"event"`
	Reason               *string `json:"reason"`
	ReportedQueuedTimeNs *int64  `json:"reported_queued_time_ns"`
	ReportedStartTimeNs  *int64  `json:"reported_start_time_ns"`
	ReportedEndTimeNs    *int64  `json:"reported_end_time_ns"`
	ReportedDurationMs   *int64  `json:"reported_duration_ms"`
	DeliveryCount        int64   `json:"delivery_count"`
	DeliveryAttempt      *int64  `json:"delivery_attempt"`
	Matched              bool    `json:"matched"`
}

// CreateRun records a benchmark run before any job is submitted.
func (d *DBPersister) CreateRun(ctx context.Context, r Run) error {
	return d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO benchmark_run (
    run_id, variant, target_host, workload_id, config_fingerprint, commit_hash,
    priority, requested_jobs, concurrency, rate_per_second, started_at_ns,
    benchmarker_version, notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.RunID, r.Variant, r.TargetHost, r.WorkloadID, r.ConfigFingerprint,
			nullString(r.CommitHash), r.Priority, r.RequestedJobs, r.Concurrency,
			r.RatePerSecond, r.StartedAtNs, r.BenchmarkerVersion, nullString(r.Notes))
		return err
	})
}

// FinishRun closes out a run with its submission tallies.
func (d *DBPersister) FinishRun(ctx context.Context, runID string, finishedAtNs int64, submitted, failed int) error {
	return d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
UPDATE benchmark_run
   SET finished_at_ns = ?, submitted_jobs = ?, failed_jobs = ?
 WHERE run_id = ?`, finishedAtNs, submitted, failed, runID)
		return err
	})
}

// RecordSubmission stores one submission attempt, successful or not.
func (d *DBPersister) RecordSubmission(ctx context.Context, s Submission) error {
	return d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO job_submission (
    submission_id, run_id, seq, job_id, variant, target_host, workload_id,
    config_fingerprint, priority, commit_hash, scheduled_release_ns,
    submit_time_ns, submit_ack_time_ns, submit_status, submit_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.SubmissionID, s.RunID, s.Seq, nullString(s.JobID), s.Variant, s.TargetHost,
			s.WorkloadID, s.ConfigFingerprint, s.Priority, nullString(s.CommitHash),
			nullInt64(s.ScheduledReleaseNs), s.SubmitTimeNs, nullInt64(s.SubmitAckTimeNs),
			s.SubmitStatus, nullString(s.SubmitError))
		return err
	})
}

// RecordCallback stores a status callback.
//
// Terminal callbacks are idempotent by job id: the first one establishes
// received_time_ns and every later one only bumps delivery_count, so a retrying
// system under test cannot double-count or move a measurement. Every callback,
// terminal or not, is appended to job_event for audit.
func (d *DBPersister) RecordCallback(ctx context.Context, cb Callback) (CallbackResult, error) {
	result := CallbackResult{Terminal: cb.Terminal}

	err := d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		// Reset on retry so a retried batch cannot leave stale values behind.
		result.Accepted = false
		result.Duplicate = false
		result.DeliveryCount = 0

		if cb.Terminal {
			var deliveryCount int64
			err := tx.QueryRowContext(ctx, `
INSERT INTO job_callback (
    job_id, received_time_ns, last_received_time_ns, source, status, raw_status,
    event, reason, reported_queued_time_ns, reported_start_time_ns,
    reported_end_time_ns, reported_duration_ms, delivery_attempt,
    delivery_count, raw_payload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
ON CONFLICT (job_id) DO UPDATE SET
    delivery_count        = job_callback.delivery_count + 1,
    last_received_time_ns = excluded.last_received_time_ns
RETURNING delivery_count`,
				cb.JobID, cb.ReceivedTimeNs, cb.ReceivedTimeNs, cb.Source, cb.Status,
				nullNonEmpty(cb.RawStatus), nullNonEmpty(cb.Event), nullNonEmpty(cb.Reason),
				nullInt64(cb.ReportedQueuedNs), nullInt64(cb.ReportedStartNs),
				nullInt64(cb.ReportedEndNs), nullInt64(cb.ReportedDurationMs),
				nullInt64(cb.DeliveryAttempt), nullNonEmpty(cb.RawPayload),
			).Scan(&deliveryCount)
			if err != nil {
				return err
			}

			result.DeliveryCount = deliveryCount
			result.Accepted = deliveryCount == 1
			result.Duplicate = deliveryCount > 1
		}

		_, err := tx.ExecContext(ctx, `
INSERT INTO job_event (
    job_id, received_time_ns, source, phase, status, terminal, accepted,
    delivery_id, delivery_attempt, remote_addr, raw_payload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nullNonEmpty(cb.JobID), cb.ReceivedTimeNs, cb.Source, nullNonEmpty(cb.Phase),
			nullNonEmpty(cb.Status), boolToInt(cb.Terminal), boolToInt(result.Accepted),
			nullNonEmpty(cb.DeliveryID), nullInt64(cb.DeliveryAttempt),
			nullNonEmpty(cb.RemoteAddr), nullNonEmpty(cb.RawPayload))
		return err
	})

	return result, err
}

const jobRowSelect = `
SELECT s.run_id, s.seq, s.submission_id, s.job_id, s.variant, s.target_host,
       s.workload_id, s.config_fingerprint, s.priority, s.commit_hash,
       s.scheduled_release_ns, s.submit_time_ns, s.submit_ack_time_ns,
       s.submit_status, s.submit_error,
       c.received_time_ns, c.status, c.raw_status, c.reason, c.source, c.event,
       c.delivery_count, c.delivery_attempt, c.reported_queued_time_ns,
       c.reported_start_time_ns, c.reported_end_time_ns, c.reported_duration_ms
  FROM job_submission s
  LEFT JOIN job_callback c ON c.job_id = s.job_id`

// ExportJobs streams every job of a run, in submission order, to fn.
//
// Rows are streamed rather than collected so a long run does not have to fit in
// memory. Jobs with no callback are included with Completed=false.
func (d *DBPersister) ExportJobs(ctx context.Context, runID string, fn func(JobRow) error) error {
	query := jobRowSelect + " WHERE (:run_id = '' OR s.run_id = :run_id) ORDER BY s.run_id, s.seq"

	rows, err := d.db.QueryContext(ctx, query, sql.Named("run_id", runID))
	if err != nil {
		return fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r JobRow
		if err := rows.Scan(
			&r.RunID, &r.Seq, &r.SubmissionID, &r.JobID, &r.Variant, &r.TargetHost,
			&r.WorkloadID, &r.ConfigFingerprint, &r.Priority, &r.CommitHash,
			&r.ScheduledReleaseNs, &r.SubmitTimeNs, &r.SubmitAckTimeNs,
			&r.SubmitStatus, &r.SubmitError,
			&r.CallbackReceivedTimeNs, &r.CallbackStatus, &r.CallbackRawStatus,
			&r.CallbackReason, &r.CallbackSource, &r.CallbackEvent,
			&r.CallbackDeliveryCount, &r.CallbackDeliveryAttempt,
			&r.ReportedQueuedTimeNs, &r.ReportedStartTimeNs,
			&r.ReportedEndTimeNs, &r.ReportedDurationMs,
		); err != nil {
			return fmt.Errorf("scan job: %w", err)
		}

		if r.SubmitAckTimeNs != nil {
			rtt := *r.SubmitAckTimeNs - r.SubmitTimeNs
			r.SubmitRttNs = &rtt
		}
		if r.ScheduledReleaseNs != nil {
			slip := r.SubmitTimeNs - *r.ScheduledReleaseNs
			r.ScheduleSlipNs = &slip
		}
		if r.CallbackReceivedTimeNs != nil {
			e2e := *r.CallbackReceivedTimeNs - r.SubmitTimeNs
			r.EndToEndNs = &e2e
			r.Completed = true
		}

		if err := fn(r); err != nil {
			return err
		}
	}

	return rows.Err()
}

// ExportCallbacks streams stored terminal callbacks, including ones that never
// matched a submission. onlyUnmatched narrows the result to those orphans,
// which are worth checking after every run.
func (d *DBPersister) ExportCallbacks(ctx context.Context, runID string, onlyUnmatched bool, fn func(CallbackRow) error) error {
	query := `
SELECT c.job_id, s.run_id, c.received_time_ns, c.last_received_time_ns, c.source,
       c.status, c.raw_status, c.event, c.reason, c.reported_queued_time_ns,
       c.reported_start_time_ns, c.reported_end_time_ns, c.reported_duration_ms,
       c.delivery_count, c.delivery_attempt
  FROM job_callback c
  LEFT JOIN job_submission s ON s.job_id = c.job_id
 WHERE (:run_id = '' OR s.run_id = :run_id)
   AND (:only_unmatched = 0 OR s.job_id IS NULL)
 ORDER BY c.received_time_ns`

	rows, err := d.db.QueryContext(ctx, query,
		sql.Named("run_id", runID), sql.Named("only_unmatched", boolToInt(onlyUnmatched)))
	if err != nil {
		return fmt.Errorf("query callbacks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r CallbackRow
		if err := rows.Scan(&r.JobID, &r.RunID, &r.ReceivedTimeNs, &r.LastReceivedTimeNs,
			&r.Source, &r.Status, &r.RawStatus, &r.Event, &r.Reason,
			&r.ReportedQueuedTimeNs, &r.ReportedStartTimeNs, &r.ReportedEndTimeNs,
			&r.ReportedDurationMs, &r.DeliveryCount, &r.DeliveryAttempt); err != nil {
			return fmt.Errorf("scan callback: %w", err)
		}
		r.Matched = r.RunID != nil
		if err := fn(r); err != nil {
			return err
		}
	}

	return rows.Err()
}

// ListRuns returns every recorded run, newest first.
func (d *DBPersister) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT run_id, variant, target_host, workload_id, config_fingerprint, commit_hash,
       priority, requested_jobs, concurrency, rate_per_second, started_at_ns,
       finished_at_ns, submitted_jobs, failed_jobs, benchmarker_version, notes
  FROM benchmark_run
 ORDER BY started_at_ns DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.RunID, &r.Variant, &r.TargetHost, &r.WorkloadID,
			&r.ConfigFingerprint, &r.CommitHash, &r.Priority, &r.RequestedJobs,
			&r.Concurrency, &r.RatePerSecond, &r.StartedAtNs, &r.FinishedAtNs,
			&r.SubmittedJobs, &r.FailedJobs, &r.BenchmarkerVersion, &r.Notes); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}

	return runs, rows.Err()
}

// CountCallbacks returns how many distinct jobs have a terminal callback.
// Used by tests and by the run summary.
func (d *DBPersister) CountCallbacks(ctx context.Context) (int, error) {
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_callback`).Scan(&n)
	return n, err
}

// CountEvents returns how many callback POSTs were received in total,
// including duplicates and non-terminal notifications.
func (d *DBPersister) CountEvents(ctx context.Context) (int, error) {
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_event`).Scan(&n)
	return n, err
}

// StoreDeprecatedReport records a call to the deprecated /v1/start_time or
// /v1/result endpoints. These write to the legacy job_results table only and
// never feed the measurement path.
func (d *DBPersister) StoreDeprecatedReport(ctx context.Context, kind string, id string, reported time.Time) error {
	return d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var query string
		switch kind {
		case "start":
			query = `INSERT INTO job_results (id, start_time) VALUES (?, ?)
                     ON CONFLICT (id) DO UPDATE SET start_time = excluded.start_time`
		case "end":
			query = `INSERT INTO job_results (id, end_time) VALUES (?, ?)
                     ON CONFLICT (id) DO UPDATE SET end_time = excluded.end_time`
		default:
			return fmt.Errorf("unknown deprecated report kind %q", kind)
		}
		_, err := tx.ExecContext(ctx, query, id, reported.UTC())
		return err
	})
}

func nullString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullNonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
