# OpenAI-compatible endpoint benchmarks, including vLLM

Status: baseline endpoint implementation delivered. The SDK-backed `openai`
backend, CLI, lifecycle split, nullable metrics, identity/provenance migrations,
reports, UI, scheduled-job configuration, and offline tests are implemented.
Live GPU validation and real-server fixture capture remain pending; external
`continue`/`prefix` modes and vLLM administrative operations are intentionally
rejected until their capability gates pass. This document retains the complete
architecture and later-stage implementation/validation plan.

Accepted direction: a generic `openai` endpoint backend using the official
`github.com/openai/openai-go/v3` SDK, with vLLM-specific operations optional.

Follow-on to [the Dyno implementation plan](dyno-implementation-plan.md), section
20.3, “Adding a server”. The original proposal was vLLM-specific. The revised
objective is **benchmarking an existing LLM endpoint**, usually OpenAI-compatible,
with vLLM as one supported server implementation—not the only target.

## 1. Adopted design

Add an `openai` benchmark backend using the official OpenAI Go SDK.
Keep Ollama as the default and preserve its native adapter and lifecycle behavior.

```text
CLI -> app provider factory -> llm.Client -> existing benchmark runner
                              |
                              +-- internal/ollama       -> Ollama SDK
                              |
                              +-- internal/openaicompat -> OpenAI Go SDK
                                     |
                                     +-- optional vLLM profile/operations
```

The `openai` backend name describes the **wire protocol**, not ownership by OpenAI
or the actual inference engine. Store/display server kind separately: for example
`backend=openai`, `server_kind=vllm`, or `server_kind=unknown`.

A baseline invocation should need only a URL and model ID:

```sh
dyno run --backend openai \
  --base-url http://127.0.0.1:8000/v1 \
  --models bench-model --cache miss --runs 5
```

This must not require a local vLLM installation, `/version`, a public `/models`
route, model loading/unloading, a discoverable context limit, or administrative
cache-reset permissions. The operator starts/manages the server independently.

### Initial scope

- Text chat-completion requests, streaming, warm-up, existing sequential scheduling.
- Actual usage counts when available; client TTFT, latency, and completion throughput.
- Authentication, custom API base URLs, cancellation, recording, plain/TUI reports.
- Optional model listing; exact supplied model IDs even when listing is unavailable.
- `miss` workloads with an explicit distinction between cache-verified and unverified.
- Existing multi-model scheduling over served IDs, without claiming engine swapping.

Later: verified `continue` experiments and opt-in vLLM `prefix` cache reset.

Not in scope: installing/starting vLLM, invoking `vllm bench`, concurrent load
sweeps, tools/multimodal requests, sleep/wake/LoRA administration, arbitrary
provider plugins, Azure-specific authentication/routes, or an orchestration
framework such as LangChain. This is a benchmark client, not an agent framework.

## 2. Library choice and responsibilities

### Selected library: official OpenAI Go SDK

Use **`github.com/openai/openai-go/v3`**, isolated inside `internal/openaicompat`.
The inspected release is **v3.71.2**; pin a tested release in `go.mod`/`go.sum`
during implementation rather than depending on `main` or an unbounded `latest`.

| Requirement | SDK support / Dyno responsibility |
| --- | --- |
| Custom compatible endpoint | `option.WithBaseURL`; Dyno validates and resolves the URL. |
| Bearer authentication | `option.WithAPIKey`; Dyno resolves secrets without persisting them. |
| Streaming chat | `client.Chat.Completions.NewStreaming`; SDK handles SSE decoding. |
| HTTP behavior and testing | `option.WithHTTPClient`; inject transport, redirect policy, and `httptest` server. |
| Retry control | `option.WithMaxRetries(0)`; SDK otherwise retries twice by default. |
| Cancellation | Request context plus explicit stream closure. |
| Optional response fields | Generated `.JSON.<Field>.Valid()` metadata distinguishes absent/null from zero. |
| Compatible-server extensions | Typed adapter/profile code; SDK extra fields/raw JSON for narrowly tested extensions. |
| Benchmark semantics | Still Dyno's responsibility: clocks, final usage, validity, cache policy, and provenance. |

Use **Chat Completions**, not the newer Responses API: the former is the intended
compatibility contract for vLLM and many other serving engines. Do not assume
an SDK supporting OpenAI's APIs makes every third-party endpoint compatible.

### Alternative and dependency trade-off

`github.com/sashabaranov/go-openai` is a credible, lighter alternative: custom
`BaseURL`/`HTTPClient`, streaming chat, and no non-standard-library dependencies
in the inspected `go.mod`. Its inspected release is **v1.43.0**.

