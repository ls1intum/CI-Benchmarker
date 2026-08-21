package persister

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// The reader and the writer must differ only in locking behaviour, and the
// writer's DSN has to stay well-formed however the shared parameters change.
func TestDSNAppendsExtraParametersWithoutMalformingTheQuery(t *testing.T) {
	read := dsnFor("/data/benchmark.db")
	write := dsnFor("/data/benchmark.db", "_txlock=immediate")

	if strings.Contains(read, "&&") || strings.HasSuffix(read, "&") {
		t.Errorf("read DSN is malformed: %s", read)
	}
	if strings.Contains(write, "&&") || strings.HasSuffix(write, "&") {
		t.Errorf("write DSN is malformed: %s", write)
	}
	if strings.Count(write, "?") != 1 {
		t.Errorf("write DSN has %d query separators: %s", strings.Count(write, "?"), write)
	}
	if !strings.HasSuffix(write, "&_txlock=immediate") {
		t.Errorf("write DSN did not gain _txlock=immediate: %s", write)
	}
	if write != read+"&_txlock=immediate" {
		t.Errorf("read and write DSNs differ by more than the lock mode:\nread  %s\nwrite %s", read, write)
	}

	query := write[strings.Index(write, "?")+1:]
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("write DSN query does not parse: %v", err)
	}
	if values.Get("_txlock") != "immediate" {
		t.Errorf("_txlock = %q, want immediate", values.Get("_txlock"))
	}
	if values.Get("_journal_mode") != "WAL" {
		t.Errorf("_journal_mode = %q, want WAL", values.Get("_journal_mode"))
	}
}

// A missing default store must name itself rather than surfacing as a nil
// dereference several frames away inside a deprecated metrics handler.
func TestDefaultPanicsWithANamedCauseWhenUninitialised(t *testing.T) {
	defaultMu.Lock()
	saved := defaultStore
	defaultStore = nil
	defaultMu.Unlock()
	t.Cleanup(func() { SetDefault(saved) })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Default() returned instead of panicking with an uninitialised store")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "InitDefault") {
			t.Errorf("panic message %v does not name the cause", r)
		}
	}()

	Default()
}
