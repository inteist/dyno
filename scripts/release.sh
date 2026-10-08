#!/usr/bin/env bash
# Tag HEAD as release vVERSION. Usage: release.sh VERSION, for example 0.2.0.
# Refuses a noncanonical number, major version 2 or later, an existing tag, and
# a tree with changes. Does not push. `just release` runs `just check` first.
set -euo pipefail
version="${1-}"
# Canonical semver, major 0 or 1: Go ignores a v2+ tag while the module path is
# `dyno` (no /v2 suffix), so the build would report another version.
re='^(0|1)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
[[ $# -eq 1 && $version =~ $re ]] || { echo "usage: release.sh VERSION (0.2.0 or 1.2.3), got: $version" >&2; exit 2; }
status="$(git status --porcelain --untracked-files=normal)"
[[ -z "$status" ]] || { echo 'working tree is not clean' >&2; exit 1; }
git tag -a "v$version" -m "dyno v$version"
echo "tagged v$version; push it with: git push origin v$version"
