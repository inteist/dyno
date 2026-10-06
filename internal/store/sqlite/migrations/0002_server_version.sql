-- The column holds the version of the LLM server, which need not be Ollama.
ALTER TABLE sessions RENAME COLUMN ollama_version TO server_version;
