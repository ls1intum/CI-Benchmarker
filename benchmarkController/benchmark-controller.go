// Package benchmarkController drives load against a system under test and
// records exactly what it did.
//
// The controller only produces the submission half of each measurement. The
// completion half arrives asynchronously at the status-callback receiver, which
// is why this handler returns as soon as every job has been submitted rather
// than waiting for jobs to finish.
package benchmarkController

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hades-scheduler/hades/shared/payload"

	"github.com/Hades-Scheduler/CI-Benchmarker/executor"
	"github.com/Hades-Scheduler/CI-Benchmarker/persister"
	"github.com/Hades-Scheduler/CI-Benchmarker/shared/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Benchmark runs one variant against one host.
type Benchmark struct {
	Executor  executor.Executor
	Persister *persister.DBPersister
	Config    config.Config
}

// RunResponse is returned once every job has been submitted. Jobs are still
// running at this point; their completions land at /v1/callback.
//
// @Description Result of a benchmark submission phase.
type RunResponse struct {
	RunID             string  `json:"run_id"`
	Variant           string  `json:"variant"`
	TargetHost        string  `json:"target_host"`
	WorkloadID        string  `json:"workload_id"`
	ConfigFingerprint string  `json:"config_fingerprint"`
	RequestedJobs     int     `json:"requested_jobs"`
	SubmittedJobs     int     `json:"submitted_jobs"`
	FailedJobs        int     `json:"failed_jobs"`
	Concurrency       int     `json:"concurrency"`
	RatePerSecond     float64 `json:"rate_per_second"`
	SubmitWallClockNs int64   `json:"submit_wall_clock_ns"`
	ExportURL         string  `json:"export_url"`
}

type runOptions struct {
	runID         string
	count         int
	concurrency   int
	ratePerSecond float64
	commitHash    *string
	workloadID    string
	notes         *string
}

