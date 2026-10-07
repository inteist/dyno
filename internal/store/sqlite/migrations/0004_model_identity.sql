-- Keep foreign_keys enabled. Replacement children reference the replacement
-- parent; drop old children first and rename the new parent before its child.
CREATE TABLE models_new (
    model_key      TEXT    NOT NULL PRIMARY KEY,
    digest         TEXT,
    identity_source TEXT,
    revision       TEXT,
    format         TEXT,
    family         TEXT,
    parameter_size TEXT,
    quantization   TEXT,
    size_bytes     INTEGER,
    first_seen_at  TEXT    NOT NULL
);

INSERT INTO models_new (
    model_key, digest, identity_source, revision,
    format, family, parameter_size, quantization, size_bytes, first_seen_at
)
-- An empty legacy digest remains a valid referenced key, not a known checksum.
SELECT digest, NULLIF(digest, ''), 'digest', NULL,
    format, family, parameter_size, quantization, size_bytes, first_seen_at
FROM models;

CREATE TABLE exchanges_new (
    id             INTEGER PRIMARY KEY,
    session_id     TEXT    NOT NULL REFERENCES sessions (id),
    model_name     TEXT    NOT NULL,
    model_key      TEXT    NOT NULL REFERENCES models_new (model_key),
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
    error          TEXT
);

INSERT INTO exchanges_new (
    id, session_id, model_name, model_key, cache_mode, run_number, kind, started_at,
    prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns,
    ttft_ns, wall_ns, done_reason, error
)
SELECT id, session_id, model_name, model_digest, cache_mode, run_number, kind, started_at,
    prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns,
    ttft_ns, wall_ns, done_reason, error
FROM exchanges;

DROP TABLE exchanges;
DROP TABLE models;
ALTER TABLE models_new RENAME TO models;
ALTER TABLE exchanges_new RENAME TO exchanges;

CREATE INDEX exchanges_report  ON exchanges (kind, model_name, cache_mode);
CREATE INDEX exchanges_session ON exchanges (session_id);