The official v3.71.2 SDK requires Go **1.25.0**, compatible with Dyno's current
Go **1.27** declaration, but includes Azure/AWS/authentication/WebSocket packages
in its module dependency graph. Do not describe it as a dependency-free solution.

Implemented dependency spike (Go **1.27.1**, **darwin/arm64**, same
`go build -trimpath -ldflags='-s -w'`): Ollama-only Git baseline `c2a07f3`
links **13,645,682 bytes**, current compatible build **20,883,522 bytes**
(**53.0%** growth). These are stripped artifact measurements, not universal
release sizes; platform/compiler/link flags matter. Dyno adds `coder/websocket`
and four `tidwall` module requirements. `x/crypto` was already required through
Ollama and changes **v0.43.0 → v0.55.0**; tidy removes obsolete checksums.
`go list -deps ./cmd/dyno` does not link Azure/AWS packages in the selected
non-cloud transport. Keep the approved official SDK: this measured footprint
is a documented trade-off, not a demonstrated compatibility blocker.

The official SDK is selected for maintained typed APIs and response-field
presence metadata, which is particularly useful for honest measurements. Run a
small compatibility/dependency spike before integration to validate its use in a
benchmark client. The community client is an evaluated alternative, not a second
runtime transport or an automatic fallback. Revisit the selection explicitly only
if validation uncovers a blocking compatibility or dependency issue; Dyno's core
ports must remain unchanged if a replacement is subsequently approved.

### Boundary rules

- The library abstracts HTTP, JSON, authentication, SSE, and API types.
- `llm.Client` remains Dyno's abstraction; never expose SDK types to core code.
- Keep scheduling, lifecycle ownership, metrics, SQLite, reports, and UI in Dyno.
- Do not write a second complete SSE decoder or a cross-provider SDK wrapper.
- A small transport guard may still be necessary for strict termination and size
  limits: the inspected SDK ends iteration on both `[DONE]` and decoder EOF;
  `stream.Err() == nil` alone does not prove a complete benchmark response.

## 3. Existing seams and required refactors

| Current location | Change |
| --- | --- |
| `internal/llm/client.go` | Split mandatory managed lifecycle from shared inference/discovery; add capabilities and optional server facts. |
| `internal/app/deps.go:newClient` | Select `ollama` or `openai`; construct optional vLLM profile only when explicitly requested. |
| `internal/bench/runner.go` | Reuse preflight, schedule, events, and durable session completion; tolerate unknown server version/metadata. |
| `internal/bench/lifecycle.go` | Add external readiness: warm-up without load/poll/unload or server ownership. |
| `internal/scenario/prefix.go`, `internal/bench/executor.go` | Rename `Reload` intent to `ResetCache`; select a real supported mechanism. |
| `internal/domain/model.go`, `internal/cli/values.go` | Move `:latest` normalization to Ollama; preserve opaque endpoint model IDs. |
| `internal/domain/metrics.go` | Represent measurement availability instead of turning unknown values into zero. |
| `internal/report/{result.go,build.go,series.go,validity.go}` | Per-metric sample availability and protocol-aware validity. |
| `internal/app/run.go` | Update independent all-time grouping and canonical historical model filters. |
| `internal/store/sqlite/{source.go,codec.go}` | Preserve SQL NULL; `orZero` currently erases it. |
| `internal/store/sqlite/migrations/0001_init.sql` | Separate model storage identity from content digest; endpoint IDs are not checksums. |
| `internal/ui/tui/runview/{state.go,view.go}` | Stop turning approximate chunk progress into authoritative tokens/tok/s. |
| `internal/archtest/imports_test.go` | Permit the SDK only within the compatible adapter. |

The existing prompt builders, order/schedule logic, statistics, throttling, and
event transport remain shared. Adding a communication library does not remove
the lifecycle and measurement differences that those seams need to represent.

## 4. CLI and endpoint configuration

