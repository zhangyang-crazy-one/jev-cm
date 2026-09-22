## ADDED Requirements

### Requirement: Frozen prefix
The context engine SHALL treat the leading scaffold of instructions and rules as a frozen prefix. Compaction and fresh-window assembly MUST NOT rewrite, summarize, or reorder bytes already in that prefix. The prefix MAY grow only when the scaffold source itself changes.

#### Scenario: Compaction leaves the prefix untouched
- **WHEN** compaction runs on a session whose prefix is already established
- **THEN** the prefix bytes in the outgoing model context are identical to the prefix bytes from before compaction

### Requirement: Tool-output elision
When context pressure reaches the configured ratio of the budget, the engine SHALL ask Jev whether each eligible older tool output is still needed. An output MUST be removed from the model-facing window only when that probability is below `drop_threshold`. The default `drop_threshold` MUST be 0.25. Removal MUST keep the tool-call identity and message order, replace the body with a stable pointer, and retain the original bytes in the elision store. Tool output that fits inside the newest protected tail MUST NOT be eligible. A tool output larger than that tail MUST remain eligible.

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

### Requirement: Original restore
The engine SHALL expose an expand operation that returns stored tool output by pointer. The returned bytes MUST match the stored original for the requested page. Expand MUST NOT rerun the tool that produced the output.

#### Scenario: Page matches stored bytes
- **WHEN** the caller expands a known pointer and page
- **THEN** the returned page bytes equal the corresponding slice of the stored original

#### Scenario: Unknown pointer
- **WHEN** the caller expands a pointer that is not in the store
- **THEN** the engine returns a not-found result and does not invent replacement text

### Requirement: Fresh window
On a fresh-window request, the engine SHALL assemble the model context from the frozen prefix, pinned decisions, Jev-selected original passages, and pointers for elided output. It MUST NOT include a generated summary of prior user or assistant messages. Prior prose MAY appear only as an original passage that the memory or elision store already held and that selection admitted.

#### Scenario: Summary is absent
- **WHEN** a fresh window is assembled after a long session
- **THEN** the assembled context contains no summary field and no model-authored recap of the omitted turns

#### Scenario: Selected original is present
- **WHEN** selection admits a stored passage under the token budget
- **THEN** that passage's original bytes appear in the fresh window with its source id

### Requirement: Token budget signal
The engine SHALL report the estimated tokens used, the budget, and the estimator name whenever it builds a model context. A passage that scores as relevant but does not fit MUST be reported as `over_budget` and MUST NOT be split in the middle.

#### Scenario: Budget report
- **WHEN** a context is assembled
- **THEN** the result includes used tokens, the budget, and the estimator name

#### Scenario: Over budget passage
- **WHEN** the next relevant passage would exceed the remaining budget
- **THEN** that passage is omitted whole and listed as `over_budget`

### Requirement: Compaction fail open
If Jev authentication fails, times out, or returns an unusable judgment, the engine MUST leave the existing model context unchanged and MUST signal that the host should continue with its built-in compaction. The engine MUST NOT substitute a locally written summary.

#### Scenario: Jev timeout
- **WHEN** the Jev call exceeds the configured timeout during compaction
- **THEN** the session messages stay as they were and the result status is fallback

### Requirement: Message identity
Elision MUST NOT delete or reorder host messages. The number and order of messages after a successful compaction MUST equal the number and order before it.

#### Scenario: Count is stable
- **WHEN** compaction elides one tool output in a session of N messages
- **THEN** the session still has N messages in the same order

### Requirement: Host window fit
When the Pi extension returns a compaction result, the estimated tokens of the summary plus the entries kept from `firstKeptEntryId` onward MUST be at or below the host safe line. The safe line is the model context window minus Pi's `reserveTokens`. When that window is unavailable, the safe line is Pi's `keepRecentTokens`. If Pi's keep point leaves a larger span, the extension MUST move the keep point forward until the span fits, and tool messages that fall before the new point MUST be included in the elision plan. When no suffix fits, the extension MUST keep none of those entries and MUST use a fresh-window marker rather than a generated recap. A keep point that already fits, and a plan with nothing to inject, MUST leave Pi's own compaction in place. This cut still happens when Jev is unavailable, because leaving the oversized span for Pi's summarizer does not remove it.

#### Scenario: Oversized kept tool result
- **WHEN** Pi's keep point would retain a tool result whose estimated tokens exceed the host safe line
- **THEN** the returned `firstKeptEntryId` starts after that tool result and the summary does not contain the tool body

#### Scenario: Nothing fits
- **WHEN** every entry in the kept span is larger than the host safe line
- **THEN** the compaction result keeps none of those entries and its summary is the fresh-window marker

#### Scenario: Fitting span without an injection
- **WHEN** the kept span is already within the host safe line and the plan has no injection text
- **THEN** the extension returns no compaction result
