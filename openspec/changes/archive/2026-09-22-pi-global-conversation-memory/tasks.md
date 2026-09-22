## 1. Global database path

- [x] 1.1 Change the default SQLite path to `~/.pi/agent/jev-cm.sqlite`, honoring `PI_CODING_AGENT_DIR` and letting `JEV_CM_SQLITE` override it
- [x] 1.2 Add `cwd`, `session_id`, and `role` columns to conversation rows, and skip an insert when the SHA-256 is already stored

## 2. Capture and recall

- [x] 2.1 Store every user and assistant prose turn from an agent run without calling Jev, and ignore tool output, tool-call arguments, empty text, and generated summaries
- [x] 2.2 Recall the `conversation` collection globally on the current prompt, skip Jev when the shortlist is empty, omit unranked and over-budget passages, leave stored rows in place when ranking fails, and cap injection at 2,000 estimated tokens
- [x] 2.3 Keep compaction recall on the same global file, including the `conversation` collection

## 3. Pi hooks and commands

- [x] 3.1 On `agent_end`, insert eligible originals using the Pi session id and cwd, with no Jev call
- [x] 3.2 On `before_agent_start`, inject admitted originals into the `jev-memory` section without replacing Pi's system prompt
- [x] 3.3 Add `/jev memory` and `/jev remember`, including the input dialog when remember has no text, and store manual text without calling Jev

## 4. Verification

- [x] 4.1 Add tests for the global path, dedupe, ignored tool output and summaries, cross-project recall, empty-store skip, unranked recall, and the 2,000-token omission using a fake System One server
- [x] 4.2 Run the tests and `openspec validate pi-global-conversation-memory --type change --strict --no-interactive`
