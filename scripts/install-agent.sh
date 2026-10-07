#!/usr/bin/env bash
# Render/load a launchd agent. Usage: install-agent.sh [--dry-run | --uninstall]
# DYNO_{LABEL,BIN,MODELS,RUNS,INTERVAL,CACHE,TAG,HOUR,MINUTE,DB} configure the job.
# Defaults: label dev.dyno.nightly; binary PATH/dyno or ~/go/bin/dyno;
# models qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b (native only); runs 5;
# interval 30s; cache miss; tag nightly; schedule 03:00;
# DB $XDG_DATA_HOME/dyno/bench.db (fallback ~/.local/share/dyno/bench.db).
# Backend ollama; compatible base http://127.0.0.1:8000/v1; usage required;
# token field max_tokens; discovery enabled; seed/temperature included.
# Compatible jobs require DYNO_MODELS explicitly; revision/version are optional.
# DYNO_{BACKEND,BASE_URL,API_KEY_ENV,SERVER_KIND,SERVER_VERSION,MODEL_REVISION,
# USAGE,TOKEN_LIMIT_FIELD,SKIP_MODEL_DISCOVERY,OMIT_SEED,OMIT_TEMPERATURE}
# configure compatible endpoints. Boolean settings are 0 or 1.
# API_KEY_ENV is optional: the binary otherwise reads OPENAI_API_KEY if present.
# Secrets are never embedded in the plist. Provision credentials in launchd's
# environment separately; an explicitly named empty key fails at job startup.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template="$root/deploy/launchd/dyno.plist.tmpl"
label="${DYNO_LABEL:-dev.dyno.nightly}"
binary="${DYNO_BIN:-$(command -v dyno || echo "$HOME/go/bin/dyno")}"
backend="${DYNO_BACKEND:-ollama}"
models="${DYNO_MODELS:-}"
[[ "$backend" != ollama || -n "$models" ]] || models="qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b"
runs="${DYNO_RUNS:-5}"
interval="${DYNO_INTERVAL:-30s}"
cache="${DYNO_CACHE:-miss}"
tag="${DYNO_TAG:-nightly}"
base_url="${DYNO_BASE_URL:-}"
api_key_env="${DYNO_API_KEY_ENV:-}"
server_kind="${DYNO_SERVER_KIND:-}"
server_version="${DYNO_SERVER_VERSION:-}"
revision="${DYNO_MODEL_REVISION:-}"
usage="${DYNO_USAGE:-}"
token_field="${DYNO_TOKEN_LIMIT_FIELD:-}"
skip_discovery="${DYNO_SKIP_MODEL_DISCOVERY:-0}"
omit_seed="${DYNO_OMIT_SEED:-0}"
omit_temperature="${DYNO_OMIT_TEMPERATURE:-0}"
hour="${DYNO_HOUR:-3}"
minute="${DYNO_MINUTE:-0}"
db="${DYNO_DB:-${XDG_DATA_HOME:-$HOME/.local/share}/dyno/bench.db}"
log_dir="${XDG_STATE_HOME:-$HOME/.local/state}/dyno"
plist="$HOME/Library/LaunchAgents/$label.plist"
domain="gui/$(id -u)"
die() { echo "install-agent: $*" >&2; exit 1; }
xml_escape() { printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }

