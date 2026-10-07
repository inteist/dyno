-- Existing sessions used Ollama's native measurements. Missing endpoint and
-- effective configuration are unknown; do not reconstruct them from host.
ALTER TABLE sessions ADD COLUMN backend TEXT NOT NULL DEFAULT 'ollama';
ALTER TABLE sessions ADD COLUMN server_kind TEXT NOT NULL DEFAULT 'ollama';
ALTER TABLE sessions ADD COLUMN version_source TEXT;
ALTER TABLE sessions ADD COLUMN endpoint TEXT;
ALTER TABLE sessions ADD COLUMN measurement_protocol TEXT NOT NULL DEFAULT 'ollama-server-v1';
ALTER TABLE sessions ADD COLUMN context_policy TEXT NOT NULL DEFAULT 'request';
ALTER TABLE sessions ADD COLUMN config_json TEXT;

UPDATE sessions SET version_source = 'server' WHERE server_version <> '';
