## Why

Full-text recall still misses a stored turn when the query and the passage do not share a token, and the dynamic-context path only asks Jev inside Pi compaction, using an internal 32,000-token budget. On the current database that elision table is empty, so Jev has not been helping manage the live window. Codex already separates a frozen prefix, a measured context window, tool-output trimming, and a fresh window. This plugin should use that shape, keep original text instead of a model summary, and let a SQLite graph decide which originals Jev ranks.

## What Changes

- Add a memory graph in the existing global SQLite file. Each conversation row is a node. Directed edges point from a newer turn to an older passage it depends on. Recall walks that graph with a recursive query, which is SQLite's documented way to query a graph. The bundled SQLite 3.53.4 build has no graph virtual table.
- After a turn is stored, ask Jev once, in one request, whether the new node depends on a bounded full-text seed. Store an edge only when the score is at or above the recall cutoff. A missing key or a failed call leaves the node in place and adds no edge.
- Change recall so the shortlist is that seed plus its graph neighborhood, capped at the existing shortlist limit. Jev then ranks the neighborhood. An empty seed still skips Jev. Admitted text is still the stored original.
- During compaction, measure pressure against the host context window, not only the internal token budget. Ask Jev which older tool outputs outside the protected tail can be replaced by pointers. Assemble the fresh window from the frozen prefix, those pointers, and graph-ranked originals. Do not call a model to write a summary.

## Capabilities

### New Capabilities

- `memory-graph`: Persist conversation nodes and directed dependency edges in the global SQLite file, and traverse them with a recursive query before Jev ranks recall.

### Modified Capabilities

- `conversation-memory`: Recall ranks a graph neighborhood instead of the raw full-text shortlist. Inserting original bytes still has no Jev write gate; a following link step may call Jev.
- `dynamic-context`: Tool-output elision uses the host context window when Pi compacts. The fresh window draws its passages from graph-ranked recall.

## Impact

- Go store, memory, engine, prepare service, and the Pi extension's compaction and capture hooks.
- The same `jev-cm.sqlite` file gains an edge table. Existing conversation rows become nodes with no edges until a later link.
- Extra Jev calls are the link batch after a successful capture, the existing per-candidate recall Nouls on the neighborhood, and elision Nouls when a compaction is under host-window pressure. Original-text writes still succeed without a key.
- Recall and compaction prompts stay local except for the shortlist and tool-output excerpts Jev scores.
