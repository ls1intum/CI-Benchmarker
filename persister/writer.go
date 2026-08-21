package persister

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
)

const (
	// writeQueueDepth bounds how many write requests may be waiting for the
	// single writer goroutine. A burst of callbacks larger than this applies
	// backpressure to the HTTP handlers rather than being dropped.
	writeQueueDepth = 8192
	// writeBatchSize bounds how many queued writes are committed in one
	// transaction. Callback storms arrive faster than SQLite can fsync one
	// transaction per row, so the writer opportunistically coalesces whatever
	// is already queued into a single commit.
	writeBatchSize = 128
)

// writeRequest is one unit of work handed to the writer goroutine.
type writeRequest struct {
	exec func(ctx context.Context, tx *sql.Tx) error
	done chan error
}

// writer serialises all writes to the SQLite database through a single
// goroutine.
//
// SQLite permits exactly one writer at a time. The previous code enforced that
// with db.SetMaxOpenConns(1), which turns concurrency into lock contention
// inside the driver: every concurrent handler blocks on the connection pool and
// the losers surface as SQLITE_BUSY. Serialising in Go instead lets the
// connection pool stay wide for readers, gives writes a predictable order, and
// makes batching possible.
//
// Every caller still gets its own error back, so a failed write is reported to
// the HTTP client rather than silently discarded.
type writer struct {
	db    *sql.DB
	queue chan *writeRequest
	wg    sync.WaitGroup

	// mu guards the enqueue/close transition. Senders hold it for reading while
	// they are on the queue send, close takes it for writing, so the channel can
	// never be closed with a send in flight.
	mu     sync.RWMutex
	closed bool

	closeOnce sync.Once
}

// ErrWriterClosed is returned when a write is submitted during shutdown. The
// callback receiver turns this into a 503, so the system under test redelivers
// and the measurement is recovered on the next start rather than lost.
var ErrWriterClosed = errors.New("persister: writer is shutting down")

func newWriter(db *sql.DB) *writer {
	w := &writer{
		db:    db,
		queue: make(chan *writeRequest, writeQueueDepth),
	}
	w.wg.Add(1)
	go w.loop()
	return w
}

// do enqueues exec and blocks until it has been committed or has failed.
// ctx bounds both the wait for queue space and the wait for the result.
func (w *writer) do(ctx context.Context, exec func(ctx context.Context, tx *sql.Tx) error) error {
	req := &writeRequest{exec: exec, done: make(chan error, 1)}

	// Held across the send so close cannot shut the channel underneath it. The
	// writer goroutine drains until the channel closes, so a full queue still
	// makes progress while this is held.
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return ErrWriterClosed
	}
	select {
	case w.queue <- req:
		w.mu.RUnlock()
	case <-ctx.Done():
		w.mu.RUnlock()
		return ctx.Err()
	}

	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// close stops the writer after draining everything already queued.
func (w *writer) close() {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.queue)
		w.mu.Unlock()

		w.wg.Wait()
	})
}

func (w *writer) loop() {
	defer w.wg.Done()

	batch := make([]*writeRequest, 0, writeBatchSize)

	for {
		req, ok := <-w.queue
		if !ok {
			return
		}

		batch = append(batch[:0], req)

		// Opportunistically absorb whatever else is already queued. This is
		// what lets a burst of several hundred callbacks land in a handful of
		// transactions instead of several hundred.
	drain:
		for len(batch) < writeBatchSize {
			select {
			case next, stillOpen := <-w.queue:
				if !stillOpen {
					w.flush(batch)
					return
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}

		w.flush(batch)
	}
}

func (w *writer) flush(batch []*writeRequest) {
	err := withRetry(func(ctx context.Context) error { return w.runBatch(ctx, batch) })
	if err == nil {
		for _, req := range batch {
			req.done <- nil
		}
		return
	}

	if len(batch) == 1 {
		batch[0].done <- err
		return
	}

	// One bad write must not fail the other 127. Re-run the batch one request
	// at a time so the failure is attributed to the request that caused it.
	slog.Warn("Batched write failed, retrying requests individually",
		slog.Int("batch_size", len(batch)), slog.Any("error", err))

	for _, req := range batch {
		single := [1]*writeRequest{req}
		req.done <- withRetry(func(ctx context.Context) error { return w.runBatch(ctx, single[:]) })
	}
}

func (w *writer) runBatch(ctx context.Context, batch []*writeRequest) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, req := range batch {
		if err := req.exec(ctx, tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}
