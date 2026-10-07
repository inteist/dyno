# Endpoint implementation review resolution

Only confirmed behavior, safety, regression, documentation, and maintenance defects
were changed. All checks are offline. No staging, commits, or live GPU/cache
validation were performed.

## Agent-test isolation and installer safety

- Every installer test uses a temporary HOME, a test-only label, and a launchctl
  shim. The uninstall regression verifies the shim's target, rather than calling
  the real nightly job. Dry-run tests are isolated too.
- The installer rejects comma-only model selections, invalid duration syntax,
  revisions with multiple models, unsupported/duplicate cache modes, and control
  characters/overlong operator labels. Compatible cache remains `miss` only.
- Plist tests decode XML and compare the original URL/tag values. Escaping is
  verified with `<`, `>` and `&`, not just a well-formed URL.
- Template rendering scans only the template. Literal `{{TOKEN}}` text inside
  inserted values is not substituted again.
- Combining `--uninstall` and `--dry-run` is rejected without unloading or deleting
  anything. Uninstall still ignores unrelated stale provider settings.
- The port-validation expression no longer uses ambiguous `A && B || C` control
  flow. The script header again documents defaults.
- README documents launchd credential provisioning, login-session persistence,
  environment/process-argument exposure, and a private Keychain-wrapper alternative.

## Transport, discovery, and diagnostic behavior

- Dial/DNS/TLS establishment and HTTP/HTTPS mismatch failures remain unavailable.
  Reset, EOF, and broken pipe after connection are ordinary interrupted requests,
  with safe origin/cause details. TLS-handshake trace evidence distinguishes a
  handshake EOF from an already-connected request EOF.
- Production CLI→adapter→runner→SQLite regressions interrupt measured run 2 of 4
  after warm-up and verify all four rows and exactly five requests. The failed
  request is retained; runs 3 and 4 execute. Partial failure still exits nonzero.
- Oversized bounded `/models` 404/405 bodies no longer veto explicit-ID fallback.
  Authentication, other status codes, and actual body-read failures do not become
  discovery fallback.
- Dial timeouts retain origin and a safe timeout cause. Truncated stream bodies
  retain `truncated response body` rather than a generic protocol description.
- Missing-model diagnostics recognize Ollama-compatible `model "x" not found`
  without calling a missing `/v1/chat` endpoint a missing model.
- Redaction is idempotent around its placeholder; newline/tab/control separators
  become spaces. Case-insensitive and HTML-unescaped credential echoes are masked.
  Arbitrary raw transport errors and response bodies are still not persisted.
- Short credential values such as `1`, `v`, `v1`, and `/` no longer invalidate a
  normal `/v1` route. Credential-free URL requirements remain; a meaningful long
  credential embedded in a persisted path is still refused.
- Duplicate User-Agent setup and unreachable CheckRedirect code were removed.
  Contract-test naming now describes compatibility behavior, not the review process.

## Native and compatible presentation

- Production-like native fixtures include client wall time. Native compact tables
  omit client latency/rate columns even when those metrics exist, and omit MODEL-ID
  only when MODEL uniquely determines the full native identity. Distinct revisions
  for one name remain visible. Compatible identity/policy distinctions stay visible.
- Native end-of-run fitting drops warnings/client columns before exclusions and
  never drops PREFILL. Width regressions cover 72, 80, 100 and 120 columns.
- The 80×24 native report golden retains full model names, CACHED, EXCLUDED,
  `1 failed`, and `1 short`; redundant native facts no longer consume extra lines.
- Compatible summaries switch to wrapped stacked rows if fitting would conceal
  warnings. Identity/cache caveats remain visible at 120 and 160 columns.
- Live titles prioritize the complete session ID before provenance. Production
  runview and plain summary fixtures use the application's real dimension set.
  Grouping uses full MODEL-ID keys; native default plain presentation retains
  its historical DIGEST header.
- Progress callbacks/counts are named chunks. The TUI consumes Progress.Unit instead
  of ignoring it. One shared helper determines compatible presentation.
- Native default plain reports restore the historical five dimensions, DIGEST
  header, blank SERVER cells, six statistic columns, and four statistics tables.
  The post-run history summary also restores its four historical dimensions.
  Compatible reports intentionally add
  provenance/client statistics; custom dimensions remain explicit. Availability
  does not change header selection, and native warnings use mapped footers.
- Shared alias/revision labels retain readable endpoint suffixes in compact/TUI
  columns even when ENDPOINT is a dimension: its cells may be truncated. Only
  untruncated plain tables/statistics suppress redundant suffixes when full
  endpoint cells already distinguish the rows. Visible-row regressions cover
  60, 72, 80, 100 and 120 columns without relying on the selected-row detail line.
  Full-key suffixes remain a fallback for missing/identical endpoint facts;
  grouping uses full keys. Native warning labels omit blank SERVER facts.
- Revision/version labels are bounded printable text; both reject padding.
  Revisions additionally accept sha256:<64 hex>. Tags retain unrestricted length.
  Hash-like revisions are abbreviated to 12 characters plus `…` in cells, with the
  full constant revision in facts. Historical labels are control-cleaned too.

## Comparability and storage

- Copy-returning comparable/operational key lists classify every adapter-written key,
  enforced by an adapter contract test. Only explicitly operational keys are
  ignored; unknown keys are conservatively fingerprinted, preventing new settings
  such as enable_thinking from silently merging incomparable history.
- Migration refusal for orphan rows includes a `PRAGMA foreign_key_check` hint.
  It still preserves the database rather than silently deleting history.
