CREATE TABLE scheduled_job (
  id   string PRIMARY KEY,
  time text    NOT NULL
);

CREATE TABLE job_results (
  id   string PRIMARY KEY,
  time text    NOT NULL
);