// HandleFunc parses the request, runs the submission phase and reports what
// happened.
//
// @Summary      Start a benchmark run
// @Description  Submits `count` jobs to the system under test and records one row per submission attempt, successful or not.
// @Tags         benchmark
// @Accept       json
// @Produce      json
// @Param        payload      body   payload.RESTPayload  true   "Job payload to submit"
// @Param        count        query  int     false  "Number of jobs to submit"  default(1)
// @Param        run_id       query  string  false  "Run identifier; generated when omitted"
// @Param        concurrency  query  int     false  "Maximum in-flight submissions"
// @Param        rate         query  number  false  "Offered submissions per second; 0 submits as fast as concurrency allows"
// @Param        workload_id  query  string  false  "Identifier for the submitted payload; defaults to the payload name"
// @Param        commit_hash  query  string  false  "Optional run tag"
// @Param        notes        query  string  false  "Free-text note stored with the run"
// @Success      200  {object}  RunResponse
// @Failure      400  {object}  response.ErrorMessage
// @Failure      500  {object}  response.ServerErrorMessage
func (b Benchmark) HandleFunc(c *gin.Context) {
	var restPayload payload.RESTPayload
	if err := c.ShouldBindJSON(&restPayload); err != nil {
		slog.Error("Failed to bind benchmark payload", slog.Any("error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to bind JSON: " + err.Error()})
		return
	}

	opts, err := b.parseOptions(c, restPayload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := b.run(c.Request.Context(), restPayload, opts)
	if err != nil {
		slog.Error("Benchmark run failed", slog.String("run_id", opts.runID), slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (b Benchmark) parseOptions(c *gin.Context, p payload.RESTPayload) (runOptions, error) {
	opts := runOptions{
		concurrency:   b.Config.DefaultConcurrency,
		ratePerSecond: b.Config.DefaultRatePerSecond,
		count:         1,
	}

	if raw := c.DefaultQuery("count", "1"); raw != "" {
		count, err := strconv.Atoi(raw)
		if err != nil || count < 1 {
			return opts, errBadQuery("count", raw)
		}
		opts.count = count
	}

	if raw := c.Query("concurrency"); raw != "" {
		concurrency, err := strconv.Atoi(raw)
		if err != nil || concurrency < 1 {
			return opts, errBadQuery("concurrency", raw)
		}
		opts.concurrency = concurrency
	}
	if opts.concurrency < 1 {
		opts.concurrency = 1
	}

	if raw := c.Query("rate"); raw != "" {
		rate, err := strconv.ParseFloat(raw, 64)
		if err != nil || rate < 0 {
			return opts, errBadQuery("rate", raw)
		}
		opts.ratePerSecond = rate
	}

	opts.runID = c.Query("run_id")
	if opts.runID == "" {
		opts.runID = uuid.NewString()
	}

	opts.workloadID = c.Query("workload_id")
	if opts.workloadID == "" {
		opts.workloadID = p.Name
	}
	if opts.workloadID == "" {
		opts.workloadID = "unnamed"
	}

	if hash := c.Query("commit_hash"); hash != "" {
		opts.commitHash = &hash
	}
	if notes := c.Query("notes"); notes != "" {
		opts.notes = &notes
	}

	return opts, nil
}

type queryError struct{ param, value string }

func (e queryError) Error() string {
	return "invalid value " + strconv.Quote(e.value) + " for query parameter " + e.param
}

func errBadQuery(param, value string) error { return queryError{param: param, value: value} }

// run performs the submission phase.
//
// Timing contract, and the reason this function exists in this shape:
//
//	submit_time_ns     is read immediately BEFORE Execute
//	submit_ack_time_ns is read immediately AFTER Execute returns
//
// The previous implementation read a single time.Now() after Execute returned
// and stored it as the job's creation time, so queue latency was short by
// exactly the submission round-trip. That error is not constant: the round-trip
// grows under load, which biases the loaded end of every measurement, and it
// grows differently per variant, which biases the comparison itself.
func (b Benchmark) run(ctx context.Context, jobPayload payload.RESTPayload, opts runOptions) (RunResponse, error) {
	variant := b.Executor.Variant()
	targetHost := b.Executor.TargetHost()
	fingerprint := configFingerprint(variant, targetHost, opts, jobPayload)

	run := persister.Run{
		RunID:              opts.runID,
		Variant:            variant,
		TargetHost:         targetHost,
		WorkloadID:         opts.workloadID,
		ConfigFingerprint:  fingerprint,
		CommitHash:         opts.commitHash,
		Priority:           jobPayload.Priority,
		RequestedJobs:      opts.count,
		Concurrency:        opts.concurrency,
		RatePerSecond:      opts.ratePerSecond,
		StartedAtNs:        time.Now().UnixNano(),
		BenchmarkerVersion: config.Version,
		Notes:              opts.notes,
	}

	if err := b.Persister.CreateRun(ctx, run); err != nil {
		return RunResponse{}, err
	}

	slog.Info("Starting benchmark run",
		slog.String("run_id", opts.runID), slog.String("variant", variant),
		slog.String("target_host", targetHost), slog.Int("count", opts.count),
		slog.Int("concurrency", opts.concurrency), slog.Float64("rate", opts.ratePerSecond))

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		submitted int
		failed    int
	)

	// The semaphore caps in-flight submissions. Together with the open-loop
	// pacer below it is what makes the offered load identical across variants:
	// both come from this one code path rather than from whatever connection
	// limit each executor happened to configure.
	//
	// While free slots exist the pacer is purely open-loop. Once the cap binds,
	// submissions are necessarily gated by completions, and no arrangement of
	// this code changes that - moving the acquire into the goroutine below only
	// moves where the wait happens, at the cost of an unbounded number of parked
	// goroutines, since count is not bounded above. What matters is that the
	// distortion is not silent: scheduled_release_ns records when each
	// submission was due, so submit_time_ns - scheduled_release_ns quantifies
	// exactly how far the cap pushed the run off its own schedule.
	semaphore := make(chan struct{}, opts.concurrency)
	pacingStart := time.Now()

	// A cancelled request must not return before the launched goroutines are
	// done. They persist with their own background context, so returning early
	// would leave benchmark_run with a NULL finished_at and 0/0 tallies while
	// job_submission kept gaining rows - the run summary would contradict the
	// exported rows - and would let a shutdown Close() race those writes.
	var runErr error

submissions:
	for seq := 0; seq < opts.count; seq++ {
		// Open-loop pacing: release times are computed from a fixed schedule,
		// not from when the previous submission finished. A closed-loop pacer
		// would slow down exactly when the system under test slows down, which
		// hides the queueing it is supposed to reveal (coordinated omission).
		var scheduledRelease *int64
		if opts.ratePerSecond > 0 {
			offset := time.Duration(float64(seq) / opts.ratePerSecond * float64(time.Second))
			releaseAt := pacingStart.Add(offset)

			// Recorded per submission so that slip against the schedule is
			// measurable afterwards instead of having to be assumed absent.
			releaseNs := releaseAt.UnixNano()
			scheduledRelease = &releaseNs

			if delay := time.Until(releaseAt); delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					runErr = ctx.Err()
					break submissions
				}
			}
		}

		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			runErr = ctx.Err()
			break submissions
		}

		wg.Add(1)
		go func(seq int, scheduledRelease *int64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			submission := b.submitOne(ctx, jobPayload, opts, variant, targetHost, fingerprint, seq)
			submission.ScheduledReleaseNs = scheduledRelease

			mu.Lock()
			if submission.SubmitStatus == persister.SubmitStatusAccepted {
				submitted++
			} else {
				failed++
			}
			mu.Unlock()

			// Persisted after the timed section so that database work can
			// never appear inside a measured interval.
			recordCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := b.Persister.RecordSubmission(recordCtx, submission); err != nil {
				slog.Error("Failed to record submission",
					slog.String("run_id", opts.runID), slog.Int("seq", seq), slog.Any("error", err))
			}

			// Deprecated dual-write so the legacy aggregate metrics endpoints
			// keep returning something for runs made during the transition.
			if submission.JobID != nil {
				if jobID, err := uuid.Parse(*submission.JobID); err == nil {
					if err := b.Persister.StoreJob(recordCtx, jobID,
						time.Unix(0, submission.SubmitTimeNs), b.Executor.Name(), opts.commitHash); err != nil {
						slog.Debug("Legacy StoreJob failed", slog.Any("error", err))
					}
				}
			}
		}(seq, scheduledRelease)
	}

	wg.Wait()
	finishedAt := time.Now()

	// Deliberately not the request context: a client that disconnected still
	// leaves a run whose tallies have to be written, or the row is unusable.
	// submitted+failed < requested_jobs is then how a cancelled run is
	// recognised in the data.
	finishCtx, cancelFinish := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelFinish()

	if err := b.Persister.FinishRun(finishCtx, opts.runID, finishedAt.UnixNano(), submitted, failed); err != nil {
		slog.Error("Failed to finish run", slog.String("run_id", opts.runID), slog.Any("error", err))
	}

	if runErr != nil {
		slog.Warn("Benchmark run cancelled; recorded what was submitted",
			slog.String("run_id", opts.runID), slog.Int("submitted", submitted),
			slog.Int("failed", failed), slog.Int("requested", opts.count))
		return RunResponse{}, runErr
	}

	slog.Info("Benchmark submission phase complete",
		slog.String("run_id", opts.runID), slog.Int("submitted", submitted), slog.Int("failed", failed))

	return RunResponse{
		RunID:             opts.runID,
		Variant:           variant,
		TargetHost:        targetHost,
		WorkloadID:        opts.workloadID,
		ConfigFingerprint: fingerprint,
		RequestedJobs:     opts.count,
		SubmittedJobs:     submitted,
		FailedJobs:        failed,
		Concurrency:       opts.concurrency,
		RatePerSecond:     opts.ratePerSecond,
		SubmitWallClockNs: finishedAt.UnixNano() - run.StartedAtNs,
		ExportURL:         "/v1/export/jobs?run_id=" + opts.runID,
	}, nil
}

