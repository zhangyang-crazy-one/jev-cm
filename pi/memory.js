import { spawnSync } from "node:child_process";
import { textOf } from "./compact.js";

export function messagesFromSession(entries) {
  const messages = [];
  for (const entry of entries || []) {
    if (entry && entry.type === "message" && entry.message) {
      messages.push(entry.message);
    }
  }
  return candidatesFromMessages(messages);
}

export function sessionEntries(manager) {
  if (!manager) {
    return [];
  }
  if (typeof manager.getBranch === "function") {
    return manager.getBranch() || [];
  }
  if (typeof manager.getEntries === "function") {
    return manager.getEntries() || [];
  }
  return [];
}

export function candidatesFromMessages(messages) {
  const out = [];
  for (const message of messages || []) {
    if (!message || typeof message !== "object") {
      continue;
    }
    if (message.role === "assistant") {
      const content = assistantProse(message.content);
      if (content) {
        out.push({ role: "assistant", content });
      }
      continue;
    }
    if (message.role === "user") {
      const content = textOf(message.content).trim();
      if (content) {
        out.push({ role: "user", content });
      }
    }
  }
  return out;
}

export function applyInjection(event, section) {
  if (!section) {
    return undefined;
  }
  const options = event?.systemPromptOptions;
  if (options && options.sections && typeof options.sections === "object") {
    options.sections["jev-memory"] = section;
    return undefined;
  }
  const base = typeof event?.systemPrompt === "string" ? event.systemPrompt : "";
  return { systemPrompt: `${base}\n\n<jev-memory>\n${section}\n</jev-memory>\n` };
}

export function runCapture(binary, payload, env = process.env, spawn = spawnSync) {
  return runJson(binary, ["capture"], payload, env, spawn);
}

export function runInject(binary, prompt, env = process.env, spawn = spawnSync) {
  const result = spawn(binary, ["inject"], {
    input: prompt,
    encoding: "utf8",
    env,
    timeout: 60_000,
  });
  if (result.error || result.status !== 0) {
    return "";
  }
  return result.stdout || "";
}

export function runMemoryStatus(binary, env = process.env, spawn = spawnSync) {
  const status = runJson(binary, ["memory"], null, env, spawn);
  if (!status || typeof status !== "object") {
    return "memory unavailable";
  }
  const key = status.key_set ? "key set" : "key missing";
  return `path ${status.path}\nrows ${status.count}\n${key}`;
}

function runJson(binary, args, payload, env, spawn) {
  const result = spawn(binary, args, {
    input: payload == null ? "" : JSON.stringify(payload),
    encoding: "utf8",
    env,
    timeout: 60_000,
  });
  if (result.error || result.status !== 0 || !result.stdout) {
    return null;
  }
  try {
    return JSON.parse(result.stdout);
  } catch {
    return null;
  }
}

function assistantProse(content) {
  if (typeof content === "string") {
    return content.trim();
  }
  if (!Array.isArray(content)) {
    return "";
  }
  return content
    .filter((block) => block && block.type === "text" && typeof block.text === "string")
    .map((block) => block.text)
    .join("\n")
    .trim();
}