| Setting | Proposed contract |
| --- | --- |
| `run/models --backend` | `ollama` default; new value `openai`. |
| `--base-url` | Full OpenAI-compatible API base, such as `https://host/service/v1`; required unless an environment/default supplies it. |
| `--host` | Existing Ollama behavior unchanged; for `openai`, allow as an alias of `--base-url`, with the same full-API-base meaning. Reject conflicting explicit values. |
| `OPENAI_BASE_URL` | Fallback only for the `openai` backend. Otherwise use local `http://127.0.0.1:8000/v1`; never silently select the public paid API. |
| `--api-key-env` | Environment-variable name; default `OPENAI_API_KEY` for compatible endpoints. For vLLM, operators can choose `VLLM_API_KEY`. An unset/empty optional default sends no bearer header; an explicitly selected variable must be set and nonempty. |
| `--usage required\|off` | Default `required`: request final streaming usage and require counts. `off`: omit `stream_options`, allow timing-only benchmarks, and leave counts/throughput unknown. |
| `run --skip-model-discovery` | Explicitly use supplied IDs and validate chat during warm-up; useful when `/models` requires permissions unavailable to an inference-only key. |
| `--token-limit-field` | Explicit `max_tokens` default for broad compatibility, or `max_completion_tokens`; send exactly one, never negotiate through a measured retry. |
| `--server` | Optional declaration, initially `unknown` or `vllm`; do not detect an engine from its model names or URL. |
| `--server-version` | Optional operator-supplied provenance, not a prerequisite for inference. |
| `--model-revision` | Optional immutable revision/checksum, operator-supplied. Initially valid for single-model sessions; per-model mapping can follow later. |
| `--server-root` | Later vLLM extension URL for non-standard metadata/admin routes, separate from the generic API base. |
| `--allow-cache-reset` | Later vLLM-only explicit permission; false by default. |
| `report --backend` | Optional stored-backend filter; no default restriction and no live client connection. |

Resolve backend-dependent settings after parsing, so flag order does not change
model interpretation or defaults. Use injected `cli.Env`. Do not let ambient SDK
`OPENAI_*` defaults silently override Dyno's configuration or add credentials,
organization/project headers, cloud identity, or arbitrary custom headers.
The SDK spike must establish controlled client initialization and test hostile
ambient environment values, including an intentionally empty resolved key.

### URLs and secrets

- Preserve the full API-base path, including proxy prefixes and non-`/v1` paths.
  Do not strip a supplied `/v1`, append another `/v1`, or guess admin routes from
  an arbitrary compatible URL. SDK resource paths are relative to this base.
- Accept trailing slash; reject malformed URLs, URL userinfo, query strings, and
  fragments in the initial configuration contract.
- Test `https://host/llm/v1` and a compatible API base with a different path.
- Keys never enter session rows, events, logs, error-body copies, or fixtures.
- Prevent credentials crossing origins on redirect; use an explicit policy.
- Store only a sanitized endpoint. Warn for credentialed non-local plain HTTP.

### Shared generation settings and honest limits

Keep `--num-predict`, temperature, and seed in the workload. Send explicit values
for the tested endpoint contract. If a target rejects a field, support a documented
explicit omission/profile setting and record the effective policy; never silently
retry or pretend an omitted setting was applied. Do not expose unrestricted JSON
that can override timing-critical fields in the first release.

For compatible endpoints, context capacity is **server-managed**:

- Do not require metadata beyond a usable chat endpoint.
- Do not apply Ollama's implicit `--num-ctx 8192` default to `openai`. Represent
  context as server-managed/unknown (for example internal `ContextSize=0` plus
  explicit context policy), not as an effective zero-token capacity.
- If explicitly supplied, reject `--num-ctx` unless a validated profile can enforce
  it or establish that it equals the known startup limit. A generic endpoint
  cannot promise to resize its engine through a chat request.
- Record any advertised capacity separately; missing capacity is not a preflight
  failure. Context overflow is a visible server error, not silent truncation.
- Future client-enforced budgets can use a tested tokenization endpoint outside
  the timed inference interval, with identical chat-template settings.
- Reject explicit `--keep-alive` for external endpoints; ignore the implicit
  Ollama default there. Keep generic scheduling validation separate.

First validate non-thinking text models. Do not infer thinking support from an
ID or treat missing metadata as false. Record unknown reasoning policy honestly;
for controlled comparisons, use a validated profile/model-specific disable option
(e.g. vLLM `chat_template_kwargs.enable_thinking=false`). Do not label unknown
policies as equivalent to Ollama's explicitly disabled thinking configuration.

## 5. Shared port, naming, and discovery

Proposed narrow contracts, retaining existing inference request/result types:

```go
type Client interface {
    Info(ctx context.Context) (ServerInfo, error)
    NormalizeModelName(name string) string
    Models(ctx context.Context, names ...string) ([]domain.Model, error)
    Chat(ctx context.Context, req ChatRequest, progress ProgressFunc) (ChatResult, error)
}

type ManagedLifecycle interface {
    Running(ctx context.Context) ([]string, error)
    Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error)
    Unload(ctx context.Context, model string) error
}

type CacheResetter interface {
    ResetCache(ctx context.Context, model string) error
}
```

`ServerInfo` describes backend, explicitly known server kind/version and sources,
measurement protocol, capabilities, and context policy. Version/capacity may be
unknown without an error. Standard compatible inference has no managed lifecycle
or reset capability. vLLM optional operations are supplied by an explicitly
configured profile/wrapper; capabilities must agree with implemented ports.

