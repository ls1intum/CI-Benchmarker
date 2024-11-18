CREATE TABLE scheduled_job (
  id        uuid  PRIMARY KEY,
  time      text    NOT NULL,
  executor  text    NOT NULL, 
  metadata  jsonb
);

CREATE TABLE job_results (
  id        uuid  PRIMARY KEY,
  time      text    NOT NULL
);