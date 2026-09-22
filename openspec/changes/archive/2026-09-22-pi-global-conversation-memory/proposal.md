## Why

Pi has no global memory of its own, and this plugin only recalls durable text during compaction from a database that defaults to the current project directory. A fact learned in one project is invisible in the next session. Conversation memory should be one store for every Pi session. Every conversation turn is written locally. Jev is used only when a later turn loads matches out of that store.

## What Changes

- Add one user-level conversation memory database that every Pi project and session shares.
- After each agent run, store every original user and assistant prose turn. Do not ask Jev before writing.
- Ignore tool results, tool-call arguments, empty text, and generated summaries. Ignored items are not inserted and are not sent to Jev.
- Before each agent run, recall matching originals from that global store and attach them to the system prompt as their own section. Jev ranks that shortlist. Jev is not called when the shortlist is empty.
- **BREAKING**: the default SQLite path moves from `data/jev-cm.sqlite` in the working directory to `~/.pi/agent/jev-cm.sqlite`. `JEV_CM_SQLITE` still overrides it. Existing project-local files are not migrated automatically.
- Add `/jev memory` and `/jev remember` so the store can be inspected and written from inside Pi. `/jev remember` uses the same write path, with no Jev gate.

## Capabilities

### New Capabilities

- `conversation-memory`: Capture every original Pi conversation turn into one global SQLite store, ignore non-conversation bytes, and inject Jev-ranked originals before the next turn.

### Modified Capabilities

- None. This repository has no published specs under `openspec/specs/`.

## Impact

- Pi extension hooks `agent_end` and `before_agent_start`, in addition to the existing compaction hook.
- The Go store, recall path, and CLI use the new default path. Elision bytes and conversation rows share that file.
- A TypeSafe or OpenCode Go key is required only for recall ranking. Judgment excerpts leave the machine. Stored bodies stay local. A missing key does not block writes.
- Manual `remember`, `recall`, and `import-source` commands keep working and read the same global file.
