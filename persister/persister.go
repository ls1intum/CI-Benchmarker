// Package persister owns the benchmark database.
//
// Two schemas live here side by side:
//
//   - The measurement core (benchmark_run, job_submission, job_callback,
//     job_event) is the source of publication data. Timestamps are epoch
//     nanoseconds stored as INTEGER and taken from the benchmarker's own clock.
//     See measurement.go.
//   - The legacy schema (scheduled_job, job_results) backs the deprecated
//     /v1/start_time and /v1/result endpoints and the old aggregate metrics
//     endpoints. It is kept so existing databases and dashboards keep working;
//     nothing new should be built on it. See legacy.go.
package persister

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/Hades-Scheduler/CI-Benchmarker/persister/model"
)

// DefaultDBFile is used when no path is configured.
const DefaultDBFile = "benchmark.db"

const maxAttempts = 5

// readPoolSize bounds concurrent readers. In WAL mode readers never block the
// writer, so the read pool can be wide.
const readPoolSize = 8

// DBPersister is the SQLite-backed store. One instance is shared process-wide;
// it holds two connection pools and a writer goroutine, so creating one per
// HTTP request (as the metrics handlers used to) leaks all three.
//
// Reads and writes get separate handles on purpose. They need different
// transaction semantics: the writer wants BEGIN IMMEDIATE so it takes the write
// lock up front, while readers must use the default deferred transactions or a
// long-running read (an export, a histogram query) would hold the write lock
// and stall every incoming callback behind it. Sharing one handle, as the
// original db.SetMaxOpenConns(1) configuration did, is what made a concurrent
// read and a callback burst contend for the same connection.
type DBPersister struct {
	db      *sql.DB // read pool, deferred transactions
	writeDB *sql.DB // single connection, BEGIN IMMEDIATE
	queries *model.Queries
	writer  *writer

	closeOnce sync.Once
}

var (
	defaultMu    sync.Mutex
	defaultStore *DBPersister
)

// resolveDBPath falls back to DefaultDBFile when no path is configured.
func resolveDBPath(path string) string {
	if path == "" {
		return DefaultDBFile
	}
	return path
}

// baseDSNParams are shared by both handles. The reader and the writer must
// agree on journal mode and busy timeout; only the locking behaviour differs.
var baseDSNParams = []string{
	"_journal_mode=WAL",
	"_busy_timeout=10000",
	"_synchronous=NORMAL",
	"_foreign_keys=on",
}

// dsnFor builds a SQLite DSN for a filesystem path, appending extra parameters
// to the shared set. Assembling the query from a slice means a caller cannot
// produce a malformed DSN by getting a separator wrong.
//
// The path is percent-escaped so that a `?` or `#` in it cannot truncate the URI
// or be mistaken for a query parameter. SQLite's own URI parser decodes the
// escapes again, so an absolute path still resolves to that absolute path -
// which persister_test.go asserts, because getting it wrong would silently write
// the database outside the mounted volume.
func dsnFor(path string, extra ...string) string {
	params := append(append([]string{}, baseDSNParams...), extra...)
	return "file:" + url.PathEscape(path) + "?" + strings.Join(params, "&")
}

// Open creates a persister backed by the SQLite file at path and brings the
// schema up to date.
func Open(path string) (*DBPersister, error) {
	dbPath := resolveDBPath(path)

	readDB, err := sql.Open("sqlite3", dsnFor(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	readDB.SetMaxOpenConns(readPoolSize)
	readDB.SetMaxIdleConns(readPoolSize)
	readDB.SetConnMaxLifetime(0)

	// _txlock=immediate makes the writer take the write lock at BEGIN rather
	// than upgrading mid-transaction, which is where SQLITE_BUSY comes from.
	writeDB, err := sql.Open("sqlite3", dsnFor(dbPath, "_txlock=immediate"))
	if err != nil {
		_ = readDB.Close()
		return nil, fmt.Errorf("open write handle: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	closeAll := func() {
		_ = readDB.Close()
		_ = writeDB.Close()
	}

	if err := readDB.PingContext(ctx); err != nil {
		closeAll()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	if _, err := Migrate(ctx, writeDB); err != nil {
		closeAll()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &DBPersister{
		db:      readDB,
		writeDB: writeDB,
		queries: model.New(readDB),
		writer:  newWriter(writeDB),
	}, nil
}

// InitDefault opens the process-wide persister. Call once at startup.
func InitDefault(path string) (*DBPersister, error) {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if defaultStore != nil {
		return defaultStore, nil
	}

	store, err := Open(path)
	if err != nil {
		return nil, err
	}
	defaultStore = store
	return store, nil
}

// Default returns the process-wide persister, or nil if InitDefault has not run.
// Default returns the process-wide persister, panicking with a named cause if
// it has not been installed yet.
//
// Returning nil here produced a bare nil dereference several frames away, inside
// whichever deprecated metrics handler happened to run first, which says nothing
// about the actual mistake. Only the deprecated aggregate endpoints still reach
// for the global; everything on the measurement path takes the store as an
// argument.
func Default() *DBPersister {
	defaultMu.Lock()
	defer defaultMu.Unlock()

	if defaultStore == nil {
		panic("persister: Default() called before InitDefault/SetDefault installed a store")
	}
	return defaultStore
}

// SetDefault installs a persister as the process-wide one. Intended for tests.
func SetDefault(p *DBPersister) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStore = p
}

// Close drains queued writes and releases the database.
func (d *DBPersister) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.writer.close()
		err = errors.Join(d.writeDB.Close(), d.db.Close())
	})
	return err
}

// DB exposes the read handle for tests and schema introspection.
func (d *DBPersister) DB() *sql.DB { return d.db }

func withRetry(op func(ctx context.Context) error) error {
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := op(ctx)
		cancel()

		if err == nil {
			return nil
		}
		if isSQLiteBusyOrLocked(err) || isDatabaseLockedMsg(err) {
			time.Sleep(backoff(attempt))
			last = err
			continue
		}
		return err // any other error is not going to fix itself
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", maxAttempts, last)
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt-1)) * 20 * time.Millisecond
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

// MustOpenDefault opens the process-wide persister or terminates. Used at
// startup where continuing without a database is meaningless.
func MustOpenDefault(path string) *DBPersister {
	store, err := InitDefault(path)
	if err != nil {
		slog.Error("Failed to open benchmark database", slog.String("path", path), slog.Any("error", err))
		panic(err)
	}
	return store
}
