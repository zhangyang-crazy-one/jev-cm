## Context

This repository is a new OpenSpec project. The product is a local dynamic-context plugin and memory system whose judgments come from Jev, TypeSafe's System One model. Jev accepts one `state` and many typed questions (`noul`, `choice`, `score`) and returns probabilities. It does not generate prose, and its weights are not available to run locally.

Two hosts can serve that same contract today:

- Official: `POST https://api.typesafe.ai/v1/systemone` with `jev-1.13.0` or `jev-latest`.
- OpenCode Go key: the same System One contract at `POST https://opencode.ai/zen/v1/systemone`, with `jev-1.13` or the limited free id `jev-1.13-free`. The bearer is `OPENCODE_GO_API_KEY`, or `OPENCODE_API_KEY` when the Go-specific variable is unset.

OpenCode Go's coding-model catalog is a different API and does not serve Jev, so a Go key is not sent to `/zen/go/v1/chat/completions`. Published context limits disagree (32K in several guides, 64K on one catalog entry), so the client treats 32K tokens as the hard state budget.

Codex's token-budget work, still behind `Feature::TokenBudget`, replaces summary compaction with a fresh window plus retrieval of original history. Pi 0.84.2 fires `session_before_compact` before its own summary model. Returning a compaction result from that hook skips the summary call. That hook is the integration point.

## Goals / Non-Goals

**Goals:**

- One client interface over the official Jev key and an OpenCode Go key, both speaking System One.
- A context engine that keeps original bytes, moves low-value tool output behind stable pointers, and can open a fresh window from scaffold plus retrieved originals.
- A local memory store whose writes and reads are gated by Jev probabilities.
- Fail open when Jev cannot be called: context compaction falls back to the host, and memory writes are refused rather than stored unverified.

**Non-Goals:**

- Calling OpenCode Go chat completions, or any generative model, to rank or rewrite context. The Go key is only a credential for System One.
- Shipping Codex or Claude Code adapters in this change.
- Embedding indexes, vector databases, or self-hosting Jev.
- Calibrating decision thresholds on a private benchmark. Defaults are conservative and configurable.
- Importing transcripts from other agents.

## Decisions

### 1. Two transports, one evaluation call

The client exposes `evaluate(state, questions)` and a provider setting: `typesafe` or `opencode-go`. `opencode-zen` is accepted as an alias of `opencode-go`. The official provider uses `TYPESAFE_API_KEY`. The Go provider prefers `OPENCODE_GO_API_KEY` and falls back to `OPENCODE_API_KEY`. The request body stays the TypeSafe contract: `model`, `state`, `questions`.

Alternatives considered:

- A single OpenAI-compatible client pointed at OpenCode Go. Rejected because Go does not implement `/v1/systemone`.
- Automatic failover after a failed request. Rejected because a timeout can still have been billed, and the two providers use different model ids. The caller selects the provider up front.

### 2. Jev selects original text and never writes it

Every keep, drop, truncate, write, and recall decision is a typed question. The text that enters the window or the memory store is copied from the source bytes. Truncation keeps a head of the original plus a pointer. There is no summary field in storage.

Alternatives considered:

- Ask Jev for a keep/summarize/drop digest and store the digest. Rejected because Jev cannot author text, and a measured handoff built from that kind of digest recalled less than the plain transcript.
- Ask a coding model to summarize, then ask Jev whether the summary is faithful. Rejected because the summary can still invent detail and it rewrites the cached prefix.

### 3. Three context regions

- Frozen prefix: instructions and rules. It only grows by explicit user or scaffold changes. Compaction never rewrites it.
- Work area: the newest messages, including a protected tail of recent tool output.
- Elision store: original tool payloads moved out of the model-facing window. The window keeps the tool-call identity, order, and a stable pointer line. `expand` returns the stored bytes in pages.

A fresh window contains the frozen prefix, pinned decisions, Jev-selected memory passages, and pointers for anything still elided. It does not contain prior user or assistant prose unless that prose was itself selected as an original passage.

