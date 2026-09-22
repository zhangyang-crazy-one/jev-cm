## 1. Graph store

- [x] 1.1 Add `memory_edge` on database open, with source, destination, kind, and probability, without rewriting existing conversation rows
- [x] 1.2 Walk seeds in both directions for at most two hops, dedupe, keep seeds first, and cap the neighborhood at `shortlist_limit`
- [x] 1.3 Test an unlinked store, a neighbor that shares no token with the query, and an empty seed list that does not walk

## 2. Link after capture

- [x] 2.1 After a successful capture insert, score at most eight full-text seeds with one Jev request and store a `related` edge at or above `recall_cutoff`
- [x] 2.2 Skip the link call for duplicate text, tool output, and generated summaries, and keep the new rows when Jev is unavailable
- [x] 2.3 Leave `/jev remember` as a no-Jev write

## 3. Recall uses the neighborhood

- [x] 3.1 Rank the graph neighborhood in `before_agent_start`, `/jev recall`, and compaction recall, with the same `0.5` cutoff, unranked omission, and 2,000-token budget
- [x] 3.2 Test that a linked neighbor can be admitted while its full-text seed is rejected, and that a ranking failure injects nothing

## 4. Host-window compaction

- [x] 4.1 Pass the host safe line from the Pi extension into `prepare` and treat pressure against that line, falling back to the internal token budget
- [x] 4.2 Ask Jev about eligible tool outputs only when that pressure is reached, and build the fresh window from the frozen prefix, pointers, and graph-ranked originals
- [x] 4.3 Test the host-safe-line trigger, pointer replacement below `0.25`, and a fresh window that contains a graph neighbor and no generated summary

## 5. Documentation

- [x] 5.1 Update the README usage section so recall describes the full-text seed, the two-hop graph walk, and the Jev rank, and so compaction describes the host-window elision call
