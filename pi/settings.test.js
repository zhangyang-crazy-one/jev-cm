import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { applyCommand, completions, envFromSettings, loadSettings, saveSettings, settingsPath } from "./settings.js";

test("slash command stores the provider and hides the key", () => {
  const switched = applyCommand({}, "provider opencode-go");
  assert.equal(switched.save, true);
  assert.equal(switched.settings.provider, "opencode-go");
  const keyed = applyCommand(switched.settings, "key go-secret-1234");
  assert.equal(keyed.settings.opencode_go_api_key, "go-secret-1234");
  assert.equal(keyed.message.includes("go-secret-1234"), false);
  assert.match(keyed.message, /set \(…1234\)/);
  assert.equal(applyCommand(keyed.settings, "key").needsKey, true);
  assert.deepEqual(envFromSettings(keyed.settings), {
    JEV_CM_PROVIDER: "opencode-go",
    OPENCODE_GO_API_KEY: "go-secret-1234",
  });
});

test("model must belong to the selected provider", () => {
  const bad = applyCommand({ provider: "typesafe" }, "model jev-1.13-free");
  assert.match(bad.error, /jev-1.13.0/);
  const ok = applyCommand({ provider: "opencode-go" }, "model jev-1.13-free");
  assert.equal(ok.settings.model, "jev-1.13-free");
});

test("settings file is private and round-trips", () => {
  const dir = mkdtempSync(join(tmpdir(), "jev-cm-"));
  const path = join(dir, "agent", "jev-cm.json");
  assert.equal(settingsPath({ JEV_CM_CONFIG: path }), path);
  saveSettings(path, { provider: "typesafe", typesafe_api_key: "ts-secret" });
  assert.equal(statSync(path).mode & 0o077, 0);
  assert.equal(loadSettings(path).typesafe_api_key, "ts-secret");
  assert.equal(readFileSync(path, "utf8").includes("ts-secret"), true);
  assert.deepEqual(completions({ provider: "typesafe" }, "model jev-l"), [{ value: "jev-latest", label: "jev-latest" }]);
});
