# durable-memory Specification

## Purpose

Persist admitted original text in a local SQLite store and recall whole passages under a token budget.

## Requirements

### Requirement: Local verbatim store
The memory store SHALL persist admitted entries in a local SQLite database. Each entry MUST contain the original text, its SHA-256, a source id, the admitting probability, and the model version that admitted it. The store MUST NOT persist a summary in place of the original text. The database MUST remain on the local machine unless the user copies it.

#### Scenario: Admitted entry keeps its bytes
- **WHEN** an entry is admitted
- **THEN** the stored text bytes hash to the recorded SHA-256 and the row contains the source id and model version

#### Scenario: Summary rejected
- **WHEN** a caller attempts to store a body marked as a generated summary
- **THEN** the store refuses the write

### Requirement: Write gate
Before inserting an entry, the store SHALL ask Jev one Noul whose high probability means the entry is durable, non-obvious, and useful on a later turn. The store MUST insert the entry only when that probability is at or above `remember_threshold`. The default `remember_threshold` MUST be 0.75. The question instructions MUST state that polarity.

#### Scenario: High score is stored
- **WHEN** the write-gate probability is at or above `remember_threshold`
- **THEN** the original entry is inserted once

#### Scenario: Low score is refused
- **WHEN** the write-gate probability is below `remember_threshold`
- **THEN** no row is inserted and the result says the entry was refused

### Requirement: Write unavailable
When Jev cannot answer the write gate, the store MUST NOT insert the entry. The result MUST be `unavailable` and MUST NOT be reported as a relevance refusal.

#### Scenario: Missing credentials
- **WHEN** a write is requested and the selected provider has no API key
- **THEN** nothing is inserted and the status is `unavailable`

### Requirement: Budgeted recall
Recall SHALL build a bounded FTS5 candidate shortlist, then ask Jev one relevance Noul per candidate against the current request. Candidates at or above the recall cutoff MUST be returned in descending probability, whole, until the token budget is exhausted. Remaining relevant candidates MUST be listed as `over_budget`. The returned passage bytes MUST equal the stored original. An empty selection MUST be a successful result.

#### Scenario: Ranked originals
- **WHEN** two candidates score at or above the cutoff and both fit
- **THEN** both originals are returned, higher probability first, each with its source id

#### Scenario: Nothing relevant
- **WHEN** every candidate scores below the cutoff
- **THEN** the result is successful, contains no passages, and does not call a generative model to fill the gap

### Requirement: Unranked recall
When Jev cannot answer a recall, the store SHALL still return the FTS shortlist. Each passage MUST be marked `unranked`, and the result MUST NOT claim a Jev probability.

#### Scenario: Recall timeout
- **WHEN** the recall judgment times out
- **THEN** the shortlist is returned with `unranked` passages and no fabricated probabilities

### Requirement: Collection scope
Recall and delete MUST operate on one named collection. Re-importing the same source id in a collection MUST replace that source's entries atomically. A different source id MUST NOT delete the other source.

#### Scenario: Re-import replaces one source
- **WHEN** the same source id is imported again into the same collection
- **THEN** the previous entries for that source are gone and entries for other sources remain
