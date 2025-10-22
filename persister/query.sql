-- name: StoreScheduledJobWithMetadata :one
INSERT INTO scheduled_job (
  id, creation_time, executor, metadata, commit_hash
) VALUES (
  ?, ?, ?, ?, ?
)
RETURNING *;

-- name: StoreScheduledJob :one
INSERT INTO scheduled_job (
  id, creation_time, executor, commit_hash
) VALUES (
  ?, ?, ?, ?
)
RETURNING *;

-- name: UpsertJobStartTime :one
INSERT INTO job_results (id, start_time)
VALUES (?, ?)
ON CONFLICT (id) DO UPDATE
  SET start_time = EXCLUDED.start_time
RETURNING *;

-- name: UpsertJobEndTime :one
INSERT INTO job_results (id, end_time)
VALUES (?, ?)
ON CONFLICT (id) DO UPDATE
  SET end_time = EXCLUDED.end_time
RETURNING *;

-- name: UpsertJobTimes :one
INSERT INTO job_results (id, start_time, end_time)
  VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE
  SET start_time = EXCLUDED.start_time,
  end_time = EXCLUDED.end_time
RETURNING *;

-- name: GetQueueLatenciesInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.start_time) - strftime('%s', s.creation_time)) AS INTEGER) AS queue_latency
FROM
    scheduled_job s
        INNER JOIN job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.start_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    queue_latency DESC;

-- name: GetBuildTimesInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', r.start_time)) AS INTEGER) AS build_time
FROM
    job_results r
        INNER JOIN scheduled_job s ON r.id = s.id
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    build_time DESC;

-- name: GetTotalLatenciesInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', s.creation_time)) AS INTEGER) AS total_latency
FROM
    scheduled_job s
        INNER JOIN job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    total_latency DESC;

-- name: GetQueueLatencySummaryInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.start_time) - strftime('%s', s.creation_time)) AS INTEGER) AS latency
FROM
    scheduled_job s
        INNER JOIN job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.start_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    latency ASC;

-- name: GetBuildTimeSummaryInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', r.start_time)) AS INTEGER) AS build_time
FROM
    job_results r
        INNER JOIN scheduled_job s ON r.id = s.id
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    build_time ASC;

-- name: GetTotalLatenciesSummaryInRangeByCommitAndExecutor :many
SELECT
    CAST((strftime('%s', r.end_time) - strftime('%s', s.creation_time)) AS INTEGER) AS total_latency
FROM
    scheduled_job s
        INNER JOIN job_results r ON s.id = r.id
WHERE
    r.start_time IS NOT NULL
  AND r.end_time IS NOT NULL
  AND (datetime(r.start_time) >= datetime(:from) OR :from IS NULL)
  AND (datetime(r.end_time) <= datetime(:to) OR :to IS NULL)
  AND (s.commit_hash = :commit_hash OR :commit_hash IS NULL)
  AND (s.executor = :executor OR :executor IS NULL)
ORDER BY
    total_latency ASC;