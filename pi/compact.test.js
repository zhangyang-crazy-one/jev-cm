import assert from "node:assert/strict";
import test from "node:test";
import { FRESH_WINDOW_ID, compactionFromPlan, messagesForCompaction, messagesFromPreparation, planCompaction, runPrepare, textOf } from "./compact.js";

test("flattens Pi tool results and keeps the prefix role", () => {
  const messages = messagesFromPreparation({
    messagesToSummarize: [
      { role: "system", content: "frozen rules" },
      { role: "toolResult", toolCallId: "call-1", content: [{ type: "text", text: "original bytes" }] },
    ],
    turnPrefixMessages: [{ role: "user", content: [{ type: "text", text: "keep going" }] }],
  });
  assert.deepEqual(messages, [
    { id: "m0", role: "system", kind: "prefix", content: "frozen rules" },
    { id: "call-1", role: "tool", kind: "tool", content: "original bytes" },
    { id: "m2", role: "user", kind: "prose", content: "keep going" },
  ]);
  assert.equal(textOf([{ text: "a" }, { content: [{ text: "b" }] }]), "a\nb");
});

test("a compacted plan becomes Pi compaction and skips the host summary", () => {
  const result = compactionFromPlan(
    { status: "compacted", context: ["[jev-cm pointer]"], estimator: "utf8-div-4", over_budget: ["policy"] },
    { firstKeptEntryId: "entry-9", tokensBefore: 1000 },
  );
  assert.equal(result.compaction.summary, "[jev-cm pointer]");
  assert.equal(result.compaction.firstKeptEntryId, "entry-9");
  assert.equal(result.compaction.tokensBefore, 1000);
  assert.equal(result.compaction.details.source, "jev-cm");
});

test("fallback and a missing binary leave Pi compaction unchanged", () => {
  assert.equal(compactionFromPlan({ status: "fallback", context: ["x"] }, {}), undefined);
  assert.equal(compactionFromPlan({ status: "compacted", context: [] }, {}), undefined);
  const plan = runPrepare("/no/such/jev-cm", [], {}, () => ({ error: new Error("spawn"), status: null }));
  assert.equal(plan, null);
});

test("moves the keep point past a tool result that does not fit the host window", () => {
  const huge = "y".repeat(8000);
  const branchEntries = [
    { id: "u1", type: "message", message: { role: "user", content: "question" } },
    { id: "t1", type: "message", message: { role: "toolResult", content: huge } },
    { id: "a1", type: "message", message: { role: "assistant", content: "ok" } },
  ];
  const preparation = {
    firstKeptEntryId: "u1",
    tokensBefore: 9000,
    settings: { reserveTokens: 100, keepRecentTokens: 50 },
    messagesToSummarize: [{ role: "user", content: "older" }],
  };
  const result = planCompaction({
    preparation,
    branchEntries,
    contextWindow: 1000,
    plan: { status: "compacted", context: ["[jev-cm pointer]"], estimator: "utf8-div-4" },
  });
  assert.equal(result.compaction.firstKeptEntryId, "a1");
  assert.equal(result.compaction.details.fresh_cut, true);
  assert.match(result.compaction.summary, /pointer/);
  const sent = messagesForCompaction(preparation, branchEntries, "a1");
  assert.equal(sent.some((message) => message.content === huge), true);
});

test("drops a kept span that is itself over the host window", () => {
  const branchEntries = [
    { id: "t1", type: "message", message: { role: "toolResult", content: "z".repeat(5000) } },
  ];
  const preparation = {
    firstKeptEntryId: "t1",
    tokensBefore: 2000,
    settings: { reserveTokens: 100, keepRecentTokens: 50 },
    messagesToSummarize: [],
  };
  const result = planCompaction({
    preparation,
    branchEntries,
    contextWindow: 1000,
    plan: null,
  });
  assert.equal(result.compaction.firstKeptEntryId, FRESH_WINDOW_ID);
  assert.match(result.compaction.summary, /fresh window/);
});

test("leaves a fitting window to Pi when Jev has nothing to inject", () => {
  const branchEntries = [
    { id: "u1", type: "message", message: { role: "user", content: "short" } },
  ];
  const preparation = {
    firstKeptEntryId: "u1",
    tokensBefore: 100,
    settings: { reserveTokens: 1000, keepRecentTokens: 32000 },
  };
  assert.equal(planCompaction({
    preparation,
    branchEntries,
    contextWindow: 262144,
    plan: { status: "fallback" },
  }), undefined);
});
test("passes the host safe line on the prepare payload", () => {
  let seen;
  runPrepare("/fake", [{ id: "m0", role: "user", kind: "prose", content: "hi" }], {}, (binary, args, options) => {
    seen = JSON.parse(options.input);
    return { status: 0, stdout: JSON.stringify({ status: "fallback" }) };
  }, 190464);
  assert.equal(seen.host_budget, 190464);
  assert.equal(seen.messages.length, 1);
  runPrepare("/fake", [], {}, (binary, args, options) => {
    seen = JSON.parse(options.input);
    return { status: 0, stdout: "{}" };
  });
  assert.equal("host_budget" in seen, false);
});
