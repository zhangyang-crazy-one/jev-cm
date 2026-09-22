## ADDED Requirements

### Requirement: Conversation nodes and dependency edges
The system SHALL treat each stored conversation row as a graph node in the same SQLite file as conversation memory. A directed edge MUST record the newer row as `src`, the older row as `dst`, the kind `related`, and the admitting probability. The system MUST NOT store a second edge with the same `src`, `dst`, and kind. Tool results, tool-call arguments, empty text, and generated summaries MUST NOT become nodes.

#### Scenario: Edge points at the older passage
- **WHEN** a newly stored turn is linked to an older conversation row
- **THEN** the edge source is the new row, the edge destination is the older row, and the kind is `related`

#### Scenario: Tool output is not a node
- **WHEN** an agent run ends with a tool result and an assistant prose reply
- **THEN** the tool result has no graph node and no edge

### Requirement: Link after capture
After new conversation rows are committed, the system SHALL ask Jev one Noul per full-text seed, at most eight seeds, for whether the new turn depends on that seed. An edge MUST be inserted only when the probability is at or above `recall_cutoff`. The insert of the original bytes MUST finish before that call. A duplicate text MUST NOT call Jev and MUST NOT add an edge. When Jev cannot answer, the new rows MUST remain and no edge MUST be added.

#### Scenario: High score stores an edge
- **WHEN** capture stores a new turn and Jev scores one of eight seeds at or above `recall_cutoff`
- **THEN** the original row is already stored and one `related` edge points from that row to the seed

#### Scenario: Missing key leaves the node isolated
- **WHEN** capture stores a new turn and the selected provider has no API key
- **THEN** the row remains and the graph has no new edge

#### Scenario: Duplicate does not link
- **WHEN** capture sees text whose SHA-256 is already stored
- **THEN** no additional row is inserted and Jev is not called

### Requirement: Recursive neighborhood
Recall MUST build its candidate set by seeding full-text search and then walking `memory_edge` in both directions for at most two hops. The walk MUST stay inside the `conversation` collection and MUST search every project. The neighborhood MUST be deduplicated, MUST keep seeds ahead of neighbors, and MUST NOT exceed `shortlist_limit`. An empty seed list MUST NOT walk the graph and MUST NOT call Jev.

#### Scenario: Neighbor without shared tokens is included
- **WHEN** full-text search seeds passage A and a `related` edge connects A to passage B within two hops, and B shares no token with the query
- **THEN** the candidate set presented to Jev includes B's stored original

#### Scenario: Unlinked store matches full-text seeds
- **WHEN** the graph has conversation rows and no edges, and full-text search returns two seeds
- **THEN** the candidate set is those two seeds

#### Scenario: Empty seed skips the walk
- **WHEN** full-text search returns no seed for the current request
- **THEN** the graph is not walked and Jev is not called
