import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import test from "node:test";
import { stageNativeHostPackage } from "../lib/package.mjs";
import { createRuntimeLaunchers } from "../lib/runtime-launchers.mjs";
import { encodeNativeMessage } from "../lib/native-messaging.mjs";
import { createObservabilityStore } from "../../../tools/mcp/lib/observability-store.mjs";

const CLI = join(import.meta.dirname, "../bin/nexus-companion-runtime-launchers.mjs");
function quote(value) { return `'${value.replaceAll("'", "'\\''")}'`; }
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "nexus-runtime-launchers-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const runtimeRoot = join(directory, "payload ' dollar$ backtick` $(ignored)");
  stageNativeHostPackage({ outputDir: runtimeRoot, version: "0.3.0-dev.1" });
  const home = join(directory, "home");
  mkdirSync(home);
  const outputDir = join(directory, "launchers ' with spaces");
  const desktopPath = join(directory, "fake desktop ' binary");
  const fakeDesktopCode = `
    import {spawnSync} from 'node:child_process';
    const result = spawnSync(process.env.NEXUS_COMPANION_NODE, [process.env.NEXUS_REPO+'/tools/mcp/companion-data.mjs', 'status'], {encoding:'utf8', stdio:['ignore','pipe','ignore']});
    process.stdout.write(JSON.stringify({root:process.env.NEXUS_REPO,node:process.env.NEXUS_COMPANION_NODE,registration:process.env.NEXUS_COMPANION_NATIVE_HOST_REGISTRATION_HELPER,args:process.argv.slice(1),status:JSON.parse(result.stdout)}));
  `;
  writeFileSync(desktopPath, `#!/bin/sh\nexec ${quote(process.execPath)} --input-type=module -e ${quote(fakeDesktopCode)} -- "$@"\n`, { mode: 0o755 });
  chmodSync(desktopPath, 0o755);
  return { directory, runtimeRoot, home, outputDir, desktopPath, nodePath: process.execPath };
}
function options(f) { return { runtimeRoot: f.runtimeRoot, outputDir: f.outputDir, desktopPath: f.desktopPath, nodePath: f.nodePath }; }
function run(f, name, args = [], input) {
  return spawnSync(join(f.outputDir, name), args, {
    env: { ...process.env, HOME: f.home, PATH: "/nonexistent-nexus-path", NODE_NO_WARNINGS: "1" },
    timeout: 10000, maxBuffer: 65536, input,
  });
}
function quiet(result) {
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr?.toString());
  assert.equal(result.stderr.toString(), "");
}

test("desktop launcher preserves literal paths/args and pins helpers with no node on PATH", (t) => {
  const f = fixture(t);
  assert.deepEqual(createRuntimeLaunchers(options(f)), { schemaVersion: 1, ok: true, launchers: ["nexus-companion-native-host-chrome", "nexus-companion-native-host-edge", "nexus-companion"] });
  const args = ["literal $HOME", "$(not-a-command)", "quote ' and `backtick`"];
  const result = run(f, "nexus-companion", args);
  quiet(result);
  const reply = JSON.parse(result.stdout.toString());
  assert.deepEqual(reply, {
    root: realpathSync(f.runtimeRoot), node: realpathSync(f.nodePath),
    registration: join(realpathSync(f.runtimeRoot), "apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs"),
    args, status: { schemaVersion: 1, ok: false, error: "companion_store_unavailable" },
  });
  assert.equal(existsSync(join(f.home, ".config")), false);
  assert.equal(existsSync(join(f.directory, "ignored")), false);
});

