// Package openaicompat implements llm.Client with the official OpenAI Go SDK's
// streaming Chat Completions API. It benchmarks an existing compatible endpoint,
// without claiming model lifecycle ownership or verified cache operations.
//
// Usage-required requests require choice-zero finish, final usage-only counts,
// and a framed [DONE] marker. Timing-only requests still require finish and [DONE]
// but leave every server metric unknown. Counts include any server-accounted
// reasoning tokens; chunk progress is not tokens. TTFT starts at first nonempty
// generated content and wall time ends on successful stream completion. Reasoning
// deltas are intentionally not content: TTFT's persisted basis is
// first-nonempty-content, not first reasoning or inferred token. Server completion
// usage still includes any server-accounted reasoning tokens.
//
// The strict usage contract excludes usage in the finish chunk and rejects a
// total_tokens value unequal to prompt_tokens + completion_tokens. Those endpoint
// variants require explicit future validation, not silent negotiation during a run.
// Discovery is bounded by a separate 30s context deadline (caller cancellation or
// an earlier deadline wins); it does not impose a chat/header timeout. Diagnostics
// retain safe classifications/known transport causes, never raw SDK error graphs.
//
// Live vLLM continuation/cache-reset validation is deferred: declaring vllm only
// records operator provenance and never probes metadata or administrative routes.
package openaicompat