### Naming

Parse trimmed raw strings. Ollama applies its existing `:latest` rule; compatible
model IDs are returned unchanged. Use an idempotent canonicalization helper on
copied options in `internal/app` before passing them to **both** `bench.NewRunner`
and `showSummary`; the runner also applies it defensively for direct callers.

Normalizing only a runner-private copy breaks historical summary filters (`foo`
versus stored `foo:latest`). Normalize before duplicate validation and scheduling.
Reports remain offline: exact raw names match any backend; an Ollama-normalized
alternative matches only Ollama rows. Preserve legacy report shorthand.

### Optional discovery

`dyno models --backend openai` uses `GET models` relative to the API base. If that
route is unavailable, say discovery is unsupported; do not invent a model catalog.

For `run --models ...`, use discovery when available, but allow explicitly supplied
IDs if listing is disabled/unsupported. Synthesize only unknown model metadata,
marked `operator-supplied`, and verify actual inference during the recorded warm-up.
Do not claim such IDs were validated by listing or that their weights are known.
Automatic fallback applies to a missing/unsupported listing route, not to 401/403
or network errors. For an inference-only key facing restricted listing, expose
`--skip-model-discovery`: deliberately skip that call, use supplied IDs, and let
chat warm-up validate inference authorization. Preserve missing-route versus
actual missing-model failures and test both restricted and absent listing.

## 6. SDK-backed adapter and stream correctness

Suggested package layout:

```text
internal/openaicompat/
  doc.go
  client.go        # SDK construction, configuration, discovery
  request.go       # domain conversation/options -> SDK parameters
  response.go      # SDK stream -> domain reply/usage/measurements
  transport.go     # timing/security/size/termination guards only as needed
  errors.go        # SDK/HTTP errors -> Dyno semantics
  *_test.go
  testdata/        # sanitized compatible streams and SDK-version manifest

internal/vllm/      # later optional profile/metadata/cache operations
```

Configure the SDK with a resolved API base/key, a controlled HTTP client, and
**`option.WithMaxRetries(0)`**. Dyno's request context/timeout controls the entire
stream, not merely response headers. Avoid implicit cloud credential discovery.

Map domain roles/messages to SDK chat parameters; explicitly send `n: 1`, the
selected token-limit field, and intended sampling settings. Usage-required mode
sends `stream_options.include_usage=true`; timing-only mode omits it. Use typed
SDK extension facilities only for validated profile settings. Never send Ollama
`num_ctx`, `truncate`, `shift`, or lifecycle options to this API.

### Stream completion and measurements

- Use SDK stream iteration and check `stream.Err()`; always close the stream.
- Accumulate choice index 0 content. Preserve usage-only final chunks with
  `choices: []`, including those after the finish reason.
- Map `length` to `domain.DoneLength`; retain `stop` as the existing string-valued
  done reason. Handle other reasons explicitly. Existing short-run validity
  remains meaningful rather than failing every run due to unmapped strings.
- Require valid finish/completion evidence. A transport guard should detect an
  absent `[DONE]` when the selected contract requires it; nil SDK error alone is
  insufficient. Keep that guard framing-only, bounded, and unbuffered enough not
  to shift observed timing. SDK handles JSON/SSE decoding.
- Timestamp TTFT at the first non-empty generated content, not headers, role-only
  chunks, usage, or reasoning-policy metadata; use monotonic elapsed time.
- Define latency as request start through successful stream completion, including
  SDK/transport/queueing effects. Do not insert discovery/tokenization calls into
  the timed inference interval.
- Use authoritative usage counts, with `.JSON` field presence checks. Missing/null
  cached count differs from present zero; validate required counts and sane ranges.
- In `--usage required`, absent final counts fail the exchange. In `--usage off`,
  complete responses can yield TTFT/latency only; counts/rates stay unknown.
- Do not stop at finish reason before final usage. Malformed/embedded errors,
  oversized events, premature EOF, or cancellation cannot become successful runs.
- Bound event/error/body processing and test split frames, CRLF, comments, multiple
  events per read, and multiline data through the actual SDK adapter.

Progress counts are approximate content chunks, not tokens. Fix
`internal/ui/tui/runview/state.go` and `internal/ui/tui/runview/view.go`, which
currently derive token-limit progress and tok/s from callbacks: carry the unit
explicitly or show streaming activity/elapsed time. Final counts/rates use usage.

