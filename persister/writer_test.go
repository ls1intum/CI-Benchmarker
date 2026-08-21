package persister

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// Shutdown must not race with in-flight callbacks. Before the enqueue/close
// transition was synchronised, a write submitted while Close ran could send on a
// closed channel and take the process down - during SIGTERM drain, which is
// exactly when queued measurements are meant to be saved.
func TestConcurrentWritesDuringCloseDoNotPanic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "shutdown.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.writeDB.Exec("CREATE TABLE probe (n INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := store.writer.do(ctx, func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT INTO probe (n) VALUES (?)", i)
				return err
			})
			// Either the write landed, or it was refused because shutdown had
			// begun. Anything else - and any panic - is a bug.
			if err != nil && !errors.Is(err, ErrWriterClosed) {
				t.Errorf("unexpected write error: %v", err)
			}
		}(i)
	}

	closed := make(chan struct{})
	go func() {
		<-start
		store.writer.close()
		close(closed)
	}()

	close(start)
	wg.Wait()
	<-closed
}

// After shutdown, a late write is refused rather than silently dropped, so the
// receiver can answer 503 and have the sender redeliver it.
func TestWriteAfterCloseIsRefused(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	store.writer.close()

	err = store.writer.do(context.Background(), func(context.Context, *sql.Tx) error { return nil })
	if !errors.Is(err, ErrWriterClosed) {
		t.Errorf("do after close = %v, want ErrWriterClosed", err)
	}
}
