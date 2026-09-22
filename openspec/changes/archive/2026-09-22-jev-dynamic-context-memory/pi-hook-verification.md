# Pi compaction hook

Checked against the installed binary `/home/zhangyangrui/pi/pi`, version 0.84.2, on 2026-09-22.

`session_before_compact` is present in that binary. The same path in `@earendil-works/pi-coding-agent` 0.86.1 (`agent-session.js`) does this:

- The hook receives `preparation`, including `messagesToSummarize`, `firstKeptEntryId`, and `tokensBefore`.
- `{ cancel: true }` aborts compaction.
- `{ compaction: { summary, firstKeptEntryId, tokensBefore } }` is stored with `appendCompaction` and the default summary model is not called.
- No result leaves the default summary generator in place.

The installed 0.84.2 binary contains the same `extensionCompaction` branch and the `session_before_compact` event name. The extension therefore returns a compaction result only when `jev-cm prepare` reports `compacted` with non-empty injection text. That text is pointers and original passages placed in the host field named `summary`. A missing binary, a Jev error, or an empty plan returns nothing, so Pi keeps its own compaction.

The OpenCode hook is not the product. `opencode/jev-cm.js` has been removed.
