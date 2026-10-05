#!/usr/bin/env bash
# Records the Ollama responses that the tests of internal/ollama replay.
#
# It talks to OLLAMA_HOST, or to http://127.0.0.1:11434 when OLLAMA_HOST is
# not set. It loads and unloads the two fixture models, so run it only while
# no benchmark runs. jq, when present, trims the large responses.
set -euo pipefail

readonly MLX_MODEL="qwen3.5:4b-mlx" # MLX engine
readonly GGUF_MODEL="lfm2.5:8b"     # GGUF engine
readonly MISSING_MODEL="token-rate-missing:0b"
readonly KEEP_ALIVE="10m0s"
readonly NUM_CTX=8192
OUT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/internal/ollama/testdata"
readonly OUT

# base_url prints OLLAMA_HOST as a URL with the default ports of Ollama:
# 11434 without a scheme, 80 for http://, and 443 for https://.
base_url() {
	local host="${OLLAMA_HOST:-127.0.0.1:11434}" scheme="http" port="11434"
	if [[ $host == *://* ]]; then
		scheme="${host%%://*}"
		host="${host#*://}"
		port="80"
		[[ $scheme == https ]] && port="443"
	fi
	host="${host%%/*}"
	[[ $host == *:* ]] || host="$host:$port"
	printf '%s://%s' "$scheme" "$host"
}

BASE="$(base_url)"
readonly BASE

# slug prints a model name in a form that fits in a file name.
slug() {
	local name="${1//:/-}"
	printf '%s' "${name//\//-}"
}

# trim runs jq -c with the arguments when jq exists, and passes the input
# through otherwise.
trim() {
	if command -v jq >/dev/null; then
		jq -c "$@"
	else
		cat
	fi
}

get() {
	curl -fsS "$BASE$1"
}

post() {
	curl -fsS -N -H 'Content-Type: application/json' -d "$2" "$BASE$1"
}

# unique_token prints a new token, so that the first chat of a model has a
# cold cache.
unique_token() {
	od -An -N4 -tx4 /dev/urandom | tr -d ' \n'
}

# load_body is the request of Client.Load: the shift value and the context
# size of the chats. Without them, the GGUF engine loads the model again for
# the first chat.
load_body() {
	printf '{"model":"%s","prompt":"","keep_alive":"%s","shift":false,"options":{"num_ctx":%d}}' \
		"$1" "$KEEP_ALIVE" "$NUM_CTX"
}

# unload_body is the request of Client.Unload.
unload_body() {
	printf '{"model":"%s","prompt":"","keep_alive":"0s","shift":false}' "$1"
}

# chat_body has the options of plan section 10.2, with num_predict 12.
chat_body() {
	printf '{"model":"%s","messages":[{"role":"user","content":"[run %s] Count from one to twenty in English words, separated by commas."}],"stream":true,"keep_alive":"%s","options":{"num_predict":12,"num_ctx":%d,"temperature":0,"seed":42},"think":false,"truncate":false,"shift":false}' \
		"$1" "$2" "$KEEP_ALIVE" "$NUM_CTX"
}

# capture_tags records the tags list with the two fixture models only.
# shellcheck disable=SC2016 # $a and $b are jq variables
capture_tags() {
	get /api/tags |
		trim --arg a "$MLX_MODEL" --arg b "$GGUF_MODEL" \
			'.models |= map(select(.name == $a or .name == $b))' >"$OUT/tags.json"
}

# capture_model records the load, the cold and the warm chat, and the show
# response of one model.
capture_model() {
	local model="$1" name token body
	name="$(slug "$model")"
	token="$(unique_token)"
	body="$(chat_body "$model" "$token")"
	post /api/generate "$(load_body "$model")" >"$OUT/generate-load-$name.ndjson"
	post /api/chat "$body" >"$OUT/chat-$name-cold.ndjson"
	post /api/chat "$body" >"$OUT/chat-$name-warm.ndjson"
	post /api/show "{\"model\":\"$model\"}" |
		trim '{details, capabilities, modified_at}' >"$OUT/show-$name.json"
}

unload() {
	post /api/generate "$(unload_body "$1")"
}

capture_unload() {
	unload "$1" >"$OUT/generate-unload-$(slug "$1").ndjson"
}

# capture_not_found records the error body of a chat with a missing model.
capture_not_found() {
	curl -sS -H 'Content-Type: application/json' \
		-d "{\"model\":\"$MISSING_MODEL\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}" \
		"$BASE/api/chat" >"$OUT/chat-not-found.json"
}

# cleanup unloads both models if the script stops early.
cleanup() {
	if [[ ${finished:-false} != true ]]; then
		unload "$MLX_MODEL" >/dev/null 2>&1 || true
		unload "$GGUF_MODEL" >/dev/null 2>&1 || true
	fi
}

main() {
	mkdir -p "$OUT"
	trap cleanup EXIT
	get /api/version >"$OUT/version.json"
	capture_tags
	capture_model "$MLX_MODEL"
	get /api/ps >"$OUT/ps.json" # only the MLX model is loaded here
	capture_model "$GGUF_MODEL"
	capture_not_found
	capture_unload "$MLX_MODEL"
	capture_unload "$GGUF_MODEL"
	finished=true
	echo "fixtures from $BASE written to $OUT"
}

main "$@"