Map genuine transport unavailability to `llm.ErrUnavailable`, actual missing
served model to `llm.ErrModelNotFound`; a missing route is not a missing model.
Dial/DNS/TLS establishment failures are unavailable; reset/EOF/broken-pipe errors
after connection are ordinary interrupted requests, preserving a safe origin/cause
and allowing subsequent measured runs. A bounded oversized 404/405 discovery body
must not veto the explicit-ID fallback.
Keep 401/403, context overflow/invalid requests, 429, and server/protocol errors
recognizable with bounded sanitized details. No automatic inference retries,
provider fallback, or generation-field negotiation during measured requests.

## 7. Lifecycle and cache semantics

### External readiness

Managed Ollama behavior remains load/poll/warm-up/unload. Compatible endpoints
warm up once per selected model/session (existing 4-token warm-up), with readiness
tracked separately from ownership. Never call successful no-op lifecycle methods,
poll `/models` as a running-model list, unload served IDs, or shut down the endpoint.

Cleanup closes active streams and finalizes durable session status; it does not
clear an external cache. Preserve current error policy: unavailable transport
aborts, ordinary request failures can continue, and parent cancellation differs
from a single request timeout. Unsupported options fail before measured runs.

### Cache modes

| Mode | Initial compatible backend | Later validated profile |
| --- | --- | --- |
| `miss` | Existing unique-ID builder; cache-unverified if details are absent. | Verified cache statistics if per-response counts are supplied. |
| `continue` | Reject initially; do not silently replace with `miss`. | Existing primer/follow-up conversation after cache reuse/accounting validation. |
| `prefix` | Reject without real reset capability. | Dedicated vLLM server, explicitly permitted successful reset, primers, measured request. |

Rename `scenario.Executor.Reload` to `ResetCache`: Ollama retains its exact reload,
warm-up, and two-primer behavior; external profiles implement a real supported
reset or expose no capability. `continue` currently does not reload; its transcript
is reusable, but prefix/KV-cache reuse is not guaranteed by protocol compatibility.

Known unsupported modes are rejected before session creation. Configuration-
dependent checks may use the recorded warm-up; failure finalizes the session before
primers/runs. Recheck optional cache data on each later exchange. A low/zero cache
share in a prepared reuse scenario is a result, not automatically a failure.

Preserve Ollama's calibrated miss rule (cached share >1% **and** cached tokens >16)
and load allowance (1 second). Do not apply them universally. Unknown cached usage
means `cache-unverified`, not zero. Timing-only/unverified runs can supply client
metrics without establishing uncached prefill or cache savings. For vLLM, calibrate
any chat-template/block allowance with pinned live fixtures before asserting it.

## 8. Optional vLLM profile—not a baseline dependency

An explicit `--server vllm` can add known engine provenance. Only an explicitly
configured `--server-root` enables non-standard route access; the generic API URL
alone does not authorize administrative probing or establish a root URL.

| Operation | vLLM extension | Rule |
| --- | --- | --- |
| Version | `GET /version` | Optional; absent means unknown, not unusable inference. |
| Context metadata | `max_model_len` on model data | Optional; never treat `root`/`created` as weight identity. |
| Cached-token details | `prompt_tokens_details.cached_tokens` | Optional; configuration-dependent, not guaranteed by version. |
| Cache reset | `POST /reset_prefix_cache` | Explicit consent, validated release, dedicated server, and successful response required. |

Published **v0.31.0** source was inspected as a candidate vLLM contract; no running
GPU deployment has been verified for this plan. Pin live-test image/release/model
revision and establish supported versions experimentally.

Relevant distinctions:

- `--enable-prompt-tokens-details` controls optional cache-detail reporting.
- Chat usage does not contain Ollama-style prompt/eval/load durations.
- Current development cache endpoints are gated by `VLLM_SERVER_DEV_MODE=1` and
  documented as unsuitable for production use.
- Context limit, tokenizer/template, generation defaults, prefix-cache settings,
  dtype/quantization, and parallelism are deployment facts—not generic chat options.

For optional `prefix`, require `--allow-cache-reset`, dedicated/quiescent-server
instructions, and a validated response's `success`, not just HTTP 200. Use
`reset_running_requests=false` and `reset_external=false`. Never abort other
callers, clear an external cache, or reset during discovery/capability probing.
Reset failure cannot fall back to “assume empty”. Permission does not prove the
server has no other users; isolation is the operator's responsibility.

## 9. Metrics, reports, and provenance

### Measurements

Introduce one optional-value representation across adapters, domain, SQLite,
fakes, reports, and UI. Raw server durations/counts can be absent. Client TTFT is
absent if no generated content arrived; wall time is measured. Successful timing-
only requests need not invent usage. Persist absence as SQL NULL, not zero.

