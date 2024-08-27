-- name: StoreScheduledJob :one
INSERT INTO scheduled_job (
  id, time
) VALUES (
  ?, ?
)
RETURNING *;

-- name: StoreJobResult :one
INSERT INTO job_results (
  id, time
) VALUES (
  ?, ?
)
RETURNING *;