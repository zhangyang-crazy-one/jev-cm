import assert from "node:assert/strict";
import test from "node:test";
import { applyInjection, candidatesFromMessages, messagesFromSession, runMemoryStatus } from "./memory.js";

test("agent end keeps prose and ignores tool output", () => {
  const messages = candidatesFromMessages([
    { role: "user", content: "remember the refund window" },
    { role: "assistant", content: [{ type: "text", text: "Tuesday" }, { type: "toolCall", name: "bash", arguments: { command: "ls" } }] },
    { role: "toolResult", content: [{ type: "text", text: "secret tool bytes" }] },
    { role: "assistant", content: "   " },
  ]);
  assert.deepEqual(messages, [
    { role: "user", content: "remember the refund window" },
    { role: "assistant", content: "Tuesday" },
  ]);
});

test("injection uses a section and keeps the existing system prompt", () => {
  const withSections = { systemPrompt: "PI PROMPT", systemPromptOptions: { sections: {} } };
  assert.equal(applyInjection(withSections, "beta refund"), undefined);
  assert.equal(withSections.systemPromptOptions.sections["jev-memory"], "beta refund");
  assert.equal(withSections.systemPrompt, "PI PROMPT");

  const legacy = applyInjection({ systemPrompt: "PI PROMPT" }, "beta refund\ncwd:/proj/b");
  assert.match(legacy.systemPrompt, /^PI PROMPT\n\n<jev-memory>\nbeta refund/);
  assert.match(legacy.systemPrompt, /cwd:\/proj\/b/);
  assert.equal(applyInjection({ systemPrompt: "PI PROMPT" }, ""), undefined);
});

test("memory status hides stored bodies", () => {
  const notice = runMemoryStatus("jev-cm", {}, () => ({
    status: 0,
    stdout: JSON.stringify({ path: "/tmp/jev-cm.sqlite", count: 2, key_set: true, text: "secret body" }),
  }));
  assert.match(notice, /\/tmp\/jev-cm.sqlite/);
  assert.match(notice, /rows 2/);
  assert.match(notice, /key set/);
  assert.equal(notice.includes("secret body"), false);
});

test("remember keeps the conversation and skips tool output", () => {
  const messages = messagesFromSession([
    { type: "message", message: { role: "user", content: "退款窗口是多久" } },
    { type: "message", message: { role: "assistant", content: [{ type: "text", text: "十四天" }, { type: "toolCall", name: "bash", arguments: { command: "ls" } }] } },
    { type: "message", message: { role: "toolResult", content: [{ type: "text", text: "secret tool bytes" }] } },
    { type: "compaction", summary: "generated recap" },
  ]);
  assert.deepEqual(messages, [
    { role: "user", content: "退款窗口是多久" },
    { role: "assistant", content: "十四天" },
  ]);
  assert.deepEqual(messagesFromSession([]), []);
});
