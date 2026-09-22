import { messagesForCompaction, planCompaction, requestCut, resolveBinary, runPrepare } from "./compact.js";
import { applyInjection, candidatesFromMessages, runCapture, runInject, runMemoryStatus } from "./memory.js";
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
    description: "设置 Jev。选中后可查看记下的对话，或记住一句原文",
    getArgumentCompletions: (prefix) => completions(loadSettings(settingsPath()), prefix),
    handler: async (args, ctx) => {
      const text = String(args ?? "").trim();
      if (text === "memory") {
        ctx.ui.notify(runMemoryStatus(resolveBinary(), childEnv()), "info");
        return;
      }
      if (text === "remember" || text.startsWith("remember ")) {
        let body = text.slice("remember".length).trim();
        if (!body) {
          body = await ctx.ui.input("记住原文", "输入要写入全局记忆的句子");
        }
        if (!body) {
          ctx.ui.notify("未写入", "warning");
          return;
        }
        const identity = sessionIdentity(ctx);
        const saved = runCapture(resolveBinary(), {
          cwd: identity.cwd,
          session_id: identity.session_id,
          source_id: "manual",
          messages: [{ role: "user", content: body }],
        }, childEnv());
        ctx.ui.notify(saved?.stored ? "已写入全局记忆" : "未写入", saved?.stored ? "info" : "warning");
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
