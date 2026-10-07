#!/usr/bin/env bash
# Explicitly opt-in smoke benchmark. Never starts/stops the endpoint or resets it.
# DYNO_LIVE_OPENAI=1 OPENAI_BASE_URL=http://host:8000/v1 DYNO_MODEL=served-id \
#   scripts/smoke-compatible.sh
# Optional: DYNO_BIN, DYNO_API_KEY_ENV, DYNO_SERVER_KIND, DYNO_SERVER_VERSION,
# DYNO_MODEL_REVISION, DYNO_USAGE. Credentials are read from the named environment
# variable by Dyno, never passed as arguments or written by this script.
set -euo pipefail
[[ "${DYNO_LIVE_OPENAI:-}" == 1 ]] || { echo 'Set DYNO_LIVE_OPENAI=1 to explicitly allow live requests.' >&2; exit 2; }
: "${OPENAI_BASE_URL:?set the full API base URL}"
: "${DYNO_MODEL:?set a served model ID}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
binary="${DYNO_BIN:-dyno}"
args=(--backend openai --base-url "$OPENAI_BASE_URL" --models "$DYNO_MODEL"
    --server "${DYNO_SERVER_KIND:-unknown}"
    --usage "${DYNO_USAGE:-required}" --runs 1 --interval 0s --num-predict 32
    --request-timeout 60s --cache miss --plain --db "$work/smoke.db")
[[ -z "${DYNO_API_KEY_ENV:-}" ]] || args+=(--api-key-env "$DYNO_API_KEY_ENV")
[[ -z "${DYNO_SERVER_VERSION:-}" ]] || args+=(--server-version "$DYNO_SERVER_VERSION")
[[ -z "${DYNO_MODEL_REVISION:-}" ]] || args+=(--model-revision "$DYNO_MODEL_REVISION")
"$binary" run "${args[@]}"
"$binary" report --db "$work/smoke.db" --backend openai --plain | tee "$work/report.txt"
if ! grep -Eq '^dyno report.*[[:space:]][1-9][0-9,]* valid([[:space:]]|$)' "$work/report.txt"; then
    echo 'Smoke failed: no valid benchmark runs (inspect exclusions above).' >&2
    exit 1
fi
printf '%s\n' 'Transport smoke completed with valid runs. This does not validate cache reuse, reasoning policy, or GPU timings.'
