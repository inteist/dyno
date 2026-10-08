# dyno

`dyno` benchmarks models through [Ollama](https://ollama.com) or an existing
OpenAI-compatible Chat Completions endpoint, including vLLM. Ollama provides
server decode/cache measurements; compatible endpoints provide client timings
and authoritative usage when the server supplies it. It runs the
models that you select one after the other, repeats each measurement, waits
between runs, and writes every request to a SQLite file. The `report` command
averages all runs across all sessions.

It is one Go binary without cgo. In a terminal it shows a TUI. When the output
is not a terminal (a pipe, `launchd`), or with `--plain`, it writes plain text.

## Install

```sh
just install          # go install ./cmd/dyno
dyno version
```

Requirements: Go 1.27 to build, and either an Ollama server (0.34 or later)
with models pulled or a running compatible endpoint. Ollama remains the default,
at `http://127.0.0.1:11434`. Dyno does not install, start, or stop vLLM.

## Use

```sh
# Decode speed with the short prompt: 5 runs per model, 30 s apart
dyno run --models qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b --runs 5 --interval 30s

# Cache comparison; the long prompt is chosen automatically
dyno run --models qwen3.5:4b-mlx,lfm2.5:8b --cache miss,continue,prefix --runs 3 --tag cache-baseline

# All-time report, and a daily trend for one model
dyno report
dyno report --model lfm2.5:8b --group model,mode,day --since 2026-10-01

# Models and available details (Ollama by default)
dyno models
```

Stop a session with `q q` or `ctrl+c` in the TUI, or with Ctrl-C in plain mode.
The finished runs stay in the database, and the session gets the status
`aborted`. The tool then unloads the models, for at most 30 s. In plain mode, a
second Ctrl-C ends the process at once. In the TUI, `ctrl+c` is a key, so the
program ends after the cleanup.

For Ollama only, a model name without a tag gets `:latest`: `--models lfm2.5`
selects `lfm2.5:latest`. Endpoint IDs are opaque and stay unchanged.

### OpenAI-compatible endpoints and vLLM

```sh
dyno run --backend openai --base-url http://127.0.0.1:8000/v1 \
  --models bench-model --runs 5 --cache miss

# Optional declared deployment facts and immutable model revision
dyno run --backend openai --base-url https://gpu.example/llm/v1 \
  --server vllm --server-version 0.31.0 --models bench-model \
  --model-revision immutable-weight-revision --tag gpu-baseline

# An inference-only key can deliberately skip restricted model listing.
# The secret is read from BENCH_API_KEY, never passed on the command line.
dyno run --backend openai --base-url https://gpu.example/llm/v1 \
  --api-key-env BENCH_API_KEY --skip-model-discovery --models bench-model

# No usage support: timing-only runs, without fabricated token counts/rates
dyno run --backend openai --models bench-model --usage off

dyno models --backend openai --base-url http://127.0.0.1:8000/v1
dyno report --backend openai
```

`--host` is an API-base alias for this backend. Preserve the full proxy/API
path; do not supply credentials, queries, or fragments in the URL. Resolution is
explicit URL, `OPENAI_BASE_URL`, then **local** `http://127.0.0.1:8000/v1`—never
an implicit public paid endpoint. If both `--host` and `--base-url` are supplied,
they must resolve to the same canonical API base. Host case and default ports are
normalized; proxy paths are preserved. URL paths must also be free of secrets.
Authentication defaults to `OPENAI_API_KEY`; unset means no bearer header.
An explicitly named `--api-key-env NAME` must be nonempty in the **process**
environment, not just an interactive shell. Remote authenticated endpoints print
a notice naming the credential variable; plain HTTP additionally prints a
credential-protection warning. Prefer HTTPS. SDK platform/OS telemetry headers
are suppressed; ambient organization/project/admin credentials are not used.

`--usage required` (default) requests final streaming usage and fails responses
without valid counts. The required-usage contract expects a separate final
`choices: []` usage chunk after the finish chunk, as specified by OpenAI streaming
usage. Prompt/completion counts must be nonnegative integers; `total_tokens`, if
present, must equal their sum. Finish-chunk usage or inconsistent totals are
protocol errors, not silently accepted measurements. `off` omits that request
and records timings only for endpoints that cannot satisfy this contract.
`--token-limit-field max_tokens` (default) or `max_completion_tokens` selects
exactly one request field. `--omit-seed` and `--omit-temperature` explicitly omit
unsupported fields and record the effective policy. An omission flag conflicts
with an explicitly supplied `--seed` or `--temperature`, respectively; ignored
defaults do not affect the workload hash. No automatic retries or parameter
negotiation occur. There is exactly one inference POST per exchange.

Context is server-managed: the Ollama `8192` default does not apply, and explicit
`--num-ctx`/`--keep-alive` are rejected. Metadata/admin routes and public model
listing are not prerequisites. Only missing/unsupported listing routes fall back
to supplied IDs automatically; authentication/transport failures remain errors.
A warm-up validates inference. Each selected served ID is warmed once; no model
load/unload, process shutdown, or cache-reset operation is performed.

Only `miss` is currently supported for compatible endpoints. Unknown cached
usage is **cache-unverified**, not zero, and no uncached-prefill claim is made.
`continue`/`prefix`, reasoning-disable profiles, and administrative vLLM reset
features remain gated on pinned, isolated live-server validation. Declaring
`--server vllm` supplies provenance; it does not authorize admin calls.
Reasoning behavior remains unknown, and completion usage may include reasoning
tokens. Use a known non-thinking model for controlled baseline comparisons.

Without `--model-revision`, aliases receive session-scoped identities: identical
names/URLs do not establish immutable weights across sessions. Consequently,
the default all-sessions report keeps separate rows, displayed with short
`session-…` IDs rather than repeating the alias as MODEL-ID. A supplied revision
is operator provenance, not a verified digest. Hash-like revisions use 12
characters plus `…` in cells; the full constant revision appears once as a fact.
Revision labels are unpadded printable text (max 64 characters), or
`sha256:<64 hex>`. Server versions are also unpadded, with at most 64 printable
characters; tags retain unrestricted length. All exclude terminal escapes and
invisible controls.
Only 40/64-hex digests are abbreviated; numeric timestamps remain literal.
To deliberately pool alias sessions, use
`dyno report --backend openai --group backend,endpoint,model,mode`; protocol and
request-policy partitions still protect incompatible measurements. Host/power
facts describe the **Dyno client machine**, not a remote GPU. Unavailable catalog facts such as
model size or thinking support render as `-`, not `0 B` or `no`.

### `run` options

| Flag                                | Default                                               | Meaning                                                                                                        |
| ----------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `--backend`                         | `ollama`                                              | `ollama` or `openai`; see endpoint-specific settings above.                                                    |
| `--base-url`                        | local `http://127.0.0.1:8000/v1` or `OPENAI_BASE_URL` | Full compatible API base, including proxy path.                                                                |
| `--api-key-env`                     | `OPENAI_API_KEY` (optional default)                   | Credential variable name; an explicit name must resolve to a nonempty value.                                   |
| `--server`, `--server-version`      | `unknown`, empty                                      | Operator-declared engine (`unknown` or `vllm`) and version; not server verification.                           |
| `--model-revision`                  | empty                                                 | Immutable operator-supplied revision for one served model.                                                     |
| `--usage`                           | `required`                                            | Require streaming usage or use timing-only `off`.                                                              |
| `--skip-model-discovery`            | off                                                   | Use explicit IDs without `GET /models`; the warm-up still validates inference.                                 |
| `--token-limit-field`               | `max_tokens`                                          | `max_tokens` or `max_completion_tokens`, never both.                                                           |
| `--omit-seed`, `--omit-temperature` | off                                                   | Omit unsupported sampling fields; conflict with their explicit value flags.                                    |
| `--models`                          | required                                              | Comma-separated model names.                                                                                   |
| `--runs`                            | `5`                                                   | Measured runs for each model and each cache mode.                                                              |
| `--interval`                        | `30s`                                                 | Pause after each run, not after the last one.                                                                  |
| `--cache`                           | `miss`                                                | Cache modes: `miss`, `continue`, `prefix`.                                                                     |
| `--prompt`                          | automatic                                             | `short`, `long`, or a JSON file. Automatic: `long` if a mode needs a system part, else `short`.                |
| `--num-predict`                     | `512`                                                 | Tokens to generate.                                                                                            |
| `--num-ctx`                         | `8192`                                                | Context size.                                                                                                  |
| `--temperature`, `--seed`           | `0`, `42`                                             | Sampling settings.                                                                                             |
| `--order`                           | `model`                                               | `model`: all runs of a model, then the next. `round-robin`: run 1 of every model, then run 2.                  |
| `--keep-alive`                      | the larger of `10m` and interval + `5m`               | How long the server keeps a model loaded. Must be longer than the interval.                                    |
| `--request-timeout`                 | `10m`                                                 | Limit for one request.                                                                                         |
| `--tag`                             | empty                                                 | Free text that marks a setup, for example `kv-q8`.                                                             |
| `--plain`                           | off                                                   | Plain text, also in a terminal.                                                                                |
| `--db`, `--host`, `--log`           |                                                       | Database path (relative to the working folder), server address, and a log file that gets a copy of the events. |

### `report` options

| Flag                                                                | Default                                                                   | Meaning                                                            |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `--since`, `--until`                                                | none                                                                      | `YYYY-MM-DD` (local time, `--until` includes the day) or RFC 3339. |
| `--model`, `--cache`, `--prompt`, `--tag`, `--session`, `--backend` | none                                                                      | Comma-separated offline filters.                                   |
| `--group`                                                           | `model,model-id,backend,engine,endpoint,server,measurement,workload,mode` | Also `digest` (legacy alias), `tag`, `day`, `week`, `session`.     |
| `--plain`                                                           | off                                                                       | Plain table, also in a terminal.                                   |

The default grouping keeps full model identities, backends, engines, endpoints,
measurement protocols, versions, and workloads separate. Incompatible measurement
protocols and effective request/context policies remain partitioned even with
custom grouping; policy fingerprints use full keys and are abbreviated only for
display. In particular, omitting seed/temperature cannot mix with explicitly
sending them, even for a trusted model revision. `N` counts valid rows;
each metric has its own sample count. Missing metrics render as `-`, not zero.
The TUI displays constant facts once, hiding a native MODEL-ID only when the
model name determines it uniquely. Native compact tables prioritize cache,
prefill, and exclusions over client statistics. Plain native default reports
retain their historical five dimensions, six statistics columns, and four
statistics tables (including the native `DIGEST` header and blank unknown SERVER
cells). Post-run native history summaries keep their four historical dimensions.
Explicit custom dimensions remain visible. Compatible reports intentionally add
provenance/client-stat columns and two client statistics tables; positional
parsers must select the appropriate schema. Headers do not change with metric
availability. The first line remains the report heading followed by the table
header. `EXCLUDED` counts failed/short or native cache/load-invalid runs;
`WARNINGS` records non-excluding uncertainty such as unverified cache use or
unknown reasoning policy. Client TTFT starts at the first nonempty visible-content
delta; reasoning-only deltas are not counted as visible output. Streaming progress
counts content chunks, not authoritative tokens. Model discovery is bounded to
30 seconds, independently of inference timeouts. Missing `/models` routes
(404/405) permit explicit-ID fallback even with an oversized bounded error body.
Dial/DNS/TLS establishment failures abort the session; resets, EOF, and broken
pipes after connection fail that request but normally allow remaining measured
runs.
After three consecutive failed steps for one compatible model, its remaining
requests are skipped; later models still run. A successful request resets that
model's streak. The limit still marks the session failed, with a nonzero exit.
Native interactive reports can browse available client timing charts; the plain
native schema intentionally retains its historical statistics.

The plain report also prints one statistics table for each metric (decode rate,
prefill time, prefill rate, TTFT, client latency, and client completion rate):
the count, mean, median, standard deviation, minimum, and maximum of each group. The TUI report shows the same
statistics for the selected group and metric, and `tab` changes the metric. The
prefill rate counts only `miss` runs, because after a cache hit too few
uncached tokens remain for a rate. Other groups show `-` for it.

### Prompt files

A prompt file is JSON:

```json
{
    "name": "code-review",
    "system": "the shared prefix, required for continue and prefix",
    "question": "the first question, required",
    "follow_up": "the second question, required for continue"
}
```

Ask for more text than `--num-predict`, so that each run reaches the limit. Runs
that stop early get the flag `short` and do not count.

## Measurement protocol

Each **Ollama** session does these steps:

1. It checks that the server has every model. It stops before the first request
   if a model is missing.
2. Before the first step of a model, it loads the model and sends one warm-up
   request. The warm-up does not count. The MLX engine has a slow first prefill
   after a load.
3. For each step, the cache mode prepares the cache, and then the tool sends the
   measured run.
4. It waits for the interval, and it unloads each model after its last step.

| Cache mode | Preparation                                                           | Measured request                          |
| ---------- | --------------------------------------------------------------------- | ----------------------------------------- |
| `miss`     | none                                                                  | A new chat with a unique ID at the start. |
| `continue` | One primer: the first turn, with a 64-token answer.                   | The next turn of that chat.               |
| `prefix`   | Reload the model, then two 1-token primers with the same system part. | The same system part with a new question. |

Every request uses `temperature 0`, `seed 42`, `think: false` (only for models
with the thinking capability), `truncate: false`, and `shift: false`. Thus a
prompt that is too long for `num_ctx` fails visibly.

The rates come from the server timings, with the same formulas as
`ollama run --verbose`:

| Value                   | Formula                                                                               |
| ----------------------- | ------------------------------------------------------------------------------------- |
| Decode rate             | `eval_count / eval_duration`                                                          |
| Prefill rate            | `(prompt_eval_count − cached) / prompt_eval_duration`, only for `miss` runs           |
| Prefill time            | `prompt_eval_duration`                                                                |
| Cached share            | `prompt_eval_cached_count / prompt_eval_count`                                        |
| TTFT                    | first generated content minus request start, measured by the client                   |
| Client latency          | successful stream completion minus request start                                      |
| Client completion tok/s | completion usage / client wall seconds; includes prefill, queue, and network overhead |

Compatible chat APIs do not supply Ollama-style server durations. TTFT is not
prefill time, and client completion rate is not server decode speed. Streaming
progress counts **content chunks**, never estimated tokens or chunk-derived tok/s.
Absent counts, cache details, and durations remain SQL NULL. Cache/load exclusion
thresholds below are calibrated for Ollama, not applied universally.

The report leaves a run out of the statistics when the request failed, when the
model stopped before `num_predict` (`short`), when a `miss` run took more than 1
% and more than 16 tokens from the cache (`unexpected-hit`), or when the server
reported a load of more than 1 s during the run (`load-in-run`). The
chat-template tokens before the unique ID always come from the cache, so a few
cached tokens in a `miss` run are normal. The report shows the number of
excluded runs.

Benchmark on AC power. On battery, macOS can halve the decode rate and make it
vary between identical requests. Each session records the power source.

The server settings, such as `OLLAMA_KV_CACHE_TYPE`, are not visible through the
API. Mark a changed setup with `--tag`.

## Data

The database is `$XDG_DATA_HOME/dyno/bench.db`, or
`~/.local/share/dyno/bench.db` without `XDG_DATA_HOME`. `--db` and
`DYNO_DB` change the path. The file uses WAL mode, so `dyno report`
can read during a session.

The tables are `sessions`, `models`, and `exchanges`. An exchange is one
request: a `warmup`, a `primer`, or a `run`. The store keeps raw counts and
nanoseconds only. A query without the tool:

```sql
SELECT x.model_name, x.cache_mode, COUNT(*) AS n,
       ROUND(median(x.eval_tokens * 1e9 / x.eval_ns), 1)            AS decode_tps,
       ROUND(median(x.prompt_ns) / 1e6)                             AS prefill_ms,
       ROUND(median(1.0 * x.cached_tokens / x.prompt_tokens) * 100) AS cached_pct
FROM exchanges x
JOIN sessions s ON s.id = x.session_id
WHERE x.kind = 'run' AND x.error IS NULL AND x.done_reason = 'length'
GROUP BY x.model_name, x.model_key, s.backend, s.server_kind, s.endpoint,
         s.server_version, s.measurement_protocol, s.workload_hash, x.cache_mode;
```

The macOS `sqlite3` has `median()`. Back up the file with
`sqlite3 bench.db ".backup bench-backup.db"`.

## Database upgrade safety

Before replacing the binary, stop/unload the scheduled agent and wait for all
running sessions to finish. **Do not migrate a database while an old binary is
writing to it.** Back up with `sqlite3 bench.db ".backup bench-backup.db"` rather
than copying a WAL-mode database file alone. Forward migrations preserve legacy
Ollama rows; old binaries cannot read the upgraded schema. Historical cached
values already flattened to zero cannot be reconstructed. A crash between
migration steps is recoverable by reopening with the new binary.

## Scheduled sessions

`scripts/install-agent.sh` installs a `launchd` agent that starts a plain-mode
session every night at 03:00 with the tag `nightly`:

```sh
just install
just agent-install                                  # defaults
DYNO_CACHE=miss,continue,prefix DYNO_HOUR=2 just agent-install
scripts/install-agent.sh --dry-run                  # print the plist only
launchctl kickstart gui/$(id -u)/dev.dyno.nightly   # start it now
just agent-uninstall
```

For compatible scheduled sessions, set `DYNO_BACKEND=openai`, `DYNO_BASE_URL`,
and `DYNO_MODELS` (served IDs); `DYNO_CACHE=miss` is required. The optional
`DYNO_API_KEY_ENV` is a **variable name**, never the key itself. Provision the
credential in launchd's environment separately; the installer never embeds it
in a plist or arguments. Compatible jobs require explicit served IDs; no Ollama
model defaults are substituted. Optional `DYNO_SERVER_KIND`, `DYNO_SERVER_VERSION`,
`DYNO_MODEL_REVISION`, `DYNO_USAGE`, `DYNO_TOKEN_LIMIT_FIELD`,
`DYNO_SKIP_MODEL_DISCOVERY`, `DYNO_OMIT_SEED`, and `DYNO_OMIT_TEMPERATURE` map to
the corresponding flags (booleans are `0` or `1`). With no `DYNO_API_KEY_ENV`,
the job uses the binary's optional `OPENAI_API_KEY` default. Existing Ollama
schedule defaults stay unchanged; uninstall ignores stale endpoint settings.
`--uninstall` and `--dry-run` cannot be combined; the script rejects that
combination without unloading the job or deleting its plist.

For an authenticated launchd job, provision the named variable in the current
user's launchd environment **before installing/reloading the job**. For example,
with shell tracing disabled (`set +x`), read a key without putting its value in
shell history:

```sh
set +x
printf 'Endpoint key: '; IFS= read -r -s BENCH_KEY; printf '\n'
launchctl setenv BENCH_KEY "$BENCH_KEY"
unset BENCH_KEY
DYNO_BACKEND=openai DYNO_BASE_URL=https://gpu.example/v1 \
  DYNO_MODELS=served-id DYNO_API_KEY_ENV=BENCH_KEY just agent-install
# Remove it when no longer needed:
launchctl unsetenv BENCH_KEY
```

`setenv` applies to future jobs in this login session; it is not persistent
across logout/reboot. Re-provision and reload the agent after login, or use a
private Keychain-backed wrapper to export the variable before executing Dyno.
Launchd environments are shared with other jobs in that user's domain, and the
`setenv` value can briefly be visible in process arguments; use a dedicated
account/Keychain wrapper if that exposure is unacceptable. Never paste the key
into `DYNO_API_KEY_ENV`, a URL, plist, or installer argument.

The script header lists all settings. The logs go to
`~/.local/state/dyno/`. The selected endpoint must be reachable at the start time;
Ollama sessions require the Ollama app to be running. When
the Mac sleeps at the start time, `launchd` starts the job after the wake.

### Offline helper checks

`just check` runs the Go-only release gate; Python is not required. If Python 3
is installed, `just script-test` separately tests the opt-in capture helper with
`python3 -B -I`, without writing `__pycache__` files. Neither command requires a
live model server.

## Upgrade from token-rate

The tool was called `token-rate`. `dyno` does not read the old names, and it
gives no warning:

| Old                                     | New                               |
| --------------------------------------- | --------------------------------- |
| `~/.local/share/token-rate/bench.db`    | `~/.local/share/dyno/bench.db`    |
| `TOKEN_RATE_DB`                         | `DYNO_DB`                         |
| `TOKEN_RATE_*` settings of the agent    | `DYNO_*`                          |
| the agent `dev.token-rate.nightly`      | `dev.dyno.nightly`                |
| the logs in `~/.local/state/token-rate` | the logs in `~/.local/state/dyno` |

Copy the database before the first `dyno` command. The file uses WAL mode,
so `mv` or `cp` of `bench.db` alone can lose the runs in `bench.db-wal`.
`.backup` copies all of them. `dyno` changes the schema of the file, and
`token-rate` cannot open it after that, so keep the old file as a backup.

```sh
mkdir -p ~/.local/share/dyno
sqlite3 ~/.local/share/token-rate/bench.db ".backup $HOME/.local/share/dyno/bench.db"
```

Then replace the binary and the agent:

```sh
DYNO_LABEL=dev.token-rate.nightly scripts/install-agent.sh --uninstall   # the old agent
rm ~/go/bin/token-rate
just install
just agent-install                                  # with DYNO_* instead of TOKEN_RATE_*
```

With `XDG_DATA_HOME` or `XDG_STATE_HOME` set, the folders are below those
paths.

## Versioning

The version is a Git tag of the form `vMAJOR.MINOR.PATCH`. The Go build reads the
tag, so no file holds the number. `dyno version` prints what the build found, and
each session records it as `tool_version`.

The examples assume a build from a Git checkout that has the release tags. A
checkout without the tags reports `v0.0.0-…`, and a build without Git data
reports `(devel)`.

| Build                          | `dyno version` output                             |
| ------------------------------ | ------------------------------------------------- |
| Tagged commit                  | `dyno v0.2.0`                                    |
| Commits after the tag          | `dyno v0.2.1-0.20261007230857-3f2c9a1b7d4e`       |
| Same, with uncommitted changes | `dyno v0.2.1-0.20261007230857-3f2c9a1b7d4e-dirty` |
| No tag yet                     | `dyno v0.0.0-20261007230857-3f2c9a1b7d4e`         |

```sh
git tag --list 'v*' --sort=-v:refname   # releases so far
just version                            # build, then print this tree's version
just release 0.2.0                      # run just check, then tag HEAD as v0.2.0
git push origin v0.2.0                  # publish the tag
```

`just release` stops if `just check` fails. It also refuses uncommitted or
untracked files, an existing tag, and a noncanonical number.

A clean build of the tagged commit reports just the release number. A build
after the tag reports a Go pseudo-version: the next patch number, a date, and a
commit. Local changes add `-dirty`. Results from such a build are never mistaken
for a release. Choose the next number by the change: patch for a
repaired bug, minor for a new feature or option, major for a break in the stored
data or the output format.

`just release` accepts only canonical numbers with major version 0 or 1. Go
ignores a `v2.0.0` or later tag while the module path is `dyno` without a `/v2`
suffix, so the build would report an older version. Before the first v2 release,
change the module path or stop reading the version from Go's module inference.

## Development

```sh
just check      # Go-only: gofmt, go vet, staticcheck, go test -race
just script-test # optional Python 3 capture-helper tests; no live endpoint
just cover      # statement coverage
just golden     # rewrite the golden files: UPDATE_GOLDEN=1 go test ./...
just fixtures   # record new Ollama streams for the adapter tests
```

Python is optional for development: only `just script-test` and manual fixture
capture use it. The release gate, Dyno binary, and scheduled agent do not. See
[Database upgrade safety](#database-upgrade-safety) before upgrading existing data.

Optional live checks (not run by CI; require an existing endpoint):

```sh
DYNO_LIVE_OPENAI=1 OPENAI_BASE_URL=http://127.0.0.1:8000/v1 DYNO_MODEL=bench-model \
  scripts/smoke-compatible.sh
# Python stdlib is needed for manual sanitized fixture capture (and helper tests).
DYNO_LIVE_OPENAI=1 OPENAI_BASE_URL=http://127.0.0.1:8000/v1 DYNO_MODEL=bench-model \
  scripts/capture-compatible-fixtures.py /tmp/dyno-compatible-capture
```

Smoke checks fail if no statistically valid runs are recorded. Captures create
private directories/files (`0700`/`0600`), but their generated content may still
be sensitive: review it before using it as testdata. Live GPU/cache validation
is separate from the CPU-only mocked SDK contract tests.

The design and the plan are in `plans/dyno-implementation-plan.md` and
`plans/vllm-implementation-plan.md`.
`internal/archtest` fails when a package imports across the permitted
directions.
