package persister

import (
	"context"
	"database/sql"
	_ "embed"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Mtze/CI-Benchmarker/persister/model"
	"github.com/google/uuid"
)

const file string = "benchmark.db"

// Persister interface
// This interface is used to store the job and the result of the job
// This implementation allows to abstract the concrete storage mechanism
type Persister interface {
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
	ctx := context.Background()

	// Open the database
	db, err := sql.Open("sqlite3", file)
	if err != nil {
		slog.Error("Error while opening DB", slog.Any("error", err))
		// Panic if the database cannot be opened
		panic(err)
	}

	// Create the table
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		slog.Debug("Error while creating table", slog.Any("error", err))
	}

	queries := model.New(db)

	return DBPersister{
		db:      db,
		queries: queries,
	}
}

func (d DBPersister) StoreJob(uuid uuid.UUID, creationTime time.Time, executor string, commitHash *string) {
	var nullableHash sql.NullString
	if commitHash != nil {
		nullableHash = sql.NullString{String: *commitHash, Valid: true}
	} else {
		nullableHash = sql.NullString{Valid: false}
	}
	d.queries.StoreScheduledJob(context.Background(), model.StoreScheduledJobParams{
		ID:           uuid,
		CreationTime: creationTime.UTC(),
		Executor:     executor,
		CommitHash:   nullableHash,
	})
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
