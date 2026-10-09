import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { cpSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { stageNativeHostPackage } from "../lib/package.mjs";
import { encodeNativeMessage } from "../lib/native-messaging.mjs";

const PACKAGE_CLI = join(import.meta.dirname, "../bin/nexus-companion-native-host-package.mjs");
const VERSION = "0.3.0-dev.1";
function temporaryDirectory(t) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-host-package-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}
function pathsInside(directory, prefix = "") {
  return readdirSync(directory).sort().flatMap((name) => {
    const path = prefix ? `${prefix}/${name}` : name;
    return lstatSync(join(directory, name)).isDirectory() ? pathsInside(join(directory, name), path) : [path];
  });
}
function fixtureSource(directory) {
  const staged = join(directory, "baseline");
  const manifest = stageNativeHostPackage({ outputDir: staged, version: VERSION });
  const sourceRoot = join(directory, "source");
  mkdirSync(sourceRoot);
  for (const { path } of manifest.files) {
    mkdirSync(dirname(join(sourceRoot, path)), { recursive: true });
    cpSync(join(staged, path), join(sourceRoot, path));
  }
  return sourceRoot;
}
function assertQuiet(result) {
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr?.toString());
  assert.equal(result.stderr?.toString(), "");
}

test("staged payload is complete, deterministic and excludes unrelated files", (t) => {
  const directory = temporaryDirectory(t);
  const sourceRoot = fixtureSource(directory);
  writeFileSync(join(sourceRoot, ".env"), "private-fixture");
  mkdirSync(join(sourceRoot, "logs"));
  writeFileSync(join(sourceRoot, "logs", "private.sqlite"), "not-a-database");
  const first = join(directory, "first");
  const second = join(directory, "second");
  const manifest = stageNativeHostPackage({ sourceRoot, outputDir: first, version: VERSION });
  assert.deepEqual(stageNativeHostPackage({ sourceRoot, outputDir: second, version: VERSION }), manifest);
  assert.deepEqual(pathsInside(first), [...manifest.files.map(({ path }) => path), "package-manifest.json"].sort());
  assert.equal(readFileSync(join(first, "package-manifest.json"), "utf8"), readFileSync(join(second, "package-manifest.json"), "utf8"));
  assert.doesNotMatch(JSON.stringify(manifest), /private-fixture|private.sqlite/);
  assert.equal(manifest.minimumNodeVersion, "22.13.0");
  assert.deepEqual(manifest.platforms, ["linux", "darwin"]);
  assert.equal(existsSync(join(sourceRoot, ".config")), false, "staging never accesses the store or browser profiles");
  for (const { path, sha256, mode } of manifest.files) {
    assert.equal(createHash("sha256").update(readFileSync(join(first, path))).digest("hex"), sha256);
    assert.equal(lstatSync(join(first, path)).mode & 0o777, mode);
  }
});

test("invalid versions, unknown options and existing destinations leave files unchanged", (t) => {
  const directory = temporaryDirectory(t);
  const outputDir = join(directory, "payload");
  for (const version of ["v0.3.0", "0.03.0", "0.3.0-01", "0.3.0+local", "../private", "0.3", ""]) {
    assert.throws(() => stageNativeHostPackage({ outputDir, version }), /package_input_invalid/);
  }
  assert.throws(() => stageNativeHostPackage({ outputDir, version: VERSION, dbPath: "ignored" }), /package_input_invalid/);
  assert.throws(() => stageNativeHostPackage({ outputDir: "relative", version: VERSION }), /package_input_invalid/);
  assert.equal(existsSync(outputDir), false);
  mkdirSync(outputDir);
  writeFileSync(join(outputDir, "keep"), "user-work");
  assert.throws(() => stageNativeHostPackage({ outputDir, version: VERSION }), /package_output_exists/);
  assert.equal(readFileSync(join(outputDir, "keep"), "utf8"), "user-work");
});

