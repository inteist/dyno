CREATE TABLE sessions (
    id             TEXT    PRIMARY KEY,
    started_at     TEXT    NOT NULL,
    finished_at    TEXT,
    status         TEXT    NOT NULL CHECK (status IN ('running', 'completed', 'aborted', 'failed')),
    tool_version   TEXT    NOT NULL,
    ollama_version TEXT    NOT NULL,
    host           TEXT    NOT NULL,
    power_source   TEXT    NOT NULL,
    tag            TEXT    NOT NULL DEFAULT '',
    prompt_name    TEXT    NOT NULL,
    prompt_hash    TEXT    NOT NULL,
    workload_hash  TEXT    NOT NULL,
    num_predict    INTEGER NOT NULL,
    num_ctx        INTEGER NOT NULL,
    temperature    REAL    NOT NULL,
    seed           INTEGER NOT NULL,
    run_order      TEXT    NOT NULL,
    interval_ns    INTEGER NOT NULL,
    runs           INTEGER NOT NULL,
    cache_modes    TEXT    NOT NULL   -- comma-separated, in run order
);

CREATE TABLE models (
    digest         TEXT    PRIMARY KEY,
    format         TEXT    NOT NULL,
    family         TEXT    NOT NULL,
    parameter_size TEXT    NOT NULL,
    quantization   TEXT    NOT NULL,
    size_bytes     INTEGER NOT NULL,
    first_seen_at  TEXT    NOT NULL
);

CREATE TABLE exchanges (
    id             INTEGER PRIMARY KEY,
    session_id     TEXT    NOT NULL REFERENCES sessions (id),
    model_name     TEXT    NOT NULL,
    model_digest   TEXT    NOT NULL REFERENCES models (digest),
    cache_mode     TEXT    NOT NULL CHECK (cache_mode IN ('miss', 'continue', 'prefix')),
    run_number     INTEGER NOT NULL,
    kind           TEXT    NOT NULL CHECK (kind IN ('warmup', 'primer', 'run')),
    started_at     TEXT    NOT NULL,
    prompt_tokens  INTEGER,
    cached_tokens  INTEGER,
    prompt_ns      INTEGER,
    eval_tokens    INTEGER,
    eval_ns        INTEGER,
    load_ns        INTEGER,
    total_ns       INTEGER,
    ttft_ns        INTEGER,
    wall_ns        INTEGER,
    done_reason    TEXT,
    error          TEXT              -- NULL when the request succeeded
);

CREATE INDEX exchanges_report  ON exchanges (kind, model_name, cache_mode);
CREATE INDEX exchanges_session ON exchanges (session_id);
CREATE INDEX sessions_started  ON sessions (started_at);