| Metric | Definition | Compatible endpoint |
| --- | --- | --- |
| Server decode tok/s | `eval_tokens / eval_duration` | Unavailable unless a separately validated extension reports it. |
| Server prefill ms/rate | Existing server-duration formulas | Unavailable; TTFT is not prefill duration. |
| Client TTFT | first generated content minus request start | Available when content arrives. |
| Client latency | stream completion minus request start | Available for completed responses. |
| Client completion tok/s | `completion_tokens / client_wall_seconds` | Available only with real usage; includes prefill/queue/network overhead. |
| Cached share | `cached_tokens / prompt_tokens` | Available only when both counts are known. |

Compute the same client definitions for Ollama where applicable. Never write
client wall time into `ServerMetrics.EvalDuration`, estimate tokens from chunks,
or derive a request's durations from aggregate Prometheus deltas. Completion
usage can include reasoning tokens; persist/label that policy rather than calling
it visible-text decode speed.

### Reports and UI

- Default grouping:
  `model,model-id,backend,engine,endpoint,server,measurement,workload,mode`.
  Backend is protocol; engine is known server kind; server remains version.
- Group on full identity/digest. `internal/report/group.go` currently groups on
  `ShortDigest()`; shorten for presentation only, especially with prefixed keys.
- Update `internal/app/run.go`'s `allTimeDimensions()` and `showSummary` independently
  of report defaults. Retain full identity/backend/protocol; scope external history
  to the endpoint. Keep historical Ollama rows with unknown endpoint eligible.
- Each metric has availability and its own sample count. `report.Valid` and table
  overall `N` count valid rows, not `Group.Decode.N`; update
  `internal/ui/format/columns.go` as well as report building.
- Apply availability to summaries, daily medians, recent values, charts, cached
  share, and live tables. Render absent values as `-`, never averaged zeros.
- Say “metric unavailable” rather than “no valid runs” for an unsupported metric.
  Keep backend/timing context visible in narrow TUI summaries.
- Measurement warnings (unknown cache/reasoning/version) do not automatically
  exclude otherwise valid client timings; separate them from exclusion flags.
- Custom grouping retains partitions for incompatible measurement protocols and
  displays the effective grouping; test `--group model,mode`.

### Identity and reproducibility

Persist backend, server kind/sources, sanitized endpoint, measurement protocol,
context policy/capacity, usage mode, effective generation/reasoning policy, and
operator-supplied deployment facts. Store no secrets or raw environment dumps.
`Session.Host` is the **Dyno client machine**, not remote GPU hardware.

Preserve the historical Ollama workload hash. Backend/protocol belong in
provenance/grouping; that hash alone does not prove tokenizer/template/reasoning
or deployment equivalence. Keep `--tag` useful for known deployment settings.

Separate model storage identity from digest:

- Ollama retains its known digest and existing identity.
- Compatible model plus operator revision: namespaced backend/model/revision key,
  marked operator-supplied, not server-verified.
- Without revision: session-scoped model identity. An alias does not safely prove
  immutable weights across sessions, even at the same URL.
- Unknown size, family, quantization, format, parameter count, and thinking support
  remain unknown. A model listing is not a content checksum.

## 10. SQLite migration

Use forward-only migrations after `0002_server_version.sql`; do not edit history.

1. `0003_backend_metadata.sql`: historical backend/server kind `ollama`, measurement
   protocol `ollama-server-v1`, truthful unknown endpoint/configuration defaults,
   optional version provenance, and explicit server-managed context policy.
2. `0004_model_identity.sql`: separate full model key, nullable digest, identity
   source/revision, and nullable details; exchanges reference the model key.
   Copy old digest into legacy key and known digest, preserving all associations.

Existing raw metric columns are nullable. Write/read them with presence preserved,
including successful timing-only exchanges; do not use `orZero` for measurements.
Historical optional cache counts flattened to zero cannot be reconstructed; retain
that documented legacy limitation rather than changing old values speculatively.

The identity migration rebuilds tables. Inspect existing transaction/FK behavior;
create/copy replacement tables, drop old child before parent, rename in a tested
order, recreate indexes, and check foreign keys before commit. Do not rely on
changing `PRAGMA foreign_keys` inside a transaction. Cover populated warm-up,
primer/run, failed, and interrupted-session data. Preserve WAL, atomic migrations,
append-only exchanges, and independent session finalization. Document backup and
older-binary incompatibility after upgrading the schema.

## 11. Implementation phases and acceptance gates

Each phase is reviewable and keeps the tree building with Ollama tests green.

### A — SDK/compatibility spike (CPU-only first)

- Pin a tested official SDK release; review module/binary impact and ambient
  credential behavior.
- `httptest` compatible endpoint: custom base path, auth, models unavailable,
  streaming content, usage-only final chunk, null/absent/present-zero fields;
  request JSON preserves explicit zero temperature/seed and exactly one token-limit
  field. Test required usage and timing-only requests separately.