// submitOne performs and times exactly one submission.
func (b Benchmark) submitOne(
	ctx context.Context,
	jobPayload payload.RESTPayload,
	opts runOptions,
	variant, targetHost, fingerprint string,
	seq int,
) persister.Submission {
	submission := persister.Submission{
		SubmissionID:      uuid.NewString(),
		RunID:             opts.runID,
		Seq:               seq,
		Variant:           variant,
		TargetHost:        targetHost,
		WorkloadID:        opts.workloadID,
		ConfigFingerprint: fingerprint,
		Priority:          jobPayload.Priority,
		CommitHash:        opts.commitHash,
	}

	// --- start of the timed section; nothing but the submission belongs here
	submitTime := time.Now()
	jobID, err := b.Executor.Execute(ctx, jobPayload)
	ackTime := time.Now()
	// --- end of the timed section

	submission.SubmitTimeNs = submitTime.UnixNano()
	ackNs := ackTime.UnixNano()
	submission.SubmitAckTimeNs = &ackNs

	if err != nil {
		// A failed submission is recorded, not dropped. Previously the
		// goroutine returned here without storing anything, so a run of 400
		// that only managed 350 submissions was indistinguishable from a run
		// of 350 and the failures never appeared in any denominator.
		message := err.Error()
		submission.SubmitStatus = persister.SubmitStatusFailed
		submission.SubmitError = &message
		return submission
	}

	id := jobID.String()
	submission.JobID = &id
	submission.SubmitStatus = persister.SubmitStatusAccepted
	return submission
}

// configFingerprint hashes everything that defines the conditions of a run, so
// two runs can be checked for comparability instead of assumed comparable.
func configFingerprint(variant, targetHost string, opts runOptions, jobPayload payload.RESTPayload) string {
	canonical := struct {
		Variant     string              `json:"variant"`
		TargetHost  string              `json:"target_host"`
		WorkloadID  string              `json:"workload_id"`
		Count       int                 `json:"count"`
		Concurrency int                 `json:"concurrency"`
		Rate        float64             `json:"rate_per_second"`
		Priority    int                 `json:"priority"`
		Payload     payload.RESTPayload `json:"payload"`
	}{
		Variant:     variant,
		TargetHost:  targetHost,
		WorkloadID:  opts.workloadID,
		Count:       opts.count,
		Concurrency: opts.concurrency,
		Rate:        opts.ratePerSecond,
		Priority:    jobPayload.Priority,
		Payload:     jobPayload,
	}

	// The payload carries a per-job id and timestamp that vary between
	// otherwise identical runs; blank them so the fingerprint describes the
	// configuration rather than one instance of it.
	canonical.Payload.QueuePayload.ID = uuid.Nil
	canonical.Payload.QueuePayload.Timestamp = time.Time{}

	encoded, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
