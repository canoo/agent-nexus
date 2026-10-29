import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { DatabaseSync } from "node:sqlite";
import { createObservabilityStore } from "../../../tools/mcp/lib/observability-store.mjs";
import { nativeHostRuntimeSupported } from "../lib/platform-support.mjs";
import { stageNativeHostPackage } from "../lib/package.mjs";
import { encodeNativeMessage } from "../lib/native-messaging.mjs";

function temporaryDirectory(t) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-host-platform-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

test("only native Linux/macOS contexts pass the runtime gate", () => {
  for (const platform of ["linux", "darwin"]) {
    assert.equal(nativeHostRuntimeSupported({ platform, flatpakId: "", flatpakInfoExists: false }), true);
    assert.equal(nativeHostRuntimeSupported({ platform, flatpakId: "com.example.Browser", flatpakInfoExists: false }), false);
    assert.equal(nativeHostRuntimeSupported({ platform, flatpakId: "", flatpakInfoExists: true }), false);
  }
  for (const platform of ["win32", "freebsd", "unknown"]) {
    assert.equal(nativeHostRuntimeSupported({ platform, flatpakId: "", flatpakInfoExists: false }), false);
  }
});

for (const browser of ["chrome", "edge"]) {
  test(`staged ${browser} host rejects Flatpak before creating any store`, (t) => {
    const directory = temporaryDirectory(t);
    const outputDir = join(directory, "payload");
    stageNativeHostPackage({ outputDir, version: "0.3.0-dev.1" });
    const home = join(directory, "isolated-home");
    mkdirSync(home);
    const launcher = join(outputDir, `apps/companion-native-host/bin/nexus-companion-native-host-${browser}.mjs`);
    const result = spawnSync(launcher, [], {
      env: { ...process.env, HOME: home, FLATPAK_ID: "com.example.Browser", NODE_NO_WARNINGS: "1" },
      input: encodeNativeMessage({
        tool_id: "chatgpt", surface: "browser", started_at: new Date(Date.now()-3000).toISOString(),
        ended_at: new Date(Date.now()-2000).toISOString(), detector: "selected-browser-tab", confidence: "surface-active",
        browser_family: browser, platform: process.platform === "darwin" ? "macos" : "linux", schema_version: 1, consent_policy_version: 1,
      }),
      timeout: 10000, maxBuffer: 4096,
    });
    assert.equal(result.error, undefined);
    assert.equal(result.status, 0);
    assert.equal(result.stderr.toString(), "");
    assert.equal(result.stdout.readUInt32LE(0), result.stdout.length - 4);
    assert.deepEqual(JSON.parse(result.stdout.subarray(4).toString()), { schema_version: 1, ok: false });
    assert.equal(existsSync(join(home, ".config")), false);
  });
}

test("staged registration and removal refuse Flatpak without changing existing files", (t) => {
  const directory = temporaryDirectory(t);
  const outputDir = join(directory, "payload");
  stageNativeHostPackage({ outputDir, version: "0.3.0-dev.1" });
  const home = join(directory, "isolated-home");
  const profile = join(home, ".config/google-chrome/NativeMessagingHosts");
  mkdirSync(profile, { recursive: true });
  const keep = join(profile, "com.codelogiic.nexus.companion.json");
  writeFileSync(keep, "preserved-registration");
  const registration = join(outputDir, "apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs");
  for (const action of ["install", "uninstall"]) {
    const args = [registration, action, "--browser", "chrome"];
    if (action === "install") args.push("--extension-id", "a".repeat(32), "--host-path", join(outputDir, "apps/companion-native-host/bin/nexus-companion-native-host-chrome.mjs"));
    const result = spawnSync(process.execPath, args, {
      env: { ...process.env, HOME: home, FLATPAK_ID: "com.example.Companion" },
      encoding: "utf8", timeout: 10000,
    });
    assert.equal(result.error, undefined);
    assert.equal(result.status, 2);
    assert.equal(result.stdout, "");
    assert.equal(result.stderr, "native_host_environment_unsupported\n");
    assert.equal(readFileSync(keep, "utf8"), "preserved-registration");
  }
  assert.equal(existsSync(join(home, ".config/nexus")), false);
});


test("unsupported host leaves existing expired history and collection settings untouched", (t) => {
  const directory = temporaryDirectory(t);
  const home = join(directory, "isolated-home");
  mkdirSync(home);
  const databasePath = join(home, ".config/nexus/logs/observability.sqlite");
  const store = createObservabilityStore({ databasePath });
  assert.equal(store.databasePath, databasePath);
  store.migrate();
  const db = new DatabaseSync(databasePath);
  try {
    db.prepare(`INSERT INTO tool_activity (id, tool_id, surface, started_at, ended_at, detector, confidence, browser_family, platform, schema_version, consent_policy_version)
      VALUES ('expired-fixture', 'chatgpt', 'browser', '2000-01-01T00:00:00Z', '2000-01-01T00:01:00Z', 'selected-browser-tab', 'surface-active', 'chrome', 'linux', 1, 1)`).run();
  } finally { db.close(); }
  const launcher = join(import.meta.dirname, "../bin/nexus-companion-native-host-chrome.mjs");
  const result = spawnSync(process.execPath, [launcher], {
    env: { ...process.env, HOME: home, FLATPAK_ID: "com.example.Browser", NODE_NO_WARNINGS: "1" },
    input: Buffer.alloc(0), timeout: 10000,
  });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0);
  assert.equal(result.stderr.toString(), "");
  const check = new DatabaseSync(databasePath);
  try {
    assert.equal(check.prepare('SELECT COUNT(*) AS count FROM tool_activity').get().count, 1);
    assert.equal(check.prepare('SELECT collection_enabled FROM companion_settings WHERE id=1').get().collection_enabled, 0);
  } finally { check.close(); }
});
