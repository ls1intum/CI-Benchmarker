package executor

import (
	"github.com/google/uuid"
)

// Specify an executor interface
// We assume that each executor returns a UUID which can be used to track the job
type Executor interface {
	// For now we only assume a single method to execute the job - This may be expanded in the future to actually pass in the job to execute
	Execute() (uuid.UUID, error)

	// Add a method to get the name of the executor
	Name() string
}
