import { messagesForCompaction, planCompaction, requestCut, resolveBinary, runPrepare } from "./compact.js";
import { applyInjection, candidatesFromMessages, messagesFromSession, runCapture, runInject, runMemoryStatus, sessionEntries } from "./memory.js";
import { applyCommand, completions, envFromSettings, loadSettings, saveSettings, settingsPath, statusText } from "./settings.js";

function childEnv() {
  return { ...process.env, ...envFromSettings(loadSettings(settingsPath())) };
}

export default function jevCm(pi) {
  pi.on("agent_end", (event, ctx) => {
    const identity = sessionIdentity(ctx);
    const messages = candidatesFromMessages(event?.messages);
    if (messages.length === 0) {
      return;
    }
    runCapture(resolveBinary(), {
      cwd: identity.cwd,
      session_id: identity.session_id,
      source_id: identity.session_id || "manual",
      messages,
    }, childEnv());
  });

  pi.on("before_agent_start", (event) => {
    const prompt = typeof event?.prompt === "string" ? event.prompt : "";
    const section = runInject(resolveBinary(), prompt, childEnv());
    return applyInjection(event, section);
  });

  pi.on("session_before_compact", (event, ctx) => {
    const preparation = event.preparation;
    const branchEntries = event.branchEntries;
    const contextWindow = ctx?.model?.contextWindow ?? 0;
    const cutId = requestCut(preparation, branchEntries, contextWindow);
    const messages = messagesForCompaction(preparation, branchEntries, cutId);
    const plan = runPrepare(resolveBinary(), messages, childEnv());
    return planCompaction({ preparation, branchEntries, contextWindow, plan });
  });

  pi.registerCommand("jev", {
    description: "设置 Jev。选中后可查看记下的对话，或留下这段对话",
    getArgumentCompletions: (prefix) => completions(loadSettings(settingsPath()), prefix),
    handler: async (args, ctx) => {
      const text = String(args ?? "").trim();
      if (text === "memory") {
        ctx.ui.notify(runMemoryStatus(resolveBinary(), childEnv()), "info");
        return;
      }
      if (text === "remember" || text.startsWith("remember ")) {
        const identity = sessionIdentity(ctx);
        const messages = messagesFromSession(sessionEntries(ctx?.sessionManager));
        if (messages.length === 0) {
          ctx.ui.notify("这段对话里还没有可留下的内容", "warning");
          return;
        }
        const saved = runCapture(resolveBinary(), {
          cwd: identity.cwd,
          session_id: identity.session_id,
          source_id: identity.session_id || "manual",
          messages,
        }, childEnv());
        const stored = Number(saved?.stored || 0);
        ctx.ui.notify(stored > 0 ? `已留下这段对话的 ${stored} 条` : "这段对话已经留下", saved ? "info" : "warning");
        return;
      }
      const path = settingsPath();
      const settings = loadSettings(path);
      let result = applyCommand(settings, args);
      if (result.needsKey) {
        const entered = await ctx.ui.input("Jev API key", "粘贴当前提供方的 key");
        if (!entered) {
          ctx.ui.notify("Jev key 未修改", "warning");
          return;
        }
        result = applyCommand(settings, `key ${entered}`);
      }
      if (result.error) {
        ctx.ui.notify(result.error, "error");
        return;
      }
      if (result.save) {
        saveSettings(path, result.settings);
      }
      ctx.ui.notify(result.message || statusText(result.settings, path), "info");
    },
  });
}

function sessionIdentity(ctx) {
  const manager = ctx?.sessionManager;
  const cwd = manager?.getCwd?.() || ctx?.cwd || "";
  const sessionID = manager?.getSessionId?.() || "";
  return { cwd, session_id: sessionID };
}
