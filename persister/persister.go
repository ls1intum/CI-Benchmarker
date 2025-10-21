package persister

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/Mtze/CI-Benchmarker/persister/model"
	"github.com/google/uuid"
)

const file string = "benchmark.db"
const maxAttempts = 5

// Persister interface
// This interface is used to store the job and the result of the job
// This implementation allows to abstract the concrete storage mechanism
type Persister interface {
	StoreJobWithMetadata(uuid uuid.UUID, creationTime time.Time, executor string, metaData *string, commitHash *string)
	StoreJob(uuid uuid.UUID, creationTime time.Time, executor string, commitHash *string)
	StoreStartTime(uuid uuid.UUID, startTime time.Time)
	StoreResult(uuid uuid.UUID, time time.Time)
}

// DBPersister is a concrete implementation of the Persister interface
// It uses a SQLite database to store the job and the result
type DBPersister struct {
	db      *sql.DB
	queries *model.Queries
}

//go:embed schema.sql
var ddl string

func NewDBPersister() DBPersister {
	dsn := "file:" + file + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		slog.Error("Error while opening DB", slog.Any("error", err))
		panic(err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, ddl); err != nil {
		slog.Error("Error while creating table", slog.Any("error", err))
	}

	queries := model.New(db)
	return DBPersister{db: db, queries: queries}
}

func withRetry(op func(ctx context.Context) error) error {
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := op(ctx)
		cancel()

		if err == nil {
			return nil
		}
		// only retry if the error is sqlite3.ErrBusy or sqlite3.ErrLocked
		if isSQLiteBusyOrLocked(err) || isDatabaseLockedMsg(err) {
			time.Sleep(backoff(attempt))
			last = err
			continue
		}
		return err // return other errors immediately
	}
	return fmt.Errorf("retry exhausted: %w", last)
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
}

func isSQLiteBusyOrLocked(err error) bool {
	var se sqlite3.Error
	if errors.As(err, &se) {
		return se.Code == sqlite3.ErrBusy || se.Code == sqlite3.ErrLocked
	}
	return false
}

func isDatabaseLockedMsg(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}

func (d DBPersister) StoreJobWithMetadata(uuid uuid.UUID, creationTime time.Time, executor string, metaData *string, commitHash *string) {
	var nullableHash sql.NullString
	if commitHash != nil {
		nullableHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		nullableHash = sql.NullString{Valid: false}
	}

	nullableMeta := sql.NullString{
		String: func() string {
			if metaData != nil {
				return *metaData
			}
			return ""
		}(),
		Valid: metaData != nil,
	}

	params := model.StoreScheduledJobWithMetadataParams{
		ID:           uuid,
		CreationTime: creationTime.UTC(),
		Executor:     executor,
		Metadata:     nullableMeta,
		CommitHash:   nullableHash,
	}

	if err := withRetry(func(ctx context.Context) error {
		_, err := d.queries.StoreScheduledJobWithMetadata(ctx, params)
		return err
	}); err != nil {
		slog.Error("StoreJob failed",
			slog.Any("uuid", uuid),
			slog.Any("executor", executor),
			slog.Any("error", err),
		)
	}
}

func (d DBPersister) StoreJob(uuid uuid.UUID, creationTime time.Time, executor string, commitHash *string) {
	var nullableHash sql.NullString
	if commitHash != nil {
		nullableHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		nullableHash = sql.NullString{Valid: false}
	}

	params := model.StoreScheduledJobParams{
		ID:           uuid,
		CreationTime: creationTime.UTC(),
		Executor:     executor,
		CommitHash:   nullableHash,
	}

	if err := withRetry(func(ctx context.Context) error {
		_, err := d.queries.StoreScheduledJob(ctx, params)
		return err
	}); err != nil {
		slog.Error("StoreJob failed",
			slog.Any("uuid", uuid),
			slog.Any("executor", executor),
			slog.Any("error", err),
		)
	}
}

func (d DBPersister) StoreStartTime(uuid uuid.UUID, startTime time.Time) {
	d.queries.UpsertJobStartTime(context.Background(), model.UpsertJobStartTimeParams{
		ID: uuid,
		StartTime: sql.NullTime{
			Time:  startTime.UTC(),
			Valid: true,
		},
	})
}

func (d DBPersister) StoreResult(uuid uuid.UUID, endTime time.Time) {
	d.queries.UpsertJobEndTime(context.Background(), model.UpsertJobEndTimeParams{
		ID: uuid,
		EndTime: sql.NullTime{
			Time:  endTime.UTC(),
			Valid: true,
		},
	})
}

func (d DBPersister) GetQueueLatenciesInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetQueueLatenciesInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetQueueLatenciesInRangeByCommit(ctx, params)
}

func (d DBPersister) GetBuildTimesInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetBuildTimesInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetBuildTimesInRangeByCommit(ctx, params)
}

func (d DBPersister) GetQueueLatencySummaryInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetQueueLatencySummaryInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetQueueLatencySummaryInRangeByCommit(ctx, params)
}

func (d DBPersister) GetBuildTimeSummaryInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetBuildTimeSummaryInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetBuildTimeSummaryInRangeByCommit(ctx, params)
}

func (d DBPersister) GetTotalLatenciesInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetTotalLatenciesInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetTotalLatenciesInRangeByCommit(ctx, params)
}

func (d DBPersister) GetTotalLatenciesSummaryInRange(from, to *time.Time, commitHash *string) ([]int64, error) {
	ctx := context.Background()

	params := model.GetTotalLatenciesSummaryInRangeByCommitParams{
		From: sql.NullTime{Valid: false},
		To:   sql.NullTime{Valid: false},
	}

	if from != nil {
		params.From = sql.NullTime{Time: from.UTC(), Valid: true}
	}
	if to != nil {
		params.To = sql.NullTime{Time: to.UTC(), Valid: true}
	}
	if commitHash != nil {
		params.CommitHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		params.CommitHash = sql.NullString{Valid: false}
	}

	return d.queries.GetTotalLatenciesSummaryInRangeByCommit(ctx, params)
}
