import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

export const PROVIDERS = ["typesafe", "opencode-go"];
export const MODELS = {
  typesafe: ["jev-1.13.0", "jev-latest"],
  "opencode-go": ["jev-1.13", "jev-1.13-free"],
};

export function settingsPath(env = process.env, home = homedir()) {
  if (env.JEV_CM_CONFIG) {
    return env.JEV_CM_CONFIG;
  }
  const agent = env.PI_CODING_AGENT_DIR || join(home, ".pi", "agent");
  return join(agent, "jev-cm.json");
}

export function emptySettings() {
  return {
    provider: "typesafe",
    model: "",
    typesafe_api_key: "",
    opencode_go_api_key: "",
  };
}

export function normalize(raw) {
  const settings = emptySettings();
  if (!raw || typeof raw !== "object") {
    return settings;
  }
  if (raw.provider === "opencode-go" || raw.provider === "opencode-zen") {
    settings.provider = "opencode-go";
  } else if (raw.provider === "typesafe") {
    settings.provider = "typesafe";
  }
  if (typeof raw.model === "string") {
    settings.model = raw.model;
  }
  if (typeof raw.typesafe_api_key === "string") {
    settings.typesafe_api_key = raw.typesafe_api_key;
  }
  if (typeof raw.opencode_go_api_key === "string") {
    settings.opencode_go_api_key = raw.opencode_go_api_key;
  }
  return settings;
}

export function loadSettings(path) {
  try {
    return normalize(JSON.parse(readFileSync(path, "utf8")));
  } catch {
    return emptySettings();
  }
}

export function saveSettings(path, settings) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, `${JSON.stringify(normalize(settings), null, 2)}\n`, { mode: 0o600 });
}

export function keyField(provider) {
  return provider === "opencode-go" ? "opencode_go_api_key" : "typesafe_api_key";
}

export function keyLabel(key) {
  if (!key) {
    return "missing";
  }
  if (key.length <= 4) {
    return "set";
  }
  return `set (…${key.slice(-4)})`;
}

export function statusText(settings, path) {
  const current = normalize(settings);
  const model = current.model || "(provider default)";
  const lines = [
    `provider ${current.provider}`,
    `model ${model}`,
    `key ${keyLabel(current[keyField(current.provider)])}`,
  ];
  if (path) {
    lines.push(`file ${path}`);
  }
  return lines.join("\n");
}

export function applyCommand(settings, args) {
  const current = normalize(settings);
  const text = String(args ?? "").trim();
  if (text === "" || text === "status") {
    return { settings: current, message: statusText(current) };
  }
  const [command, ...rest] = text.split(/\s+/);
  const value = rest.join(" ").trim();
  if (command === "provider") {
    const provider = value === "opencode-zen" ? "opencode-go" : value;
    if (!PROVIDERS.includes(provider)) {
      return { error: "provider 只能是 typesafe 或 opencode-go" };
    }
    current.provider = provider;
    if (current.model && !MODELS[provider].includes(current.model)) {
      current.model = "";
    }
    return { settings: current, message: statusText(current), save: true };
  }
  if (command === "model") {
    if (!value) {
      current.model = "";
      return { settings: current, message: statusText(current), save: true };
    }
    if (!MODELS[current.provider].includes(value)) {
      return { error: `model 只能是 ${MODELS[current.provider].join(" 或 ")}` };
    }
    current.model = value;
    return { settings: current, message: statusText(current), save: true };
  }
  if (command === "key") {
    if (!value) {
      return { settings: current, needsKey: true };
    }
    current[keyField(current.provider)] = value;
    return { settings: current, message: statusText(current), save: true };
  }
  return { error: "用法: /jev [status | provider typesafe|opencode-go | key | model <id> | memory | remember <text>]" };
}

export function completions(settings, prefix) {
  const current = normalize(settings);
  const parts = String(prefix ?? "").trim().split(/\s+/).filter(Boolean);
  const asItems = (values, typed) => values.filter((value) => value.startsWith(typed)).map((value) => ({ value, label: value }));
  if (parts.length <= 1) {
    return asItems(["status", "provider", "key", "model", "memory", "remember"], parts[0] ?? "");
  }
  if (parts[0] === "provider" && parts.length === 2) {
    return asItems(PROVIDERS, parts[1]);
  }
  if (parts[0] === "model" && parts.length === 2) {
    return asItems(MODELS[current.provider], parts[1]);
  }
  return [];
}

export function envFromSettings(settings) {
  const current = normalize(settings);
  const env = {};
  if (current.provider) {
    env.JEV_CM_PROVIDER = current.provider;
  }
  if (current.model) {
    env.JEV_CM_MODEL = current.model;
  }
  if (current.typesafe_api_key) {
    env.TYPESAFE_API_KEY = current.typesafe_api_key;
  }
  if (current.opencode_go_api_key) {
    env.OPENCODE_GO_API_KEY = current.opencode_go_api_key;
  }
  return env;
}
