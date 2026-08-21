-- Measurement core.
--
-- Every timestamp in these tables is epoch NANOSECONDS stored as INTEGER, taken
-- from the benchmarker's own clock unless the column name says "reported_"
-- (in which case it came from the system under test and is provenance only,
-- never a measurement input). Storing integers removes strftime('%s') second
-- flooring, RFC3339 fractional-second truncation and text-parsing ambiguity in
-- one step: every derived duration is an exact int64 subtraction.

-- One row per benchmark invocation. Everything needed to know what was run.
CREATE TABLE benchmark_run
(
    run_id              TEXT    PRIMARY KEY,
    variant             TEXT    NOT NULL, -- hades-docker | hades-k8s | jenkins | ...
    target_host         TEXT    NOT NULL, -- which SUT VM this run was pointed at
    workload_id         TEXT    NOT NULL, -- identifies the job payload that was submitted
    config_fingerprint  TEXT    NOT NULL, -- sha256 over variant+host+workload+pacing+payload
    commit_hash         TEXT,
    priority            INTEGER NOT NULL,
    requested_jobs      INTEGER NOT NULL,
    concurrency         INTEGER NOT NULL, -- in-flight submission cap actually used
    rate_per_second     REAL    NOT NULL, -- offered submission rate; 0 means unpaced
    started_at_ns       INTEGER NOT NULL,
    finished_at_ns      INTEGER,
    submitted_jobs      INTEGER NOT NULL DEFAULT 0,
    failed_jobs         INTEGER NOT NULL DEFAULT 0,
    benchmarker_version TEXT,
    notes               TEXT
);

-- One row per submission ATTEMPT, including the ones that failed. A run of N
-- always yields N rows here, so denominators are recoverable and a failed
-- submission can never silently shrink the sample.
CREATE TABLE job_submission
(
    submission_id      TEXT    PRIMARY KEY, -- benchmarker-side id, always present
    run_id             TEXT    NOT NULL REFERENCES benchmark_run (run_id) ON DELETE CASCADE,
    seq                INTEGER NOT NULL,    -- 0-based submission index within the run
    job_id             TEXT,                -- id the SUT acknowledged; NULL if submission failed
    variant            TEXT    NOT NULL,
    target_host        TEXT    NOT NULL,
    workload_id        TEXT    NOT NULL,
    config_fingerprint TEXT    NOT NULL,
    priority           INTEGER NOT NULL,
    commit_hash        TEXT,
    submit_time_ns     INTEGER NOT NULL,    -- taken IMMEDIATELY BEFORE Execute()
    submit_ack_time_ns INTEGER,             -- taken immediately AFTER Execute() returns
    submit_status      TEXT    NOT NULL,    -- accepted | failed
    submit_error       TEXT,
    UNIQUE (run_id, seq)
);

CREATE UNIQUE INDEX idx_job_submission_job_id ON job_submission (job_id) WHERE job_id IS NOT NULL;
CREATE INDEX idx_job_submission_run ON job_submission (run_id, seq);

-- The authoritative terminal observation for a job: the first terminal status
-- callback the SUT pushed back, stamped with the benchmarker's own clock on
-- arrival. PRIMARY KEY (job_id) plus INSERT .. ON CONFLICT makes the receiver
-- idempotent: a retried callback bumps delivery_count and updates
-- last_received_time_ns but can never move received_time_ns.
--
-- There is deliberately NO foreign key to job_submission. A callback must never
-- be rejected: it can legitimately arrive before its own submission row has been
-- committed, and an unmatched callback is itself a finding worth keeping.
-- Matching happens with a LEFT JOIN at export time.
CREATE TABLE job_callback
(
    job_id                 TEXT    PRIMARY KEY,
    received_time_ns       INTEGER NOT NULL, -- benchmarker clock, FIRST terminal callback
    last_received_time_ns  INTEGER NOT NULL,
    source                 TEXT    NOT NULL, -- hades | jenkins | unknown
    status                 TEXT    NOT NULL, -- succeeded | failed | stopped
    raw_status             TEXT,             -- verbatim status string from the SUT
    event                  TEXT,             -- Hades event discriminator, e.g. job.completed
    reason                 TEXT,
    -- SUT-reported times. Hades derives these from NATS server timestamps of
    -- the underlying status events rather than from a clock read at send time,
    -- so they are immune to dispatcher lag and to redelivery, which makes them
    -- a usable cross-check. They are still never the measurement: the two hosts
    -- are not clock-synchronised.
    reported_queued_time_ns INTEGER,
    reported_start_time_ns  INTEGER,
    reported_end_time_ns    INTEGER,
    reported_duration_ms    INTEGER,
    -- delivery_attempt is the SUT's own redelivery counter (Hades sends
    -- NumDelivered as "attempt"). delivery_count is how many terminal
    -- callbacks THIS receiver saw. They disagree when a delivery was lost in
    -- transit rather than rejected, which is worth being able to see.
    delivery_attempt       INTEGER,
    delivery_count         INTEGER NOT NULL DEFAULT 1,
    raw_payload            TEXT
);

CREATE INDEX idx_job_callback_received ON job_callback (received_time_ns);

-- Append-only audit log of every callback POST that reached the receiver,
-- terminal or not, duplicate or not, parseable or not. Never read by the
-- measurement path; it exists so a suspicious run can be reconstructed.
CREATE TABLE job_event
(
    event_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id           TEXT,
    received_time_ns INTEGER NOT NULL,
    source           TEXT    NOT NULL,
    phase            TEXT,
    status           TEXT,
    terminal         INTEGER NOT NULL, -- 1 if the status is a terminal one
    accepted         INTEGER NOT NULL, -- 1 if this POST became the authoritative row
    delivery_id      TEXT,             -- X-Hades-Delivery, unique per delivery attempt
    delivery_attempt INTEGER,          -- X-Hades-Attempt / payload "attempt"
    remote_addr      TEXT,
    raw_payload      TEXT
);

CREATE INDEX idx_job_event_job ON job_event (job_id, received_time_ns);