# Shell/system-AWK preflight; no Python dependency or network calls. Pin macOS
# /usr/bin/awk: label validation requires byte semantics under LC_ALL=C.
# The Go endpoint parser is authoritative at startup.
valid_ipv6() {
    local ip="$1" part tail count=0
    local groups=() octets=()
    if [[ "$ip" == *.* ]]; then
        tail="${ip##*:}"
        [[ "$tail" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
        IFS=. read -r -a octets <<< "$tail"
        for part in "${octets[@]}"; do
            [[ ${#part} -le 3 && ( ${#part} == 1 || "$part" != 0* ) ]] || return 1
            (( 10#$part <= 255 )) || return 1
        done
        ip="${ip%:*}:0:0"
    fi
    [[ "$ip" =~ ^[[:xdigit:]:]+$ && "$ip" != *:::* ]] || return 1
    if [[ "$ip" == *::* ]]; then
        [[ "${ip#*::}" != *::* ]] || return 1
    else
        [[ "$ip" != :* && "$ip" != *: ]] || return 1
    fi
    IFS=: read -r -a groups <<< "$ip"
    for part in "${groups[@]}"; do
        [[ -z "$part" ]] && continue
        [[ ${#part} -le 4 ]] || return 1
        count=$((count+1))
    done
    if [[ "$ip" == *::* ]]; then (( count < 8 )); else (( count == 8 )); fi
}
validate_url() {
    local raw="$1" authority host path escaped port=""
    [[ ! "$raw" =~ [[:space:][:cntrl:]] && "$raw" =~ ^[hH][tT][tT][pP][sS]?://([^/?#]+)(/[^?#]*)?$ ]] || die "DYNO_BASE_URL must be a credential-free absolute http(s) API base"
    authority="${BASH_REMATCH[1]}"
    path="${BASH_REMATCH[2]}"
    escaped="$path"
    while [[ "$escaped" == *%* ]]; do
        escaped="${escaped#*%}"
        [[ "$escaped" =~ ^[[:xdigit:]]{2} ]] || die "DYNO_BASE_URL has an invalid path escape"
        escaped="${escaped:2}"
    done
    [[ ! "$path" =~ %[01][[:xdigit:]]|%7[fF]|%[cC]2%[89][[:xdigit:]] ]] || die "DYNO_BASE_URL path must not contain controls"
    if [[ "$authority" == \[* ]]; then
        [[ "$authority" =~ ^\[([^]]+)\](:([0-9]+))?$ ]] || die "DYNO_BASE_URL has an invalid host or port"
        host="${BASH_REMATCH[1]}"; port="${BASH_REMATCH[3]}"
        if [[ "$host" == *%* ]]; then
            [[ "$host" =~ %25[A-Za-z0-9_.-]+$ ]] || die "DYNO_BASE_URL has an invalid IPv6 zone"
            host="${host%%%25*}"
        fi
        valid_ipv6 "$host" || die "DYNO_BASE_URL has an invalid IPv6 host"
    else
        [[ "$authority" =~ ^([[:alnum:]._-]+)(:([0-9]+))?$ ]] || die "DYNO_BASE_URL has an invalid host or port"
        port="${BASH_REMATCH[3]}"
    fi
    if [[ -n "$port" ]]; then
        while [[ ${#port} -gt 1 && "$port" == 0* ]]; do port="${port#0}"; done
        if [[ ${#port} -gt 5 ]] || ! (( 10#$port >= 1 && 10#$port <= 65535 )); then die "DYNO_BASE_URL port must be 1-65535"; fi
    fi
}
validate_interval() {
    LC_ALL=C DYNO_CHECK_INTERVAL="$interval" /usr/bin/awk -f "$root/scripts/validate-interval.awk" || die "DYNO_INTERVAL must be a nonnegative Go duration within range"
}
label_count() {
    LC_ALL=C DYNO_CHECK_TEXT="$1" /usr/bin/awk -f "$root/scripts/validate-label.awk" "$root/scripts/printable-ranges.txt"
}
check() {
    local ids=() modes=() id mode seen=, model_count=0
    [[ -n "${models//[[:space:],]/}" ]] || die "DYNO_MODELS must contain at least one model"
    IFS=, read -r -a ids <<< "$models"
    for id in "${ids[@]}"; do [[ -z "${id//[[:space:]]/}" ]] || model_count=$((model_count+1)); done
    (( model_count > 0 )) || die "DYNO_MODELS must contain at least one model"
    [[ -z "$revision" || "$model_count" == 1 ]] || die "DYNO_MODEL_REVISION requires exactly one model"
    validate_interval
    [[ -n "${cache//[[:space:],]/}" ]] || die "DYNO_CACHE must contain at least one mode"
    IFS=, read -r -a modes <<< "$cache"
    for mode in "${modes[@]}"; do
        mode="${mode#"${mode%%[![:space:]]*}"}"; mode="${mode%"${mode##*[![:space:]]}"}"
        [[ -n "$mode" ]] || continue
        case "$mode" in miss|continue|prefix) ;; *) die "DYNO_CACHE must use miss, continue or prefix" ;; esac
        [[ "$seen" != *",$mode,"* ]] || die "DYNO_CACHE contains duplicate modes"
        seen+="$mode,"
    done
    local version_count revision_count
    label_count "$tag" >/dev/null || die "tag must be printable UTF-8 text"
    version_count="$(label_count "$server_version")" || die "server version must be printable UTF-8 text"
    revision_count="$(label_count "$revision")" || die "revision must be printable UTF-8 text"
    [[ "$version_count" -le 64 ]] || die "server version must be at most 64 characters"
    if [[ "$revision" == sha256:* ]]; then
        [[ "$revision" =~ ^sha256:[0-9a-fA-F]{64}$ ]] || die "revision must use sha256:<64 hex>"
    else
        [[ "$revision_count" -le 64 ]] || die "revision must be at most 64 characters"
    fi
    [[ "$revision" != ' '* && "$revision" != *' ' && "$server_version" != ' '* && "$server_version" != *' ' ]] || die "revision and server version must not contain padding"
    [[ ! "$models" =~ [[:cntrl:]] ]] || die "DYNO_MODELS must not contain controls"
    [[ "$backend" == ollama || "$backend" == openai ]] || die "DYNO_BACKEND must be ollama or openai"
    if [[ "$backend" == ollama ]]; then
        [[ -z "$base_url$api_key_env$server_kind$server_version$revision$usage$token_field" && "$skip_discovery$omit_seed$omit_temperature" == 000 ]] || die "compatible endpoint settings require DYNO_BACKEND=openai"
    else
        [[ -n "${models//[[:space:]]/}" ]] || die "DYNO_BACKEND=openai requires explicit DYNO_MODELS served IDs"
        [[ "$cache" == miss ]] || die "external endpoints currently support DYNO_CACHE=miss only"
        [[ -z "$api_key_env" || "$api_key_env" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "DYNO_API_KEY_ENV must be a variable name"
        [[ -z "$server_kind" || "$server_kind" == unknown || "$server_kind" == vllm ]] || die "DYNO_SERVER_KIND must be unknown or vllm"
        [[ -z "$usage" || "$usage" == required || "$usage" == off ]] || die "DYNO_USAGE must be required or off"
        [[ -z "$token_field" || "$token_field" == max_tokens || "$token_field" == max_completion_tokens ]] || die "invalid DYNO_TOKEN_LIMIT_FIELD"
        [[ "$skip_discovery" =~ ^[01]$ && "$omit_seed" =~ ^[01]$ && "$omit_temperature" =~ ^[01]$ ]] || die "boolean compatible settings must be 0 or 1"
        validate_url "${base_url:-http://127.0.0.1:8000/v1}"
    fi
    [[ "$binary" = /* ]] || die "DYNO_BIN must be an absolute path"
    [[ "$db" = /* ]] || die "DYNO_DB must be an absolute path"
    [[ "$hour" =~ ^([01]?[0-9]|2[0-3])$ ]] || die "DYNO_HOUR must be 0-23"
    [[ "$minute" =~ ^[0-5]?[0-9]$ ]] || die "DYNO_MINUTE must be 0-59"
    [[ "$runs" =~ ^[1-9][0-9]*$ ]] || die "DYNO_RUNS must be 1 or more"
}
render() (
    local provider_args="" pair key value
    if [[ "$backend" == openai ]]; then
        provider_args="<string>--backend</string><string>openai</string><string>--base-url</string><string>$(xml_escape "${base_url:-http://127.0.0.1:8000/v1}")</string>"
        for pair in "api-key-env=$api_key_env" "server=$server_kind" "server-version=$server_version" "model-revision=$revision" "usage=$usage" "token-limit-field=$token_field"; do
            key="${pair%%=*}"; value="${pair#*=}"
            [[ -z "$value" ]] || provider_args+="<string>--$key</string><string>$(xml_escape "$value")</string>"
        done
        [[ "$skip_discovery" != 1 ]] || provider_args+="<string>--skip-model-discovery</string>"
        [[ "$omit_seed" != 1 ]] || provider_args+="<string>--omit-seed</string>"
        [[ "$omit_temperature" != 1 ]] || provider_args+="<string>--omit-temperature</string>"
    fi
    export DYNO_RENDER_PROVIDER_ARGS="$provider_args"
    for pair in "LABEL=$label" "BINARY=$binary" "MODELS=$models" "RUNS=$runs" "INTERVAL=$interval" "CACHE=$cache" "TAG=$tag" "DB=$db" "HOUR=$hour" "MINUTE=$minute" "LOG_DIR=$log_dir"; do
        key="${pair%%=*}"; value="$(xml_escape "${pair#*=}")"
        export "DYNO_RENDER_$key=$value"
    done
    # Scan only the template; inserted values are literal and never rescanned.
    /usr/bin/awk '{ line=$0; out=""; while (match(line, /\{\{[A-Z_]+\}\}/)) {
        token=substr(line,RSTART+2,RLENGTH-4); out=out substr(line,1,RSTART-1) ENVIRON["DYNO_RENDER_" token]; line=substr(line,RSTART+RLENGTH)
    } print out line }' "$template"
)
uninstall() {
    launchctl bootout "$domain/$label" 2>/dev/null || true
    rm -f "$plist"
    echo "removed $label"
}
install() {
    [[ -x "$binary" ]] || die "no executable at $binary; run 'just install' first"
    mkdir -p "$log_dir" "$(dirname "$plist")"
    render > "$plist.tmp"
    plutil -lint "$plist.tmp" > /dev/null || die "the rendered plist is not valid"
    mv "$plist.tmp" "$plist"
    launchctl bootout "$domain/$label" 2>/dev/null || true
    launchctl bootstrap "$domain" "$plist"
    printf 'installed %s: every day at %02d:%02d, logs in %s\n' "$label" "$hour" "$minute" "$log_dir"
    echo "start it now with: launchctl kickstart $domain/$label"
}
# Labels name a launchd job and a local filename, never a path.
[[ "$label" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "DYNO_LABEL must be a simple launchd label"
# Uninstall must work even if unrelated endpoint/job settings have gone stale.
[[ $# -le 1 ]] || die "use only one of --uninstall or --dry-run"
case "${1:-}" in
    --uninstall) uninstall; exit 0 ;;
    --dry-run|"") ;;
    *) die "unknown argument $1; use --dry-run or --uninstall" ;;
esac
check
hour=$((10#$hour)); minute=$((10#$minute))
if [[ "${1:-}" == --dry-run ]]; then render; else install; fi
