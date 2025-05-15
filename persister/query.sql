-- name: StoreScheduledJobWithMetadata :one
INSERT INTO scheduled_job (
  id, creation_time, executor, metadata
) VALUES (
  ?, ?, ?, ?
)
RETURNING *;

-- name: StoreScheduledJob :one
INSERT INTO scheduled_job (
  id, creation_time, executor
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: StoreJobResult :one
INSERT INTO job_results (
  id, start_time, end_time
) VALUES (
  ?, ?, ?
)
RETURNING *;

-- name: GetQueueLatenciesInRange :many
SELECT
    CAST((strftime('%s', r.start_time) - strftime('%s', s.creation_time)) AS INTEGER) AS queue_latency
FROM
    scheduled_job s
        INNER JOIN
    job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.start_time) <= datetime(:to) OR :to IS NULL)
ORDER BY
    queue_latency DESC;

-- name: GetBuildTimesInRange :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', r.start_time)) AS INTEGER) AS build_time
FROM
    job_results r
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
ORDER BY
    build_time DESC;

-- name: GetQueueLatencySummaryInRange :many
SELECT
    CAST((strftime('%s', r.start_time) - strftime('%s', s.creation_time)) AS INTEGER) AS latency
FROM
    scheduled_job s
        INNER JOIN job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.start_time) <= datetime(:to) OR :to IS NULL)
ORDER BY
    latency ASC;

-- name: GetBuildTimeSummaryInRange :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', r.start_time)) AS INTEGER) AS build_time
FROM
    job_results r
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
ORDER BY
    build_time ASC;