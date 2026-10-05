#!/usr/bin/env bash
# Fills deploy/launchd/token-rate.plist.tmpl and loads it as a launchd agent.
#
# Usage: scripts/install-agent.sh [--dry-run | --uninstall]
#
# Settings come from the environment. The defaults are in brackets:
#   TOKEN_RATE_LABEL     agent label            [dev.token-rate.nightly]
#   TOKEN_RATE_BIN       absolute binary path   [the token-rate on PATH, else ~/go/bin/token-rate]
#   TOKEN_RATE_MODELS    models                 [qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b]
#   TOKEN_RATE_RUNS      runs for each mode     [5]
#   TOKEN_RATE_INTERVAL  pause between runs     [30s]
#   TOKEN_RATE_CACHE     cache modes            [miss]
#   TOKEN_RATE_TAG       session tag            [nightly]
#   TOKEN_RATE_HOUR      start hour, 0-23       [3]
#   TOKEN_RATE_MINUTE    start minute, 0-59     [0]
#   TOKEN_RATE_DB        database path          [$XDG_DATA_HOME or ~/.local/share]/token-rate/bench.db
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
template="$root/deploy/launchd/token-rate.plist.tmpl"

label="${TOKEN_RATE_LABEL:-dev.token-rate.nightly}"
binary="${TOKEN_RATE_BIN:-$(command -v token-rate || echo "$HOME/go/bin/token-rate")}"
models="${TOKEN_RATE_MODELS:-qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b}"
runs="${TOKEN_RATE_RUNS:-5}"
interval="${TOKEN_RATE_INTERVAL:-30s}"
cache="${TOKEN_RATE_CACHE:-miss}"
tag="${TOKEN_RATE_TAG:-nightly}"
hour="${TOKEN_RATE_HOUR:-3}"
minute="${TOKEN_RATE_MINUTE:-0}"
db="${TOKEN_RATE_DB:-${XDG_DATA_HOME:-$HOME/.local/share}/token-rate/bench.db}"
log_dir="${XDG_STATE_HOME:-$HOME/.local/state}/token-rate"
plist="$HOME/Library/LaunchAgents/$label.plist"
domain="gui/$(id -u)"

die() { echo "install-agent: $*" >&2; exit 1; }

# xml_escape writes $1 with the XML special characters escaped.
xml_escape() {
    local s="${1//&/&amp;}"
    s="${s//</&lt;}"
    s="${s//>/&gt;}"
    printf '%s' "$s"
}

# sed_escape writes $1 so that sed takes it as a literal replacement.
sed_escape() { printf '%s' "$1" | sed -e 's/[\/&|]/\\&/g'; }

render() {
    local args=()
    local pair key value
    for pair in \
        "LABEL=$label" "BINARY=$binary" "MODELS=$models" "RUNS=$runs" \
        "INTERVAL=$interval" "CACHE=$cache" "TAG=$tag" "DB=$db" \
        "HOUR=$hour" "MINUTE=$minute" "LOG_DIR=$log_dir"; do
        key="${pair%%=*}"
        value="$(sed_escape "$(xml_escape "${pair#*=}")")"
        args+=(-e "s|{{$key}}|$value|g")
    done
    sed "${args[@]}" "$template"
}

uninstall() {
    launchctl bootout "$domain/$label" 2>/dev/null || true
    rm -f "$plist"
    echo "removed $label"
}

check() {
    [[ "$binary" = /* ]] || die "TOKEN_RATE_BIN must be an absolute path, not $binary"
    [[ "$db" = /* ]] || die "TOKEN_RATE_DB must be an absolute path, not $db"
    [[ "$hour" =~ ^([01]?[0-9]|2[0-3])$ ]] || die "TOKEN_RATE_HOUR must be 0-23, not $hour"
    [[ "$minute" =~ ^[0-5]?[0-9]$ ]] || die "TOKEN_RATE_MINUTE must be 0-59, not $minute"
    [[ "$runs" =~ ^[1-9][0-9]*$ ]] || die "TOKEN_RATE_RUNS must be 1 or more, not $runs"
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

check
# Base 10, so that 08 and 09 are numbers for printf and plain in the plist.
hour=$((10#$hour))
minute=$((10#$minute))
case "${1:-}" in
    --dry-run) render ;;
    --uninstall) uninstall ;;
    "") install ;;
    *) die "unknown argument $1; use --dry-run or --uninstall" ;;
esac
