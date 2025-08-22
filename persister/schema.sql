CREATE TABLE scheduled_job
(
    id            uuid      PRIMARY KEY,
    creation_time timestamp NOT NULL,
    executor      text      NOT NULL,
    metadata      jsonb,
    commit_hash   text      DEFAULT NULL
);

CREATE TABLE job_results
(
    id         uuid PRIMARY KEY,
    start_time timestamp NULL,
    end_time   timestamp NULL
);