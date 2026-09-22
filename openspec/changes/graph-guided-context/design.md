## Context

The local Codex checkout at `codex-rs` manages a model window in four layers. This change copies the shape of that window and refuses the part that writes a summary.

Codex keeps a world-state prefix (instructions, rules, environment, skills, permissions). Compaction does not summarize it. A pre-turn compaction clears it and the next turn renders it again. A mid-turn compaction inserts it before the last real user message. Window accounting in `context_window.rs` compares active tokens with the model context window and with `model_auto_compact_token_limit`. The `BodyAfterPrefix` scope subtracts the prefix baseline so growth is measured on the body. Each window has a first id, a previous id, and a current id.

Compaction has three implementations:

- Local responses compaction runs a summarization prompt and replaces history with the newest user messages that fit in 20,000 tokens, then a summary that starts with a fixed prefix. Tool calls are not what that replacement keeps.
- Remote compaction posts `/v1/responses/compact`. The replacement drops tool and function items. Remote v2 keeps user, developer, and system messages from the tail up to 64,000 tokens and appends the compaction output. If the history still exceeds the window, function outputs are replaced from the end with a fixed truncation string. The original tool bytes are not stored.
- Token-budget compaction does not call a summarizer. It installs a fresh context window that contains the current world state. Pre-compact hooks can stop it.

jev-cm already freezes a prefix, stores original conversation turns, ranks a full-text shortlist with Jev, and can replace a low-scoring tool output with a pointer during `prepare`. Two gaps remain. Recall never sees a passage that shares no token with the query, so Jev never scores it. Elision uses an internal 32,000-token budget times `pressure_ratio`, and the live elision table is empty, so that Jev call has not been participating in window management. Pi's `before_agent_start` hook can add a system-prompt section. It cannot rewrite live messages. Message replacement belongs in `session_before_compact`.

The bundled `modernc.org/sqlite` v1.59.0 is SQLite 3.53.4. That build has no graph virtual table. SQLite documents `WITH RECURSIVE` as the way to query a graph stored in ordinary tables.

## Goals / Non-Goals

**Goals:**

- Seed recall with full-text search, expand it through stored dependency edges, and let Jev rank that neighborhood.
- After a captured turn is stored, ask Jev whether it depends on a small set of older passages, and record the admitted links.
- When Pi compacts because the host window does not fit, ask Jev which dropped tool outputs become pointers, and fill the fresh window with graph-ranked originals.
- Leave stored originals in place when Jev is unavailable.

**Non-Goals:**

- Adopting Codex's summarization prompt, summary prefix, or remote `/v1/responses/compact` endpoint.
- Rewriting the live Pi transcript on `before_agent_start`.
- A SQLite graph virtual table, a vector index, or a graph query language beyond recursive common-table expressions.
- Calling Jev from `/jev remember`. That command stays a no-Jev write. A later captured turn can still link to those rows.
- Putting every stored passage, or every node in a connected component, into the session.

## Decisions

### 1. Nodes are conversation rows, edges are a new table

`memory.id` is the node. A new `memory_edge` table stores `src`, `dst`, `kind`, and the admitting probability. `kind` is only `related`. `src` is the newer row and `dst` is the older row. The primary key is `(src, dst, kind)`.

One edge kind is enough because recall asks Jev about relevance again. Separate `continues`, `decides`, and `mentions` edges would multiply link Nouls without changing the neighborhood Jev sees.

The alternative of a graph virtual table does not exist in the bundled SQLite. Recursive queries over this table are the mechanism SQLite documents for graphs.

### 2. Link after the row is committed, against at most eight seeds

Capture still inserts original bytes with no Jev call and no write gate. After the new rows commit, full-text search returns at most eight seeds for the new text. One `Evaluate` sends one Noul per seed. The instruction says a high probability means the new turn depends on that passage: the same task, a decision it relies on, or a fact it cites. A low probability means the texts only share common words.

An edge is inserted when the probability is at or above `recall_cutoff` (default `0.5`). Duplicate text does not insert a row and does not call Jev. Tool output, tool arguments, empty text, and generated summaries are not nodes and are not link seeds. If Jev errors, times out, or the key is missing, the new rows remain and no edge is added.

### 3. Recall walks two hops, then Jev ranks at most twenty passages

Full-text search, including the CJK bigram index, produces the seeds. A recursive query follows `memory_edge` in both directions for at most two hops. The neighborhood is the seeds plus those nodes, deduplicated, capped at `shortlist_limit` (default 20). Seeds are kept ahead of neighbors. Neighbors are ordered by descending edge probability.

Jev then scores one recall Noul per neighborhood member, with the same `0.5` cutoff, unranked omission, and 2,000-token injection budget as today. An empty seed list does not call Jev and does not walk the graph. A graph with no edges returns the full-text seeds, so current behavior remains for unlinked rows.

### 4. Compaction uses the host window and does not summarize

`prepare` receives the host safe line the extension already computes: model context window minus Pi's `reserveTokens`, or `keepRecentTokens` when the window is unavailable. Pressure is reached when the span being compacted estimates at or above `pressure_ratio` times that safe line. The internal 32,000-token budget remains the fallback when the extension has no safe line.

Eligible tool outputs are those outside the protected tail. Jev's existing keep Noul and `drop_threshold` of `0.25` decide pointer versus original. The fresh window is the frozen prefix, elision pointers, and passages admitted by the graph recall of the latest user prose. It has no summary field and no model-authored recap. A Jev failure still returns `fallback` for the elision judgment and leaves stored rows in place. The extension's existing rule stands: a keep point that already fits, and a plan with nothing to inject, leaves Pi's own compaction in place. The host-window cut still happens when Jev is unavailable.

### 5. Codex window identity is not copied into the prompt

Codex shows thread and window ids, plus tokens remaining, as a developer fragment. jev-cm already reports used tokens, budget, and estimator on the compaction plan. Adding a second developer fragment would compete with `jev-memory`. This change does not add window ids to the system prompt.

## Risks / Trade-offs

- [Link calls send seed excerpts off the machine after every captured turn] → Cap the seed list at eight and send one request. No key means no call and no edge.
- [A loose link pulls an unrelated neighbor into the shortlist] → The edge cutoff is `0.5`, the walk is two hops, and recall Jev can still reject the neighbor.
- [Two hops on a dense graph exceeds the shortlist] → The neighborhood is capped at `shortlist_limit` before Jev is called.
- [Host windows are large, so `0.65` of the safe line rarely fires] → The ratio stays configurable. The extension still cuts an oversized kept span before `prepare` returns.
- [Existing rows have no edges until a newer turn links to them] → Recall falls back to the full-text seeds, so current matches keep working.

## Migration Plan

Opening the database creates `memory_edge` if it is missing. Existing `memory` rows are unchanged and start with no edges. No backfill scan calls Jev. Rollback is to stop reading edges; the conversation rows and the full-text index stay valid. Uninstalling the extension does not delete the file.

## Open Questions

- None. Link batch size, hop limit, and edge kind are fixed above so implementation does not invent a second threshold.
