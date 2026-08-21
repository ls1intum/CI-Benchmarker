package executor

import (
	"context"

	"github.com/google/uuid"
	"github.com/hades-scheduler/hades/shared/payload"
)

// Executor is the extension point for a job-execution variant. Adding a
// variant (a raw batch/v1 Job runner, a raw `docker run` runner, ...) means
// implementing this interface and registering a route; nothing in the
// measurement path needs to know which variant it is driving.
//
// Implementations must be safe for concurrent use: the benchmark controller
// submits from many goroutines at once.
type Executor interface {
	// Execute submits one job and returns the id the system under test
	// acknowledged. It must do exactly one thing that takes time - the
	// submission itself - because the caller times it from immediately before
	// the call to immediately after it returns.
	//
	// ctx bounds the submission. Returning a non-nil error is expected and is
	// recorded as a failed submission rather than being dropped.
	Execute(ctx context.Context, job payload.RESTPayload) (uuid.UUID, error)

	// Name is the human-readable executor name, kept for the legacy tables.
	Name() string

	// Variant is the stable slug used to group results in the exported data
	// (hades-docker, hades-k8s, jenkins, ...).
	Variant() string

	// TargetHost identifies the system under test this executor points at, so
	// exported rows record which machine produced them.
	TargetHost() string
}