- Storage documentation distinguishes per-migration atomic transactions from an
  all-migrations transaction, and raw NULL metrics from legacy empty/zero metadata.
- Shorthand-filter query-plan tests use 1,001 populated models with ANALYZE and
  test model-name constraints without requiring a particular index-name spelling.

## Release gate and optional capture helper

- `just check` is Go-only again: formatting, vet, staticcheck, and Go race tests.
  Optional `just script-test` uses Python 3 with `-B -I`; `__pycache__/` is ignored.
  README and the implementation plan agree on this dependency boundary.
- The capture helper validates retained index, finish reason, role, and content
  types. Deep malformed retained fields and parser RecursionError fail without
  tracebacks or partial capture files. Unknown private fields are discarded.
- The plan now distinguishes an optional unset default credential from an
  explicitly selected required variable; it also documents request recovery and
  oversized missing-route fallback.

## Intentional behavior retained

- Strict terminal framing and separate required streaming-usage evidence remain;
  timing-only compatibility is an explicit `--usage off` policy, not auto-negotiation.
- No SDK retries, provider fallback, external lifecycle no-ops, or unverified admin
  cache resets were introduced. Session failure on genuine unavailability remains.
- Visible-content TTFT is not native decode/prefill timing; chunk callbacks are not
  tokens. Missing raw metrics remain unavailable, and per-metric counts stay honest.
- Approved official SDK selection and optional capability interfaces remain. No
  speculative enum/interface refactor, generic predicate framework, or inverted
  historical availability default was needed. --server still declares the ENGINE;
  --server-version supplies SERVER, and DYNO_SERVER_KIND remains the documented
  installer setting. Native TUI charts may show measured client metrics while the
  historical plain schema omits them. The withdrawn redaction-performance concern
  was not changed.

## Verification

- Regression suite covers interruption recovery, oversized discovery fallback,
  safe diagnostics, operator labels, policy metadata, production native layouts,
  compatible caveats, fixed plain headers, and safe installer actions.
- UI goldens were regenerated and inspected, including 80×24 native tables.
- `just check` (formatting, vet, staticcheck, all Go race tests) — passed.
- `just script-test` — all twelve offline Python safety tests passed with `-B -I`.
- ShellCheck and `bash -n scripts/install-agent.sh` — passed.
- `git diff --check` — passed.
- Live vLLM GPU/cache behavior remains deliberately unclaimed.

## Further confirmed hardening

- Invalid UTF-8 credentials are rejected in both CLI and adapter construction.
  Regex construction is fallible defensively, never a credential-triggered panic.
  Split control/format-character echoes redact before whitespace cleanup; U+2028
  and U+2029 become safe separators. Endpoint errors name --host, --base-url or
  OPENAI_BASE_URL according to the actual input source.
- Connected-request recovery is bounded by the named three-failure limit per
  external model, not per session. That model's remaining requests are skipped;
  other models run in model and round-robin order. The session stays failed.
  Scheduling waits only if a runnable step remains, not before a blocked tail.
  Fake-clock regressions cover the single-model and mixed-order cases.
  Dial/DNS/TLS failures and failed warm-ups still abort immediately.
- Missing-model messages recognize IDs containing route/endpoint substrings;
  actual route errors stay distinct. Guard errors retain the openai chat prefix.
- Historical revision facts sanitize controls. Plain facts occupy one separated
  block, retaining endpoints even outside grouping dimensions and omitting empty
  endpoint placeholders. Numeric revisions, including 40-digit values, remain
  literal unless explicitly sha256-prefixed. Varying TUI endpoints remain visible.
- The application and runview fixtures consume one shared history-dimension
  function, not independently copied literals.
- Installer checks UTF-8 labels using the Go Unicode printability table and
  pinned macOS /usr/bin/awk under LC_ALL=C. This depends on that AWK's byte
  semantics; arbitrary PATH-selected, always-UTF-8 implementations are not used.
  Counts are Unicode characters; controls, separators and private-use values are
  rejected. Go verifies every codepoint in the table. Runtime validator tests
  check all printable range endpoints, representative gaps, and malformed UTF-8,
  including C1 81 and three-/four-byte overlong encodings.
- Interval checks retain exact integer arithmetic and mirror Go leadingFraction
  truncation and float64 fractional rounding. The previously accepted rounding
  case 2562047.78801521550222222222h now fails like Go. Parity tests compare both
  acceptance and nanoseconds around hour/minute/second limits and for .5s,
  1500µs and 1.5ms, rather than testing only whether small values parse.
  The exact Go maximum remains accepted; its next nanosecond is rejected.
  Only existing shell/system-AWK tools are needed; Go regenerates the Unicode
  table, and normal CI still requires no Python.
- DefaultKeepAlive now saturates at the duration maximum rather than overflowing
  within its five-minute margin; ordinary defaults are unchanged. A native
  interval exactly at the ceiling still cannot satisfy keep-alive > interval.
- Installer regressions cover locale-valid labels, invalid Unicode, version and
  revision padding/length, exact SHA-256 spelling, anchored launchd-label rejection,
  interval units/limits, model controls and isolated XDG settings.
- Capture files are UTF-8-validated before directory/file creation; lone
  surrogates leave no partial chat.sse. Role/finish enums are validated and a BOM
  preserves the first frame. Stale CLI/storage documentation was corrected.
- README duplication was not present in the current tree; retained the existing
  single Use example block, removing only the confirmed leftover <br />.