test("pinned Chrome/Edge wrappers ingest and desktop reads the same isolated store", (t) => {
  const f = fixture(t);
  createRuntimeLaunchers(options(f));
  const databasePath = join(f.home, ".config/nexus/logs/observability.sqlite");
  const store = createObservabilityStore({ databasePath });
  assert.equal(store.databasePath, databasePath);
  store.migrate();
  const db = new DatabaseSync(databasePath);
  try {
    const boundary = new Date(Date.now()-10000).toISOString();
    db.prepare("UPDATE companion_settings SET collection_enabled=1, collection_started_at=? WHERE id=1").run(boundary);
    for (const browser of ["chrome", "edge"]) {
      db.prepare("INSERT INTO companion_tool_consents (adapter_id,tool_id,enabled,consent_policy_version,updated_at) VALUES (?, 'chatgpt', 1, 1, ?)").run(`browser-${browser}`, boundary);
    }
  } finally { db.close(); }
  for (const browser of ["chrome", "edge"]) {
    const event = { tool_id: "chatgpt", surface: "browser", started_at: new Date(Date.now()-3000).toISOString(), ended_at: new Date(Date.now()-2000).toISOString(), detector: "selected-browser-tab", confidence: "surface-active", browser_family: browser, platform: process.platform === "darwin" ? "macos" : "linux", schema_version: 1, consent_policy_version: 1 };
    const result = run(f, `nexus-companion-native-host-${browser}`, [], encodeNativeMessage(event));
    quiet(result);
    assert.equal(result.stdout.readUInt32LE(0), result.stdout.length-4);
    assert.deepEqual(JSON.parse(result.stdout.subarray(4).toString()), { schema_version: 1, ok: true });
  }
  const result = run(f, "nexus-companion");
  quiet(result);
  assert.equal(JSON.parse(result.stdout.toString()).status.storedSpans, 2);
  assert.equal(existsSync(join(f.home, ".config/google-chrome")), false);
  assert.equal(existsSync(join(f.home, ".config/microsoft-edge")), false);
});

test("changed payload bytes and unsafe manifest paths fail before creating launchers", (t) => {
  const f = fixture(t);
  const path = join(f.runtimeRoot, "apps/companion-native-host/lib/host.mjs");
  const bytes = readFileSync(path);
  writeFileSync(path, "changed-payload");
  assert.throws(() => createRuntimeLaunchers(options(f)), /launcher_payload_invalid/);
  assert.equal(existsSync(f.outputDir), false);
  writeFileSync(path, bytes);
  const manifestPath = join(f.runtimeRoot, "package-manifest.json");
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  manifest.files[0].path = "../../private-file";
  writeFileSync(manifestPath, JSON.stringify(manifest));
  assert.throws(() => createRuntimeLaunchers(options(f)), /launcher_payload_invalid/);
  assert.equal(existsSync(f.outputDir), false);
});

test("incompatible runtime, relative executable and existing output fail closed", (t) => {
  const f = fixture(t);
  assert.throws(() => createRuntimeLaunchers({ ...options(f), desktopPath: "relative" }), /launcher_input_invalid/);
  const nodePath = join(f.directory, "fake-node");
  writeFileSync(nodePath, '#!/bin/sh\nprintf \'{"version":"v20.0.0","sqlite":true}\'\n', { mode: 0o755 });
  assert.throws(() => createRuntimeLaunchers({ ...options(f), nodePath }), /launcher_runtime_unavailable/);
  assert.equal(existsSync(f.outputDir), false);
  mkdirSync(f.outputDir);
  writeFileSync(join(f.outputDir, "keep"), "user-work");
  assert.throws(() => createRuntimeLaunchers(options(f)), /launcher_output_exists/);
  assert.equal(readFileSync(join(f.outputDir, "keep"), "utf8"), "user-work");
});

test("launcher CLI validates flags and rejects Flatpak with fixed responses", (t) => {
  const f = fixture(t);
  const env = { ...process.env, HOME: f.home };
  const invalid = spawnSync(process.execPath, [CLI, "--unknown", "private-value"], { env, encoding: "utf8", timeout: 10000 });
  assert.equal(invalid.status, 2);
  assert.equal(invalid.stdout, "");
  assert.deepEqual(JSON.parse(invalid.stderr), { schemaVersion: 1, ok: false, error: "launcher_input_invalid" });
  const unsupported = spawnSync(process.execPath, [CLI, "--runtime-root", f.runtimeRoot, "--output", f.outputDir, "--node-path", f.nodePath, "--desktop-path", f.desktopPath], {
    env: { ...env, FLATPAK_ID: "com.example.Companion" }, encoding: "utf8", timeout: 10000,
  });
  assert.equal(unsupported.status, 1);
  assert.equal(unsupported.stdout, "");
  assert.deepEqual(JSON.parse(unsupported.stderr), { schemaVersion: 1, ok: false, error: "launcher_environment_unsupported" });
  assert.equal(existsSync(f.outputDir), false);
  assert.equal(existsSync(join(f.home, ".config")), false);
});
