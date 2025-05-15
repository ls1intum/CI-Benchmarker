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
	StoreJob(uuid uuid.UUID, creationTime time.Time, executor string)
	StoreResult(uuid uuid.UUID, startTime time.Time, endTime time.Time)
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

func (d DBPersister) StoreJob(uuid uuid.UUID, creationTime time.Time, executor string) {
	d.queries.StoreScheduledJob(context.Background(), model.StoreScheduledJobParams{
		ID:           uuid,
		CreationTime: creationTime.UTC().Format("2006-01-02 15:04:05"),
		Executor:     executor,
	})
}

func (d DBPersister) StoreResult(uuid uuid.UUID, startTime time.Time, endTime time.Time) {
	d.queries.StoreJobResult(context.Background(), model.StoreJobResultParams{
		ID:        uuid,
		StartTime: startTime.UTC().Format("2006-01-02 15:04:05"),
		EndTime:   endTime.UTC().Format("2006-01-02 15:04:05"),
	})
}

func (d DBPersister) GetQueueLatenciesInRange(from, to *time.Time) ([]int64, error) {
	ctx := context.Background()

	params := model.GetQueueLatenciesInRangeParams{
		From: nil,
		To:   nil,
	}

	if from != nil {
		params.From = from.UTC().Format("2006-01-02 15:04:05")
	}
	if to != nil {
		params.To = to.UTC().Format("2006-01-02 15:04:05")
	}

	return d.queries.GetQueueLatenciesInRange(ctx, params)
}

func (d DBPersister) GetBuildTimesInRange(from, to *time.Time) ([]int64, error) {
	ctx := context.Background()

	params := model.GetBuildTimesInRangeParams{
		From: nil,
		To:   nil,
	}

	if from != nil {
		params.From = from.UTC().Format("2006-01-02 15:04:05")
	}
	if to != nil {
		params.To = to.UTC().Format("2006-01-02 15:04:05")
	}

	return d.queries.GetBuildTimesInRange(ctx, params)
}
