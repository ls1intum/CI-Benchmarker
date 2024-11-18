-- name: StoreScheduledJobWithMetadata :one
INSERT INTO scheduled_job (
  id, time, executor, metadata
) VALUES (
  ?, ?, ?, ?
)
RETURNING *;

-- name: StoreScheduledJob :one
INSERT INTO scheduled_job (
  id, time, executor
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: StoreJobResult :one
INSERT INTO job_results (
  id, time
) VALUES (
  ?, ?
)
RETURNING *;