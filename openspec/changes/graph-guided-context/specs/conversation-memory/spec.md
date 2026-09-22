## MODIFIED Requirements

### Requirement: Turn capture
After an agent run ends, the system SHALL store each original user message and assistant prose message from that run. The insert MUST NOT call Jev and MUST NOT wait on Jev. A candidate whose SHA-256 is already stored MUST NOT be inserted again. A graph link call MAY follow a successful insert, as specified by memory-graph, and a failed link call MUST leave the inserted rows in place.

#### Scenario: Assistant prose is stored
- **WHEN** an agent run ends with a user message and an assistant prose reply
- **THEN** each of those texts is stored as its original bytes before any Jev call

#### Scenario: Duplicate text
- **WHEN** a candidate's SHA-256 is already present in the conversation store
- **THEN** the system does not insert another row and does not call Jev

#### Scenario: Link failure keeps the row
- **WHEN** the insert succeeds and the following graph link call fails
- **THEN** the stored rows remain

### Requirement: Global recall before the turn
Before the agent starts a turn, the system SHALL recall the `conversation` collection with the current user prompt and MUST search every project in the store. The candidate set MUST be the memory-graph neighborhood seeded by full-text search. An empty seed list MUST NOT call Jev. Otherwise the system MUST rank that neighborhood with one Noul per candidate, keep passages at or above `0.5`, and omit passages marked unranked. A recall failure MUST leave stored rows and edges in place and MUST NOT inject unranked passages. Admitted passages MUST be the stored original text, ordered by descending probability, and labeled with session id and cwd. The system MUST inject them as the `jev-memory` system-prompt section and MUST NOT replace Pi's system prompt. The section MUST stay within 2,000 estimated tokens. A passage that does not fit MUST be omitted whole.

#### Scenario: Memory from another project is loaded
- **WHEN** the store holds a passage written in a different cwd and Jev ranks it at or above `0.5` for the current prompt
- **THEN** the `jev-memory` section contains that original text and its cwd, and Pi's system prompt is otherwise unchanged

#### Scenario: Empty store skips Jev
- **WHEN** the conversation store has no rows
- **THEN** the turn starts with no `jev-memory` section and Jev is not called

#### Scenario: Linked neighbor is ranked
- **WHEN** full-text search seeds one passage and a graph edge adds a second passage that shares no token with the prompt, and Jev ranks only the second passage at or above `0.5`
- **THEN** the `jev-memory` section contains the second passage's original text and omits the seed

#### Scenario: Unranked shortlist is not injected
- **WHEN** recall ranking fails
- **THEN** the neighborhood is not written into `jev-memory` and the stored rows and edges remain

#### Scenario: Over budget passage is omitted whole
- **WHEN** the next relevant passage does not fit in the 2,000-token section budget
- **THEN** that passage is omitted entirely and no partial text is injected

### Requirement: Pi memory commands
The Pi extension SHALL register `/jev memory` to report the database path and the conversation row count without printing stored bodies. `/jev remember` MUST store the current session's user messages and assistant prose without calling Jev. It MUST NOT ask for a single sentence. Tool results and generated summaries MUST stay out of that write. When the session has no such prose, nothing is inserted. `/jev recall` MUST rank the memory-graph neighborhood with Jev. With no extra text, the request is the current session's user and assistant prose, using the newest turns that fit in 2,000 estimated tokens and keeping each turn whole. With extra text, that text is the request. Ranking uses one Noul per neighborhood candidate, keeps passages at or above `0.5`, omits unranked passages, and stays within 2,000 estimated tokens. A passage that does not fit MUST be omitted whole. The command MUST NOT place every stored passage into the session. When the current session has no such prose and no extra text is given, nothing is injected and Jev is not called.

#### Scenario: Status hides bodies
- **WHEN** the user runs `/jev memory` and the store has rows
- **THEN** the notice includes the database path and the row count and does not include a stored passage

#### Scenario: Manual remember
- **WHEN** the user runs `/jev remember` in a session that has a user message and an assistant reply
- **THEN** both texts are stored as their original bytes and Jev is not called

#### Scenario: Empty conversation
- **WHEN** the user runs `/jev remember` and the session has no user or assistant prose
- **THEN** nothing is inserted

#### Scenario: Recall follows the current conversation
- **WHEN** the user runs `/jev recall` with no extra text while the current session is about a refund window, and Jev ranks one neighborhood passage at or above `0.5`
- **THEN** the current session receives that original passage and does not receive stored passages Jev ranks below `0.5`

#### Scenario: Empty current conversation
- **WHEN** the user runs `/jev recall` with no extra text and the current session has no user or assistant prose
- **THEN** nothing is injected and Jev is not called
