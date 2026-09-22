# conversation-memory Specification

## Purpose

Store original Pi user and assistant turns in one local SQLite file and recall them across projects before the next turn.

## Requirements

### Requirement: Global conversation store
The system SHALL store conversation memory in one SQLite file shared by every Pi project and session. The default path MUST be `~/.pi/agent/jev-cm.sqlite`. When `PI_CODING_AGENT_DIR` is set, the file MUST live in that directory as `jev-cm.sqlite`. When `JEV_CM_SQLITE` is set, that path MUST be used instead. The store MUST record the original text, its SHA-256, the project cwd, the Pi session id, and the role `user` or `assistant`.

#### Scenario: Same file across projects
- **WHEN** two Pi sessions run in different working directories and neither sets `JEV_CM_SQLITE`
- **THEN** both sessions read and write `~/.pi/agent/jev-cm.sqlite`

#### Scenario: Explicit path wins
- **WHEN** `JEV_CM_SQLITE` is set to an absolute path
- **THEN** conversation reads and writes use that path and do not create `data/jev-cm.sqlite`

### Requirement: Turn capture
After an agent run ends, the system SHALL store each original user message and assistant prose message from that run. The system MUST NOT call Jev while storing. A candidate whose SHA-256 is already stored MUST NOT be inserted again.

#### Scenario: Assistant prose is stored
- **WHEN** an agent run ends with a user message and an assistant prose reply
- **THEN** each of those texts is stored as its original bytes and Jev is not called

#### Scenario: Duplicate text
- **WHEN** a candidate's SHA-256 is already present in the conversation store
- **THEN** the system does not insert another row and does not call Jev

### Requirement: Ignored turn content
The system MUST ignore tool results, tool-call arguments, empty text, and generated summaries during turn capture. An ignored item MUST NOT be inserted into the conversation store and MUST NOT be sent to Jev. Tool output remains eligible only for the elision store.

#### Scenario: Tool output is ignored
- **WHEN** an agent run ends with a tool result and an assistant prose reply
- **THEN** the tool result is not inserted into the conversation store and is not sent to Jev

#### Scenario: Generated summary is ignored
- **WHEN** a candidate is a generated summary
- **THEN** the system refuses it without a network call and does not insert a row

### Requirement: Global recall before the turn
Before the agent starts a turn, the system SHALL recall the `conversation` collection with the current user prompt and MUST search every project in the store. An empty shortlist MUST NOT call Jev. Otherwise the system MUST rank the FTS shortlist with one Noul per candidate, keep passages at or above `0.5`, and omit passages marked unranked. A recall failure MUST leave stored rows in place and MUST NOT inject unranked passages. Admitted passages MUST be the stored original text, ordered by descending probability, and labeled with session id and cwd. The system MUST inject them as the `jev-memory` system-prompt section and MUST NOT replace Pi's system prompt. The section MUST stay within 2,000 estimated tokens. A passage that does not fit MUST be omitted whole.

#### Scenario: Memory from another project is loaded
- **WHEN** the store holds a passage written in a different cwd and Jev ranks it at or above `0.5` for the current prompt
- **THEN** the `jev-memory` section contains that original text and its cwd, and Pi's system prompt is otherwise unchanged

#### Scenario: Empty store skips Jev
- **WHEN** the conversation store has no rows
- **THEN** the turn starts with no `jev-memory` section and Jev is not called

#### Scenario: Unranked shortlist is not injected
- **WHEN** recall ranking fails
- **THEN** the shortlist is not written into `jev-memory` and the stored rows remain

#### Scenario: Over budget passage is omitted whole
- **WHEN** the next relevant passage does not fit in the 2,000-token section budget
- **THEN** that passage is omitted entirely and no partial text is injected

### Requirement: Pi memory commands
The Pi extension SHALL register `/jev memory` to report the database path and the conversation row count without printing stored bodies. `/jev remember` MUST store the supplied text with source id `manual` and role `user` without calling Jev. When `/jev remember` is invoked without text, the extension MUST ask for the text through Pi's input dialog.

#### Scenario: Status hides bodies
- **WHEN** the user runs `/jev memory` and the store has rows
- **THEN** the notice includes the database path and the row count and does not include a stored passage

#### Scenario: Manual remember
- **WHEN** the user runs `/jev remember` with a sentence
- **THEN** the sentence is stored with source id `manual` and role `user` and Jev is not called
