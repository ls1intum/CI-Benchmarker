package persister

import (
	"context"
	"database/sql"
	"time"

	"github.com/Hades-Scheduler/CI-Benchmarker/persister/model"
	"github.com/google/uuid"
)

// This file holds the pre-measurement-core storage path.
//
// It is retained so that databases collected before the rework stay readable
// and the old aggregate metrics endpoints keep responding. Do not extend it.
// Every metric it can produce is truncated to whole seconds by strftime('%s')
// and every query it backs filters on start_time IS NOT NULL, which drops any
// job whose in-workload reporter failed.
//
// Two things did change here, because they were losing data rather than merely
// measuring it badly: the writes now go through the serialised writer with
// retries, and they return their errors instead of discarding them.

// StoreJob records a scheduled job in the legacy table.
func (d *DBPersister) StoreJob(ctx context.Context, id uuid.UUID, creationTime time.Time, executor string, commitHash *string) error {
	return d.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO scheduled_job (id, creation_time, executor, commit_hash) VALUES (?, ?, ?, ?)
             ON CONFLICT (id) DO NOTHING`,
			id, creationTime.UTC(), executor, nullString(commitHash))
		return err
	})
}

// StoreStartTime records a build start time reported by the deprecated
// /v1/start_time endpoint.
//
// The previous implementation ignored the returned error entirely and did not
// go through withRetry, so a single SQLITE_BUSY dropped the observation without
// a trace.
func (d *DBPersister) StoreStartTime(ctx context.Context, id uuid.UUID, startTime time.Time) error {
	return d.StoreDeprecatedReport(ctx, "start", id.String(), startTime)
}

// StoreResult records a build completion time reported by the deprecated
// /v1/result endpoint. Same caveat as StoreStartTime.
func (d *DBPersister) StoreResult(ctx context.Context, id uuid.UUID, endTime time.Time) error {
	return d.StoreDeprecatedReport(ctx, "end", id.String(), endTime)
}

func legacyRangeParams(from, to *time.Time, commitHash *string) (sql.NullTime, sql.NullTime, sql.NullString) {
	var fromNull, toNull sql.NullTime
	var hashNull sql.NullString

	if from != nil {
		fromNull = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		toNull = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		hashNull = sql.NullString{String: *commitHash, Valid: true}
	}

	return fromNull, toNull, hashNull
}

func (d *DBPersister) GetQueueLatenciesInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetQueueLatenciesInRangeByCommitAndExecutor(context.Background(),
		model.GetQueueLatenciesInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}

func (d *DBPersister) GetBuildTimesInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetBuildTimesInRangeByCommitAndExecutor(context.Background(),
		model.GetBuildTimesInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}

func (d *DBPersister) GetQueueLatencySummaryInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetQueueLatencySummaryInRangeByCommitAndExecutor(context.Background(),
		model.GetQueueLatencySummaryInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}

func (d *DBPersister) GetBuildTimeSummaryInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetBuildTimeSummaryInRangeByCommitAndExecutor(context.Background(),
		model.GetBuildTimeSummaryInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}

func (d *DBPersister) GetTotalLatenciesInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetTotalLatenciesInRangeByCommitAndExecutor(context.Background(),
		model.GetTotalLatenciesInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}

func (d *DBPersister) GetTotalLatenciesSummaryInRange(from, to *time.Time, commitHash *string, executor string) ([]int64, error) {
	f, t, h := legacyRangeParams(from, to, commitHash)
	return d.queries.GetTotalLatenciesSummaryInRangeByCommitAndExecutor(context.Background(),
		model.GetTotalLatenciesSummaryInRangeByCommitAndExecutorParams{From: f, To: t, CommitHash: h, Executor: executor})
}
