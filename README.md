# dyno

`dyno` measures how fast local models generate tokens through
[Ollama](https://ollama.com), and how much the prompt cache saves. It runs the
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

Requirements: Go 1.27 to build, and an Ollama server (0.34 or later) with the
models pulled. The default server address is `http://127.0.0.1:11434`.

## Use

```sh
# Decode speed with the short prompt: 5 runs per model, 30 s apart
dyno run --models qwen3.5:9b-mlx,qwen3.5:4b-mlx,lfm2.5:8b --runs 5 --interval 30s

# Cache comparison; the long prompt is chosen automatically
dyno run --models qwen3.5:4b-mlx,lfm2.5:8b --cache miss,continue,prefix --runs 3 --tag cache-baseline

# All-time report, and a daily trend for one model
dyno report
dyno report --model lfm2.5:8b --group model,mode,day --since 2026-10-01

# Local models and their details
dyno models
```

Stop a session with `q q` or `ctrl+c` in the TUI, or with Ctrl-C in plain mode.
The finished runs stay in the database, and the session gets the status
`aborted`. The tool then unloads the models, for at most 30 s. In plain mode, a
second Ctrl-C ends the process at once. In the TUI, `ctrl+c` is a key, so the
program ends after the cleanup.

A model name without a tag gets `:latest`, as in Ollama: `--models lfm2.5`
selects `lfm2.5:latest`.

### `run` options

| Flag                      | Default                                 | Meaning                                                                                         |
| ------------------------- | --------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `--models`                | required                                | Comma-separated model names.                                                                    |
| `--runs`                  | `5`                                     | Measured runs for each model and each cache mode.                                               |
| `--interval`              | `30s`                                   | Pause after each run, not after the last one.                                                   |
| `--cache`                 | `miss`                                  | Cache modes: `miss`, `continue`, `prefix`.                                                      |
| `--prompt`                | automatic                               | `short`, `long`, or a JSON file. Automatic: `long` if a mode needs a system part, else `short`. |
| `--num-predict`           | `512`                                   | Tokens to generate.                                                                             |
| `--num-ctx`               | `8192`                                  | Context size.                                                                                   |
| `--temperature`, `--seed` | `0`, `42`                               | Sampling settings.                                                                              |
| `--order`                 | `model`                                 | `model`: all runs of a model, then the next. `round-robin`: run 1 of every model, then run 2.   |
| `--keep-alive`            | the larger of `10m` and interval + `5m` | How long the server keeps a model loaded. Must be longer than the interval.                     |
| `--request-timeout`       | `10m`                                   | Limit for one request.                                                                          |
| `--tag`                   | empty                                   | Free text that marks a setup, for example `kv-q8`.                                              |
| `--plain`                 | off                                     | Plain text, also in a terminal.                                                                 |
| `--db`, `--host`, `--log` |                                         | Database path (relative to the working folder), server address, and a log file that gets a copy of the events. |

### `report` options

| Flag                                                   | Default                             | Meaning                                                                                  |
| ------------------------------------------------------ | ----------------------------------- | ---------------------------------------------------------------------------------------- |
| `--since`, `--until`                                   | none                                | `YYYY-MM-DD` (local time, `--until` includes the day) or RFC 3339.                       |
| `--model`, `--cache`, `--prompt`, `--tag`, `--session` | none                                | Comma-separated filters.                                                                 |
| `--group`                                              | `model,digest,server,workload,mode` | Any of `model`, `digest`, `server`, `workload`, `mode`, `tag`, `day`, `week`, `session`. |
| `--plain`                                              | off                                 | Plain table, also in a terminal.                                                         |

The default grouping never mixes runs of different model digests, server
versions, or workloads.

The plain report also prints one statistics table for each metric (decode rate,
prefill time, prefill rate, and TTFT): the count, mean, median, standard
deviation, minimum, and maximum of each group. The TUI report shows the same
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

Each session does these steps:

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

| Value        | Formula                                                          |
| ------------ | ---------------------------------------------------------------- |
| Decode rate  | `eval_count / eval_duration`                                     |
| Prefill rate | `(prompt_eval_count − cached) / prompt_eval_duration`, only for `miss` runs |
| Prefill time | `prompt_eval_duration`                                           |
| Cached share | `prompt_eval_cached_count / prompt_eval_count`                   |
| TTFT         | first streamed chunk minus request start, measured by the client |

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
GROUP BY x.model_name, x.model_digest, s.server_version, s.workload_hash, x.cache_mode;
```

The macOS `sqlite3` has `median()`. Back up the file with
`sqlite3 bench.db ".backup bench-backup.db"`.

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

The script header lists all settings. The logs go to
`~/.local/state/dyno/`. The Ollama app must run at the start time. When
the Mac sleeps at the start time, `launchd` starts the job after the wake.

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

## Development

```sh
just check      # gofmt, go vet, staticcheck, go test -race
just cover      # statement coverage
just golden     # rewrite the golden files: UPDATE_GOLDEN=1 go test ./...
just fixtures   # record new Ollama streams for the adapter tests
```

The design and the plan are in `plans/dyno-implementation-plan.md`.
`internal/archtest` fails when a package imports across the permitted
directions.
