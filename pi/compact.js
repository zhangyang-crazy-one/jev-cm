import { spawnSync } from "node:child_process";
import { existsSync, realpathSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const FRESH_WINDOW_ID = "jev-cm:fresh-window";
export const FRESH_WINDOW_LINE =
  "[jev-cm: fresh window; earlier turns stay in the session log and are not summarized. Stored tool output can be retrieved with expand.]";
const SUMMARY_ALLOWANCE = 4000;

export function textOf(content) {
  if (typeof content === "string") {
    return content;
  }
  if (!Array.isArray(content)) {
    return "";
  }
  return content
    .map((block) => {
      if (!block || typeof block !== "object") {
        return "";
      }
      if (typeof block.text === "string") {
        return block.text;
      }
      if (typeof block.content === "string" || Array.isArray(block.content)) {
        return textOf(block.content);
      }
      return "";
    })
    .filter(Boolean)
    .join("\n");
}

export function estimateTokens(text) {
  const value = typeof text === "string" ? text : "";
  return Math.ceil(value.length / 4);
}

export function entryTokens(entry) {
  if (!entry || typeof entry !== "object") {
    return 0;
  }
  if (entry.type === "message" && entry.message) {
    return estimateTokens(JSON.stringify(entry.message));
  }
  if (entry.type === "compaction" || entry.type === "branch_summary") {
    return estimateTokens(entry.summary || "");
  }
  if (entry.type === "custom_message") {
    return estimateTokens(JSON.stringify(entry.content ?? ""));
  }
  return 0;
}

export function hostBudget(preparation, contextWindow) {
  const reserve = numberOr(preparation?.settings?.reserveTokens, 65536);
  const keepRecent = numberOr(preparation?.settings?.keepRecentTokens, 32000);
  if (typeof contextWindow === "number" && contextWindow > reserve) {
    return contextWindow - reserve;
  }
  return keepRecent;
}

export function suffixFrom(entries, id) {
  if (!Array.isArray(entries) || !id) {
    return [];
  }
  const index = entries.findIndex((entry) => entry && entry.id === id);
  if (index < 0) {
    return [];
  }
  return entries.slice(index);
}

export function selectFreshCut(entries, summaryTokens, safeBudget) {
  const list = Array.isArray(entries) ? entries.filter((entry) => entry && entry.id) : [];
  const summary = Math.max(0, summaryTokens || 0);
  if (safeBudget <= 0 || summary > safeBudget) {
    return { firstKeptEntryId: null, keptTokens: 0 };
  }
  let keptTokens = 0;
  let start = list.length;
  for (let index = list.length - 1; index >= 0; index -= 1) {
    const next = keptTokens + entryTokens(list[index]);
    if (summary + next > safeBudget) {
      break;
    }
    keptTokens = next;
    start = index;
  }
  if (start >= list.length) {
    return { firstKeptEntryId: null, keptTokens: 0 };
  }
  return { firstKeptEntryId: list[start].id, keptTokens };
}

export function deepenedCut(preparation, branchEntries, contextWindow, summaryTokens) {
  const preferred = preparation?.firstKeptEntryId;
  const budget = hostBudget(preparation, contextWindow);
  const suffix = suffixFrom(branchEntries, preferred);
  if (summaryTokens + sumTokens(suffix) <= budget) {
    return preferred;
  }
  const cut = selectFreshCut(suffix, summaryTokens, budget);
  return cut.firstKeptEntryId || FRESH_WINDOW_ID;
}

export function resolveBinary(env = process.env, here = dirname(fileURLToPath(import.meta.url))) {
  if (env.JEV_CM_BIN) {
    return env.JEV_CM_BIN;
  }
  const dirs = [here];
  try {
    dirs.push(realpathSync(here));
  } catch {
    // Keep the path Pi used to load the extension.
  }
  for (const dir of dirs) {
    const sibling = join(dir, "..", "bin", "jev-cm");
    if (existsSync(sibling)) {
      return sibling;
    }
  }
  return "jev-cm";
}

export function messagesFromPreparation(preparation) {
  const out = [];
  let index = 0;
  const add = (message) => {
    if (!message || typeof message !== "object") {
      return;
    }
    const role = typeof message.role === "string" ? message.role : "user";
    const id = message.id || message.toolCallId || `m${index}`;
    index += 1;
    let kind = "prose";
    let mapped = role;
    if (role === "system") {
      kind = "prefix";
    }
    if (role === "toolResult" || role === "tool") {
      kind = "tool";
      mapped = "tool";
    }
    out.push({ id, role: mapped, kind, content: textOf(message.content) });
  };
  for (const message of preparation?.messagesToSummarize || []) {
    add(message);
  }
  for (const message of preparation?.turnPrefixMessages || []) {
    add(message);
  }
  return out;
}

export function messagesForCompaction(preparation, branchEntries, cutId) {
  const base = messagesFromPreparation(preparation);
  if (!cutId || cutId === preparation?.firstKeptEntryId) {
    return base;
  }
  const suffix = suffixFrom(branchEntries, preparation?.firstKeptEntryId);
  const cutIndex = cutId === FRESH_WINDOW_ID ? suffix.length : suffix.findIndex((entry) => entry.id === cutId);
  const dropped = cutIndex >= 0 ? suffix.slice(0, cutIndex) : [];
  return base.concat(messagesFromPreparation({ messagesToSummarize: dropped.filter((entry) => entry?.type === "message").map((entry) => entry.message) }));
}

export function compactionFromPlan(plan, preparation) {
  if (!plan || plan.status !== "compacted" || !Array.isArray(plan.context) || plan.context.length === 0) {
    return undefined;
  }
  return {
    compaction: {
      summary: plan.context.join("\n\n"),
      firstKeptEntryId: preparation?.firstKeptEntryId,
      tokensBefore: preparation?.tokensBefore,
      details: {
        source: "jev-cm",
        estimator: plan.estimator,
        over_budget: plan.over_budget || [],
      },
    },
  };
}

export function planCompaction({ preparation, branchEntries, contextWindow, plan }) {
  const budget = hostBudget(preparation, contextWindow);
  const preferred = preparation?.firstKeptEntryId;
  const suffix = suffixFrom(branchEntries, preferred);
  let summary = fitSummary(summaryText(plan), budget);
  const over = estimateTokens(summary) + sumTokens(suffix) > budget;
  let firstKept = preferred;
  if (over) {
    const allowance = estimateTokens(summary) || estimateTokens(FRESH_WINDOW_LINE);
    const cut = selectFreshCut(suffix, allowance, budget);
    firstKept = cut.firstKeptEntryId || FRESH_WINDOW_ID;
    if (!summary) {
      summary = FRESH_WINDOW_LINE;
    }
  }
  if (!summary || !firstKept) {
    return undefined;
  }
  const kept = firstKept === FRESH_WINDOW_ID ? [] : suffixFrom(branchEntries, firstKept);
  if (estimateTokens(summary) + sumTokens(kept) > budget) {
    summary = fitSummary(summary, Math.max(budget - sumTokens(kept), 0)) || FRESH_WINDOW_LINE;
  }
  return {
    compaction: {
      summary,
      firstKeptEntryId: firstKept,
      tokensBefore: preparation?.tokensBefore,
      details: {
        source: "jev-cm",
        estimator: plan?.estimator,
        over_budget: plan?.over_budget || [],
        fresh_cut: firstKept !== preferred,
      },
    },
  };
}

export function runPrepare(binary, messages, env = process.env, spawn = spawnSync) {
  const result = spawn(binary, ["prepare"], {
    input: JSON.stringify({ messages }),
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

export function requestCut(preparation, branchEntries, contextWindow) {
  return deepenedCut(preparation, branchEntries, contextWindow, SUMMARY_ALLOWANCE);
}

function summaryText(plan) {
  if (!plan || plan.status !== "compacted" || !Array.isArray(plan.context) || plan.context.length === 0) {
    return "";
  }
  return plan.context.join("\n\n");
}

function fitSummary(summary, budget) {
  if (!summary || estimateTokens(summary) <= budget) {
    return summary || "";
  }
  const kept = [];
  let used = 0;
  for (const part of summary.split("\n\n")) {
    const cost = estimateTokens(part) + (kept.length ? estimateTokens("\n\n") : 0);
    if (used + cost > budget) {
      break;
    }
    kept.push(part);
    used += cost;
  }
  return kept.join("\n\n");
}

function sumTokens(entries) {
  return (entries || []).reduce((total, entry) => total + entryTokens(entry), 0);
}

function numberOr(value, fallback) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}
