## Why

Coding agents lose long-task context by asking a generative model to summarize history. Those summaries invent details, break the prompt cache, and then get stored as if they were facts. Jev can score what to keep, drop, or retrieve, but it cannot write the replacement text, so the window and the memory store should hold original bytes only.

## What Changes

- Add a System One client that calls Jev with either the official TypeSafe key or an OpenCode Go key, using the same `state` and typed-question contract.
- Add dynamic context management modeled on Codex token-budget windows: show remaining budget, move low-value tool output out of the live window, leave a stable pointer, and open a fresh window that re-injects the frozen scaffold plus retrieved originals.
- Add a local memory store that accepts a write only when Jev judges the entry durable, and recalls verbatim passages ranked for the current request.
- Add a Pi extension on `session_before_compact` that can supply the fresh window and skip Pi's summary model, and fail open to Pi's built-in compaction when Jev is unavailable.
- Send the OpenCode Go key to the System One endpoint. Do not call OpenCode Go's chat-completions catalog for Jev.

## Capabilities

### New Capabilities

- `systemone-client`: Send one shared state and many Noul, Choice, or Score questions using either the official Jev key or an OpenCode Go key, and return typed probabilities with the actual model version.
- `dynamic-context`: Keep a frozen prefix, prune or restore tool output by Jev score without rewriting it, and start a fresh context window under an explicit token budget.
- `durable-memory`: Store and recall source-backed original text locally, with a Jev write gate and a budgeted relevance ranking at read time.

### Modified Capabilities

- None. This repository has no existing specs.

## Impact

- New library code for the System One client, context engine, and SQLite memory store.
- New Pi extension registered through `session_before_compact`.
- Runtime dependency on a TypeSafe API key or an OpenCode Go API key. No model weights. The Go key is sent to System One, not to the Go coding-model endpoint.
- Local SQLite files for elided tool output and durable memory. Nothing is sent except the state and questions required for a Jev judgment.
- Codex-style skills or MCP tools are out of scope for this change; the host integration is the Pi extension.