### 4. SQLite, with FTS only as a shortlist

Elided tool output and durable memory live in one local SQLite file under the project, in separate tables. Durable rows store the original text, SHA-256, source id, and the Jev probability that admitted them. Recall runs FTS5/BM25 to produce a bounded candidate set, then one Noul per candidate. Paragraphs at or above the cutoff fill the token budget in score order. A relevant paragraph that does not fit is reported as `over_budget` and is not cut mid-paragraph.

Alternatives considered:

- Score every stored paragraph with Jev on each request. Rejected because cost and the 32K state cap grow with the whole collection.
- Vector search first. Rejected for this change to avoid an embedding dependency and a second model.

### 5. Conservative thresholds and an explicit Noul polarity

Tool output is removed from the window only when Jev's probability that it is still needed is below `drop_threshold` (default 0.25). Memory is stored only when the probability that it is durable, non-obvious, and useful on a later turn is at or above `remember_threshold` (default 0.75). Question text states which outcome is the high probability, and tests lock that polarity.

### 6. Batch inside 32K and preserve host message identity

Questions that share a state go in one request until the estimated state plus questions reach 32K tokens, then the client splits. The estimator is recorded on the response because it may differ from the provider tokenizer. Compaction replaces payload text in place. It does not delete or reorder host messages, so a host that captured the message count before the hook still indexes valid rows.

### 7. Go library, Pi extension

The client, stores, context engine, memory gate, and CLI are Go. Pi loads extensions as JavaScript, so `pi/index.js` only registers `session_before_compact` and execs the `jev-cm` binary. SQLite comes from a pure-Go driver so the build does not need cgo.

### 8. Pi hook, fail open

The extension registers `session_before_compact`. It runs the Jev plan first. When the plan is `compacted` and has injection text, the extension returns that text in Pi's required `summary` field together with `firstKeptEntryId` and `tokensBefore`. Pi then stores the extension result and does not call its summary model. The field is the host's storage slot; the bytes are pointers and original passages. If the key is missing, the binary is missing, the call times out, or the plan has nothing to inject, the extension returns no result and Pi continues with its own compaction. Memory writes take the other fail-closed side: an unavailable Jev returns `unavailable` and stores nothing, because this repository has no host memory path to fall back to. Reads still return the FTS shortlist marked `unranked`.

## Risks / Trade-offs

- [Pi stores extension compaction in a field named summary] → Fill that field with pointers and original passages. Do not send a Jev-written recap. Keep the elision store as the source of truth.
- [32K versus 64K provider catalogs] → Hard-cap requests at 32K until a live response proves a higher limit for the selected model version.
- [Uncalibrated probabilities] → Defaults keep tool output unless the score is low, and refuse memory unless the score is high. Both thresholds are configuration, not constants buried in prompts.
- [FTS misses a paraphrased memory] → Acceptable for v1. The shortlist size is configurable so a collection can raise recall without scanning the corpus.
- [Prompt-cache invalidation] → Never rewrite the frozen prefix or the middle of an already cached region; only the work-area tail is elided.
- [Judgment state leaves the machine] → Send only the excerpt required for the question. Store bodies stay local. Document this in the plugin README.
- [Author-reported savings from other Jev repos are not evidence] → This change does not cite them as acceptance criteria. Tests check behavior, not token-reduction percentages.

## Migration Plan

Installing the extension is additive: `pi -e ./pi/index.js` or `pi install` of this directory. Rollback is removing that extension and restarting Pi. Elision and memory SQLite files are left on disk so a later reinstall can still expand old pointers. Uninstall does not delete them unless the user passes an explicit purge flag.

## Open Questions

- Which tokenizer should the budget use? The design records the estimator name `utf8-div-4` and does not assume it matches Jev or the coding model.
- Codex skill and MCP packaging stays a follow-up change after the Pi path is real.

The compaction-hook question is closed for Pi 0.84.2. Returning `compaction` from `session_before_compact` skips the summary model. See `pi-hook-verification.md`.