- Verify exactly one POST on retriable failures, timeouts/cancellation, stream
  closure, EOF versus terminal marker, and bounded processing.
- Freeze API dialect, token-limit/usage policies, and fixture manifest.

**Gate:** the selected SDK demonstrably satisfies benchmark semantics. Any blocking
issue is reported for an explicit design decision, not hidden by runtime fallback.
This gate needs no GPU or paid endpoint.

### B — generic readiness, naming, and settings

Files: `internal/llm/client.go`, `internal/llm/llmtest/*`,
`internal/ollama/{client.go,response.go}`, `internal/domain/model.go`,
`internal/bench/{runner.go,lifecycle.go,executor.go,options.go}`,
`internal/scenario/{scenario.go,prefix.go}`, and app/CLI option handling.

Split lifecycle; add optional server facts/capabilities; preserve Ollama resets;
canonicalize summary/run names consistently; validate server-managed context and
external keep-alive. Add a fake that genuinely lacks managed-lifecycle methods.

**Gate:** complete warm-up/miss session against an external fake with no lifecycle
calls, and all existing Ollama ownership/cleanup/cancellation behavior preserved.

### C — availability, durable identity, and reports

Files: `internal/domain/{metrics.go,session.go,model.go}`, SQLite migrations and
recorder/source/codecs/query tests, `internal/report/*`, `internal/app/run.go`,
and plain/TUI formatters and views.

Implement optional measurements, client metrics, provenance, full identities,
populated migration tests, independent row/sample counts, safe default/all-time
grouping, truthful progress, and unavailable-value presentation.

**Gate:** mixed Ollama/compatible data, including timing-only successful runs,
reports no false zeros or merged identities; historical raw values remain intact.

### D — selectable compatible endpoint backend

Files: new `internal/openaicompat/*`, `go.mod`, `go.sum`,
`internal/app/{deps.go,run.go,models.go,report.go}`,
`internal/cli/{cli.go,run.go,models.go,report.go,values.go}`,
`internal/archtest/imports_test.go`, and help/golden tests.

Integrate SDK chat/discovery mapping, endpoint/key configuration, explicit dialect
controls, optional-listing fallback, offline filters, and generic metadata. Permit
SDK imports only in the adapter; no SDK/HTTP types enter core or UI.

**Gate:** URL + model is sufficient; metadata-free fixture and live vLLM endpoint
work in plain/TUI mode. No default resets, retries, paid-host fallback, or changes
to existing Ollama commands. Timing-only mode is usable without fake token rates.

### E — optional verified vLLM cache features

Files: new `internal/vllm` profile/operations, factory wiring, cache preparation,
validity policies, and fixtures.

On an isolated pinned GPU server, validate usage details enabled/disabled,
continuation transcript reuse, miss template/block allowance, primer count, and
reset success/failure. Add version/context discovery only as optional provenance.
Record server/image/model/tokenizer revisions and launch configuration.

**Gate:** cache modes reflect actual capabilities; unknown details stay unknown;
admin routes are never called without explicit profile/root/permission.

### F — docs and release checks

Update `README.md` provider/CLI tables, endpoint examples, metric formulas,
unknown-data and alias-identity caveats. Add sanitized compatible fixture capture
and optional vLLM capture; include SDK/server versions in manifests. Update
scheduled-install scripts/templates for backend/base URL without embedding keys;
retain Ollama defaults. Keep the original plan's server-extension cross-reference
current as implementation proceeds.

**Gate:** `just check`, race/migration tests, reviewed goldens, metadata-free
compatibility fixtures, and opt-in live-vLLM smoke test pass. Normal CI requires
no Python, GPU, running server, network, or API credits. `just script-test` is a
separate optional Python 3 gate for the capture helper; it runs with `-B -I` and
is not a prerequisite of `just check`.

## 12. Test matrix