test("missing, unexpected or symlinked source files fail before creating output", (t) => {
  const directory = temporaryDirectory(t);
  const sourceRoot = fixtureSource(directory);
  const outputDir = join(directory, "payload");
  const migrations = join(sourceRoot, "tools/mcp/migrations");
  const unexpected = join(migrations, "unexpected.txt");
  writeFileSync(unexpected, "private-fixture");
  assert.throws(() => stageNativeHostPackage({ sourceRoot, outputDir, version: VERSION }), /package_source_invalid/);
  rmSync(unexpected);
  const license = join(sourceRoot, "LICENSE");
  rmSync(license);
  symlinkSync(join(directory, "baseline/LICENSE"), license);
  assert.throws(() => stageNativeHostPackage({ sourceRoot, outputDir, version: VERSION }), /package_source_invalid/);
  rmSync(license);
  assert.throws(() => stageNativeHostPackage({ sourceRoot, outputDir, version: VERSION }), /package_source_invalid/);
  cpSync(join(directory, "baseline/LICENSE"), license);
  rmSync(join(sourceRoot, "tools"), { recursive: true });
  symlinkSync(join(directory, "baseline/tools"), join(sourceRoot, "tools"));
  assert.throws(() => stageNativeHostPackage({ sourceRoot, outputDir, version: VERSION }), /package_source_invalid/);
  assert.equal(existsSync(outputDir), false);
});

test("package CLI validates flags and reports fixed errors without filesystem diagnostics", (t) => {
  const directory = temporaryDirectory(t);
  const outputDir = join(directory, "payload");
  const env = { ...process.env, HOME: directory, NODE_NO_WARNINGS: "1" };
  for (const args of [[], ["--output", outputDir, "--output", outputDir], ["--version", VERSION, "--unknown", "private-path"]]) {
    const result = spawnSync(process.execPath, [PACKAGE_CLI, ...args], { env, encoding: "utf8", timeout: 10000 });
    assert.equal(result.status, 2);
    assert.equal(result.stdout, "");
    assert.deepEqual(JSON.parse(result.stderr), { ok: false, error: "package_input_invalid" });
    assert.equal(existsSync(outputDir), false);
  }
  const result = spawnSync(process.execPath, [PACKAGE_CLI, "--version", VERSION, "--output", outputDir], { env, encoding: "utf8", timeout: 10000 });
  assertQuiet(result);
  const reply = JSON.parse(result.stdout);
  const manifest = JSON.parse(readFileSync(join(outputDir, "package-manifest.json"), "utf8"));
  assert.deepEqual(reply, { ok: true, formatVersion: 1, version: VERSION, fileCount: manifest.files.length });
  assert.equal(existsSync(join(directory, ".config")), false);
});

