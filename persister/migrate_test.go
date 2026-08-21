package persister

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestLoadMigrationsAreOrderedAndUnique(t *testing.T) {
	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migrations) < 2 {
		t.Fatalf("expected at least the baseline and measurement migrations, got %d", len(migrations))
	}

	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version <= migrations[i-1].Version {
			t.Fatalf("migrations out of order: %d then %d", migrations[i-1].Version, migrations[i].Version)
		}
	}
}

func TestMigrateAppliesEverythingOnce(t *testing.T) {
	db := openRawDB(t)

	applied, err := Migrate(context.Background(), db)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("first Migrate applied nothing")
	}

	// Re-running must be a no-op. The previous bootstrap used
	// CREATE TABLE IF NOT EXISTS, which is also a no-op, but silently so: it
	// could never apply a change to an existing database.
	appliedAgain, err := Migrate(context.Background(), db)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(appliedAgain) != 0 {
		t.Fatalf("second Migrate re-applied %v", appliedAgain)
	}

	version, err := SchemaVersion(context.Background(), db)
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if version != applied[len(applied)-1] {
		t.Errorf("SchemaVersion = %d, want %d", version, applied[len(applied)-1])
	}
}

// A database created by the pre-migration bootstrap has the legacy tables but
// no schema_migrations row. Migrating it must succeed and must not destroy the
// rows already there.
func TestMigrateOverExistingLegacyDatabase(t *testing.T) {
	db := openRawDB(t)
	ctx := context.Background()

	legacyBootstrap := `
CREATE TABLE IF NOT EXISTS scheduled_job (
    id uuid PRIMARY KEY, creation_time timestamp NOT NULL, executor text NOT NULL,
    metadata jsonb, commit_hash text DEFAULT NULL);
CREATE TABLE IF NOT EXISTS job_results (
    id uuid PRIMARY KEY, start_time timestamp NULL, end_time timestamp NULL);`

	if _, err := db.ExecContext(ctx, legacyBootstrap); err != nil {
		t.Fatalf("legacy bootstrap: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO scheduled_job (id, creation_time, executor) VALUES ('old-job', '2026-01-01 00:00:00', 'HadesDockerExecutor')`,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if _, err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate over legacy db: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_job`).Scan(&count); err != nil {
		t.Fatalf("count legacy rows: %v", err)
	}
	if count != 1 {
		t.Errorf("legacy row count = %d, want 1; migrating must not destroy collected data", count)
	}

	for _, table := range []string{"benchmark_run", "job_submission", "job_callback", "job_event"} {
		if !tableExists(t, db, table) {
			t.Errorf("table %q missing after migration", table)
		}
	}
}

// Every measurement timestamp column must be an INTEGER holding epoch
// nanoseconds. A text timestamp would reintroduce strftime and its
// second-flooring.
func TestMeasurementTimestampColumnsAreIntegers(t *testing.T) {
	db := openRawDB(t)
	if _, err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	expected := map[string][]string{
		"benchmark_run":  {"started_at_ns", "finished_at_ns"},
		"job_submission": {"submit_time_ns", "submit_ack_time_ns"},
		"job_callback":   {"received_time_ns", "last_received_time_ns", "reported_start_time_ns", "reported_end_time_ns"},
		"job_event":      {"received_time_ns"},
	}

	for table, columns := range expected {
		types := columnTypes(t, db, table)
		for _, column := range columns {
			got, ok := types[column]
			if !ok {
				t.Errorf("%s.%s missing", table, column)
				continue
			}
			if got != "INTEGER" {
				t.Errorf("%s.%s has type %q, want INTEGER", table, column, got)
			}
		}
	}
}

// job_submission.run_id must reference benchmark_run. The legacy schema had no
// foreign key at all, so job_results rows could reference scheduled_job rows
// that did not exist.
func TestForeignKeyOnSubmissions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.RecordSubmission(ctx, Submission{
		SubmissionID: "s1",
		RunID:        "no-such-run",
		Seq:          0,
		Variant:      "hades-docker",
		TargetHost:   "sut-1",
		WorkloadID:   "w",
		SubmitTimeNs: 1,
		SubmitStatus: SubmitStatusAccepted,
	})
	if err == nil {
		t.Fatal("expected a foreign key violation for an unknown run_id")
	}
}

func openRawDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", "file:"+path+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("sqlite_master lookup: %v", err)
	}
	return true
}

func columnTypes(t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()

	rows, err := db.Query(`SELECT name, type FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()

	types := make(map[string]string)
	for rows.Next() {
		var name, columnType string
		if err := rows.Scan(&name, &columnType); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		types[name] = columnType
	}
	return types
}
