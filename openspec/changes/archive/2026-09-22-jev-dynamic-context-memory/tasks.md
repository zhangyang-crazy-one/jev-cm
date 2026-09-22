## 1. Project skeleton

- [x] 1.1 Add a Go module for the client, context engine, memory store, and CLI, plus the Pi extension that execs that binary. Tests use the standard library and a pure-Go SQLite driver.
- [x] 1.2 Add configuration for provider (`typesafe` or `opencode-go`), model override, thresholds, protected tail, token budget, and the local SQLite path
- [x] 1.3 Reject an OpenCode Go chat-completions base URL during configuration load

## 2. System One client

- [x] 2.1 Implement `evaluate(state, questions)` for the official key and the OpenCode Go key, including bearer auth from the matching environment variable
- [x] 2.2 Validate `noul`, `choice`, and `score` questions locally, require `instructions`, and return answers under the caller-supplied keys
- [x] 2.3 Map default model ids per provider and surface the provider-reported model version on the result
- [x] 2.4 Split a question batch at the 32K estimated-token cap, record the estimator name, and merge answers without duplicating keys
- [x] 2.5 Return authentication, timeout, and parse errors without fabricating probabilities

## 3. Local stores

- [x] 3.1 Create the SQLite schema for elided tool output and durable memory, including SHA-256, source id, probability, and model version
- [x] 3.2 Store and page original tool-output bytes by stable pointer, and return not-found for an unknown pointer
- [x] 3.3 Add FTS5 shortlist and atomic replace-by-source-id inside one collection

## 4. Dynamic context

- [x] 4.1 Assemble the frozen prefix so compaction cannot rewrite it, and report used tokens, budget, and estimator name
- [x] 4.2 Score eligible tool output with Jev and elide only scores below `drop_threshold` (default 0.25), preserving message count and order
- [x] 4.3 Protect the configured recent tail from scoring and elision
- [x] 4.4 Implement expand so a known pointer page matches the stored original and does not rerun the tool
- [x] 4.5 Assemble a fresh window from the prefix, pinned decisions, selected originals, and pointers, with no generated summary
- [x] 4.6 On Jev failure, leave the session unchanged and signal host fallback

## 5. Durable memory

- [x] 5.1 Gate inserts on one Noul with the documented polarity and default `remember_threshold` of 0.75
- [x] 5.2 Refuse summary bodies, refuse writes when Jev is unavailable, and keep that status distinct from a low-score refusal
- [x] 5.3 Rank the FTS shortlist with per-candidate Nouls, return whole originals in score order, and mark the rest `over_budget`
- [x] 5.4 Return an empty selection as success, and return the shortlist as `unranked` when recall judgment fails

## 6. Pi extension

- [x] 6.1 Register `session_before_compact` and run the elision plan before Pi writes a continuation summary
- [x] 6.2 Return selected originals and pointers in Pi's compaction result, and document that judgment excerpts leave the machine while store bodies stay local
- [x] 6.3 Verify against the installed Pi 0.84.2 that a returned compaction result skips the host summary call, and record the result in the change
- [x] 6.4 Add an explicit purge path that is off unless requested, and document uninstall as removing the extension without deleting SQLite

## 7. Verification

- [x] 7.1 Add tests that cover each scenario in `systemone-client`, `dynamic-context`, and `durable-memory` using a fake System One server
- [x] 7.2 Run the tests and `openspec validate --change jev-dynamic-context-memory`

## 8. Host window fit

- [x] 8.1 Treat a tool output larger than the protected tail as eligible, and move Pi's keep point forward until the summary plus the kept span fits the host safe line
