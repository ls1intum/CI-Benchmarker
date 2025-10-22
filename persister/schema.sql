CREATE TABLE IF NOT EXISTS scheduled_job
(
    id            uuid      PRIMARY KEY,
    creation_time timestamp NOT NULL,
    executor      text      NOT NULL,
    metadata      jsonb,
    commit_hash   text      DEFAULT NULL
);

CREATE TABLE IF NOT EXISTS job_results
(
    id         uuid PRIMARY KEY,
    start_time timestamp NULL,
    end_time   timestamp NULL
);

CREATE INDEX IF NOT EXISTS idx_scheduled_job_commit   ON scheduled_job(commit_hash);
CREATE INDEX IF NOT EXISTS idx_scheduled_job_executor ON scheduled_job(executor);
CREATE INDEX IF NOT EXISTS idx_job_results_start      ON job_results(start_time);
CREATE INDEX IF NOT EXISTS idx_job_results_end        ON job_results(end_time);