| Layer | Required cases |
| --- | --- |
| Configuration | Ollama unchanged; flag-order independence; full API paths; conflicting URL flags; backend-scoped env; no implicit paid host/cloud credentials; empty key; offline reports. |
| SDK contract | Custom HTTP client; one POST with retries disabled; context cancellation throughout stream; body closure; size bounds; no origin-crossing credential leak. |
| Discovery | Supported listing; absent route with supplied IDs; explicitly skipped restricted listing; genuine missing model; authentication/transport errors not silently disguised as unsupported discovery. |
| Stream | Role-only chunks; split/combined frames; CRLF/comments/multiline data; finish before usage; usage-only chunk; absent/null/zero counts; missing finish/terminal marker; malformed/embedded errors; premature EOF. |
| Metrics | First-content TTFT; complete wall interval; required versus timing-only usage; unknown cache; no chunk token estimates; no synthesized server duration. |
| Runner | External warm-up once per model; no load/unload/poll/reset; unsupported options/modes; warm-up failure finalizes status; unavailable versus ordinary request failure; timeout versus Ctrl-C durability. |
| Storage | Populated legacy migrations; full identities/FKs/indexes; NULL round-trip; successful timing-only rows; unknown context/version; no secrets; append-only behavior. |
| Report/UI | Canonical Ollama historical names; different revisions/aliases/engines/endpoints separated by default; all-time/custom groups; correct row and per-metric N; unavailable charts; honest progress; narrow-layout labels; plain/TUI goldens. |
| vLLM opt-in | Pinned release; details/cache on/off; no default admin call; failed/false-success reset; no abort/external-reset flags; continuation/block allowance/reasoning/context provenance. |
| Architecture | Only compatible adapter imports SDK; core ports independent of SDK/profile; no plugin registry required. |

## 13. Target workflows

### Any compatible endpoint

```sh
dyno run --backend openai \
  --base-url https://llm.example.com/api/v1 \
  --models my-served-model --cache miss --num-predict 512 --runs 5

# Optional authentication: resolve a secret from this environment variable.
dyno run --backend openai --base-url https://llm.example.com/api/v1 \
  --api-key-env BENCH_API_KEY --models my-served-model

# Endpoint does not emit streaming usage: benchmark timings only.
dyno run --backend openai --base-url http://127.0.0.1:8000/v1 \
  --models bench-model --usage off

dyno models --backend openai --base-url http://127.0.0.1:8000/v1
dyno report --backend openai
```

### vLLM through the same adapter

Operator starts a known non-thinking model on an isolated server, ideally with
immutable model/tokenizer revisions, explicit generation settings, and cache
usage details enabled. No local launch is required by Dyno itself.

```sh
dyno run --backend openai --base-url http://gpu-host:8000/v1 \
  --server vllm --models bench-model --cache miss \
  --model-revision immutable-weight-revision --tag vllm-baseline
```

Later cache-stage example, only after isolated-server contract validation:

```sh
dyno run --backend openai --base-url http://gpu-host:8000/v1 \
  --server vllm --server-root http://gpu-host:8000 \
  --models bench-model --cache miss,continue,prefix --allow-cache-reset
```

## 14. Completion criteria and remaining decisions

- Existing Ollama defaults, raw historical measurements, and lifecycle guarantees
  remain intact.
- A compatible URL and model ID suffice without vLLM-only metadata/admin APIs.
- SDK communication is contained in the adapter; no home-grown full SSE stack.
- Exactly one measured request; no hidden retry/fallback/parameter negotiation.
- Counts, timings, and unknowns are truthful in storage and every report path.
- Model aliases, client/server hardware, reasoning policies, and timing definitions
  are not silently treated as equivalent.
- Reset is genuinely optional, permissioned, and engine-specific.

Resolve in the SDK/profile validation phases: selected SDK dependency impact;
controlled ambient-env initialization; strict stream-termination
guard; target-specific token-limit/sampling/reasoning options; restricted discovery
behavior; supported vLLM versions and cache experiments. Source inspection is not
a substitute for a successful live benchmark.

### References

- [Dyno original implementation plan](dyno-implementation-plan.md), section 20.3.
- [Official OpenAI Go SDK](https://github.com/openai/openai-go).
- [Inspected official release v3.71.2](https://github.com/openai/openai-go/releases/tag/v3.71.2).
- [Pinned SDK request options](https://github.com/openai/openai-go/blob/v3.71.2/option/requestoption.go).
- [Pinned SDK stream implementation](https://github.com/openai/openai-go/blob/v3.71.2/packages/ssestream/ssestream.go).
- [Pinned response-field presence](https://github.com/openai/openai-go/blob/v3.71.2/packages/respjson/respjson.go).
- [Pinned SDK dependency graph](https://github.com/openai/openai-go/blob/v3.71.2/go.mod).
- [Community Go OpenAI client](https://github.com/sashabaranov/go-openai), inspected release v1.43.0.
- [vLLM OpenAI-compatible server](https://docs.vllm.ai/en/stable/serving/online_serving/openai_compatible_server/).
- [vLLM serving/development endpoints](https://docs.vllm.ai/en/stable/serving/online_serving/).
- [Candidate v0.31.0 model/usage schema](https://github.com/vllm-project/vllm/blob/v0.31.0/vllm/entrypoints/serve/engine/protocol.py).
- [Candidate v0.31.0 cache reset](https://github.com/vllm-project/vllm/blob/v0.31.0/vllm/entrypoints/serve/dev/cache/api_router.py).