for (const browser of ["chrome", "edge"]) {
  test(`relocated ${browser} package launches, ingests and exposes the shared helper independently`, (t) => {
    const directory = temporaryDirectory(t);
    const outputDir = join(directory, "relocated payload");
    stageNativeHostPackage({ outputDir, version: VERSION });
    const home = join(directory, "isolated-home");
    mkdirSync(home);
    const env = { ...process.env, HOME: home, NODE_NO_WARNINGS: "1" };
    const options = { env, cwd: outputDir, timeout: 10000, maxBuffer: 65536 };
    // Execute the packaged store and packaged migrations, never a fixture schema.
    const initialize = spawnSync(process.execPath, ["--input-type=module", "-e", `
      import { createObservabilityStore } from './tools/mcp/lib/observability-store.mjs';
      import { DatabaseSync } from 'node:sqlite';
      import { join } from 'node:path';
      const databasePath = join(process.env.HOME, '.config/nexus/logs/observability.sqlite');
      const store = createObservabilityStore({databasePath});
      if (store.databasePath !== databasePath) throw new Error('fixture_path_invalid');
      store.migrate();
      const db = new DatabaseSync(databasePath);
      const settings = db.prepare('SELECT collection_enabled FROM companion_settings WHERE id=1').get();
      if (settings.collection_enabled !== 0) throw new Error('unexpected_collection');
      const boundary = new Date(Date.now()-10000).toISOString();
      db.prepare('UPDATE companion_settings SET collection_enabled=1, collection_started_at=? WHERE id=1').run(boundary);
      db.prepare('INSERT INTO companion_tool_consents (adapter_id,tool_id,enabled,consent_policy_version,updated_at) VALUES (?, ?, 1, 1, ?)').run('browser-${browser}', 'chatgpt', boundary);
      db.close();
    `], options);
    assertQuiet(initialize);
    const event = {
      tool_id: "chatgpt", surface: "browser", started_at: new Date(Date.now()-3000).toISOString(),
      ended_at: new Date(Date.now()-2000).toISOString(), detector: "selected-browser-tab", confidence: "surface-active",
      browser_family: browser, platform: process.platform === "darwin" ? "macos" : "linux", schema_version: 1, consent_policy_version: 1,
    };
    const launcher = join(outputDir, `apps/companion-native-host/bin/nexus-companion-native-host-${browser}.mjs`);
    // Direct executable launch verifies the shebang and payload mode, not node <source>.
    const result = spawnSync(launcher, [], { ...options, input: encodeNativeMessage(event) });
    assertQuiet(result);
    assert.equal(result.stdout.readUInt32LE(0), result.stdout.length - 4);
    assert.deepEqual(JSON.parse(result.stdout.subarray(4).toString()), { schema_version: 1, ok: true });
    const helper = spawnSync(process.execPath, [join(outputDir, "tools/mcp/companion-data.mjs"), "status"], options);
    assertQuiet(helper);
    assert.deepEqual(JSON.parse(helper.stdout.toString()), { schemaVersion: 1, ok: true, action: "status", retentionDays: 14, storedSpans: 1 });
    const db = new DatabaseSync(join(home, ".config/nexus/logs/observability.sqlite"));
    try {
      assert.equal(db.prepare("SELECT COUNT(*) AS count FROM tasks").get().count, 0);
      assert.equal(db.prepare("SELECT browser_family FROM tool_activity").get().browser_family, browser);
    } finally { db.close(); }
    assert.equal(existsSync(join(home, ".config/google-chrome")), false);
    assert.equal(existsSync(join(home, ".config/microsoft-edge")), false);
  });
}


test("staged registration install and uninstall stay browser-specific and preserve history", (t) => {
  const directory = temporaryDirectory(t);
  const outputDir = join(directory, "payload");
  stageNativeHostPackage({ outputDir, version: VERSION });
  const home = join(directory, "isolated-home");
  mkdirSync(home);
  const logs = join(home, ".config/nexus/logs");
  mkdirSync(logs, { recursive: true });
  const history = join(logs, "observability.sqlite");
  writeFileSync(history, "preserved-history-fixture");
  const env = { ...process.env, HOME: home, NODE_NO_WARNINGS: "1" };
  const registration = join(outputDir, "apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs");
  const manifests = {};
  for (const browser of ["chrome", "edge"]) {
    const hostPath = join(outputDir, `apps/companion-native-host/bin/nexus-companion-native-host-${browser}.mjs`);
    const result = spawnSync(process.execPath, [registration, "install", "--browser", browser, "--extension-id", "a".repeat(32), "--host-path", hostPath], { env, encoding: "utf8", timeout: 10000 });
    assertQuiet(result);
    manifests[browser] = result.stdout.trim();
    assert.ok(manifests[browser].startsWith(home));
    assert.deepEqual(JSON.parse(readFileSync(manifests[browser], "utf8")), {
      name: "com.codelogiic.nexus.companion", description: `NEXUS Companion ${browser} activity host (local-only)`,
      path: hostPath, type: "stdio", allowed_origins: [`chrome-extension://${"a".repeat(32)}/`],
    });
  }
  assert.notEqual(manifests.chrome, manifests.edge);
  const removed = spawnSync(process.execPath, [registration, "uninstall", "--browser", "chrome"], { env, encoding: "utf8", timeout: 10000 });
  assertQuiet(removed);
  assert.equal(existsSync(manifests.chrome), false);
  assert.equal(existsSync(manifests.edge), true);
  assert.equal(readFileSync(history, "utf8"), "preserved-history-fixture");
});
