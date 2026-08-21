package persister

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// The container sets DB_PATH=/data/benchmark.db, pointing at a mounted volume.
// If the DSN ever stops resolving an absolute DB_PATH to that exact absolute
// path, the database silently lands in the container filesystem instead and a
// `docker compose up --force-recreate` destroys the dataset - the failure this
// whole change exists to prevent. Assert the resolution rather than trusting it.
func TestDSNResolvesAbsolutePathVerbatim(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "benchmark.db")

	db, err := sql.Open("sqlite3", dsnFor(want))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// sql.Open is lazy; force a connection so the file is actually created.
	if _, err := db.Exec("CREATE TABLE probe (x INTEGER)"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("database did not land at %s: %v", want, err)
	}
}

// A path containing characters that are structural in a URI must not truncate
// the DSN or leak into the query parameters.
func TestDSNEscapesPathsThatWouldBreakTheURI(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "run?a=1#frag.db")

	db, err := sql.Open("sqlite3", dsnFor(want))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE probe (x INTEGER)"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	if _, err := os.Stat(want); err != nil {
		t.Fatalf("database did not land at %s: %v", want, err)
	}
}

func TestDefaultDBFileUsedWhenPathEmpty(t *testing.T) {
	if got := resolveDBPath(""); got != DefaultDBFile {
		t.Errorf("resolveDBPath(%q) = %q, want %q", "", got, DefaultDBFile)
	}
	if got := resolveDBPath("/data/benchmark.db"); got != "/data/benchmark.db" {
		t.Errorf("resolveDBPath overrode an explicit path: got %q", got)
	}
}
