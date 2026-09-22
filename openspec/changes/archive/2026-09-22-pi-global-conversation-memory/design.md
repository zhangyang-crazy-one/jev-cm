## Context

Pi 0.84.2 has no global memory file and no memory slash command. This plugin already stores elided tool bytes and durable passages in SQLite, but the default path is `data/jev-cm.sqlite` relative to the process working directory, and the Pi extension only recalls that store while handling `session_before_compact`. A fact captured in one project therefore never appears in another session.

The extension is installed at user scope (`pi install` of this repository). `before_agent_start` can add a named section to `systemPromptOptions.sections` without replacing the system prompt. `agent_end` receives the messages produced by that run. `sessionManager.getSessionId()`, `getSessionFile()`, and `getCwd()` identify the session.

An earlier draft of this change gated writes with a 0.75 Noul. That draft is superseded. Writes store the original turn. Jev runs only on the way out.

## Goals / Non-Goals

**Goals:**

- One SQLite file for every Pi project and session, defaulting to `~/.pi/agent/jev-cm.sqlite`.
- Write every original user and assistant prose turn after an agent run, with no Jev call.
- Ignore tool output, tool-call arguments, empty text, and generated summaries.
- Load ranked originals into the next turn, across projects, before the model answers.

**Non-Goals:**

- Reading a Pi-provided global memory file. Pi does not have one.
- Storing tool output in the conversation collection. Tool bytes stay in the elision table.
- Asking Jev or another model to write a memory summary.
- Automatic migration of an existing `data/jev-cm.sqlite`.
- Per-project recall filters. Coverage is global. The project path is recorded so the injected text can name its origin.
- A Jev write gate. Low-value turns are still stored. Ranking happens at recall.

## Decisions

### 1. One file under the Pi agent directory

The default path is `~/.pi/agent/jev-cm.sqlite`, beside `jev-cm.json`. `PI_CODING_AGENT_DIR` changes the directory. `JEV_CM_SQLITE` still wins when set. Elision rows and conversation rows share the file so a pointer created in any project can still expand.

The working-directory default is rejected because the extension's child process inherits Pi's project cwd, which would split memory by project.

### 2. Conversation rows keep the original text

New columns on `memory`: `cwd`, `session_id`, `role` (`user` or `assistant`). Collection is `conversation`. `source_id` is the Pi session id, or `manual` for `/jev remember`. The stored body is the original message text. SHA-256 dedupes globally: a repeated paragraph does not insert a second row and does not call Jev.

Tool results, tool-call arguments, and empty text are ignored. A body marked as a generated summary is ignored without a network call.

### 3. Write on agent_end, with no Jev call

`agent_end` runs after the loop finishes, so a turn that is still calling tools is not stored mid-flight. Each remaining user or assistant text is inserted as its original bytes. Jev is not called. A missing key, a timeout, or a later recall failure does not delete stored rows.

Writes finish before the extension handler returns, so a crash after the reply can drop that turn. That is accepted. The next successful run does not replay the missed turn.

### 4. Read on before_agent_start, global and budgeted

`before_agent_start` recalls the collection `conversation` with the current user prompt. The search covers every project. An empty shortlist returns immediately and does not call Jev. Otherwise the FTS shortlist is ranked with one Noul per candidate, cutoff `0.5`. Passages marked unranked are not injected, and the stored rows stay. Admitted passages are original text, ordered by probability, each labeled with session id and cwd.

Injection uses `systemPromptOptions.sections["jev-memory"]`. The handler does not set `systemPrompt`, so Pi's own prompt stays in place. The section budget defaults to 2,000 estimated tokens (`utf8-div-4`), separate from the compaction budget, so memory cannot crowd out the coding prompt. Passages that do not fit are omitted whole.

Compaction continues to recall this same file. It includes the `conversation` collection as well as `default`, so a compacted window can still carry global memories.

### 5. Commands

`/jev memory` prints the database path, conversation row count, and whether the key is set. It does not print stored bodies. `/jev remember` takes the following text, or an input dialog when the text is absent, and stores it with source id `manual` and role `user`. It does not call Jev. The CLI commands `remember`, `recall`, and `import-source` use the global path with no extra flag. `remember` on this collection follows the same no-Jev write path.

## Risks / Trade-offs

- [A sentence from one project appears in another] → That is the requested coverage. The label includes cwd and session id so the model can see the origin. Recall still has to rank it above `0.5`.
- [Jev sees the shortlist text] → Only the shortlist excerpts are sent, and only when a prompt is being matched. The SQLite file is not uploaded. Writes send nothing.
- [Default path change hides an old project database] → Do not delete or move `data/jev-cm.sqlite`. Document the new path and the `JEV_CM_SQLITE` override.
- [agent_end stores a long assistant reply] → The row is the original text. SHA dedupe stops repeats. The 2,000-token read budget limits what comes back.
- [before_agent_start adds latency] → One shortlist plus at most 20 Nouls. An empty store returns immediately and does not call Jev.

## Migration Plan

Install the updated extension and restart Pi. New writes go to `~/.pi/agent/jev-cm.sqlite`. To keep using a project file, set `JEV_CM_SQLITE` to that path before starting Pi. Rollback is removing the extension. The SQLite file stays on disk. `jev-cm purge --yes` deletes whichever file the current configuration points at.

## Open Questions

- None for this change. The estimator remains `utf8-div-4` until a provider tokenizer is adopted.
