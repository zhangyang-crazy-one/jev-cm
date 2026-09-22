## MODIFIED Requirements

### Requirement: Tool-output elision
When Pi compacts and the estimated tokens of the span being compacted reach `pressure_ratio` times the host safe line, the engine SHALL ask Jev whether each eligible older tool output is still needed. The host safe line is the model context window minus Pi's `reserveTokens`. When that window is unavailable, the safe line is Pi's `keepRecentTokens`. When the extension supplies no safe line, the budget is the internal token budget. An output MUST be removed from the model-facing window only when that probability is below `drop_threshold`. The default `drop_threshold` MUST be 0.25. Removal MUST keep the tool-call identity and message order, replace the body with a stable pointer, and retain the original bytes in the elision store. Tool output that fits inside the newest protected tail MUST NOT be eligible. A tool output larger than that tail MUST remain eligible.

#### Scenario: Low score is elided
- **WHEN** an eligible tool output scores below `drop_threshold` and its original bytes are stored
- **THEN** the model-facing message keeps its identity and contains a pointer instead of the original body

#### Scenario: Uncertain score is kept
- **WHEN** an eligible tool output scores at or above `drop_threshold`
- **THEN** the model-facing body remains the original tool output

#### Scenario: Protected tail
- **WHEN** a tool output fits inside the configured protected tail
- **THEN** the engine does not send it to Jev and does not elide it

#### Scenario: Tool output larger than the tail
- **WHEN** a tool output is larger than the protected tail budget
- **THEN** the engine treats that output as eligible and elides it when Jev scores it below the drop threshold

#### Scenario: Host safe line sets pressure
- **WHEN** the extension supplies a host safe line different from the internal token budget and the compacted span reaches `pressure_ratio` times that safe line
- **THEN** the engine asks Jev about each eligible tool output

### Requirement: Fresh window
On a fresh-window request, the engine SHALL assemble the model context from the frozen prefix, pinned decisions, graph-ranked original passages, and pointers for elided output. The passages MUST be the ones admitted by conversation recall for the latest user prose, using the memory-graph neighborhood. It MUST NOT include a generated summary of prior user or assistant messages. Prior prose MAY appear only as an original passage that the memory or elision store already held and that selection admitted.

#### Scenario: Summary is absent
- **WHEN** a fresh window is assembled after a long session
- **THEN** the assembled context contains no summary field and no model-authored recap of the omitted turns

#### Scenario: Selected original is present
- **WHEN** selection admits a stored passage under the token budget
- **THEN** that passage's original bytes appear in the fresh window with its source id

#### Scenario: Graph neighbor can enter the window
- **WHEN** the latest user prose seeds one stored passage and a graph edge adds another passage that Jev ranks at or above `0.5`
- **THEN** the fresh window can contain that neighbor's original bytes and contains no generated summary
