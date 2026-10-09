#!/usr/bin/env node
// Verify trusted locally built artifacts; never launch the desktop GUI or install packages.
import assert from "node:assert/strict";
import { DatabaseSync } from "node:sqlite";
import { createHash } from "node:crypto";
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync } from "node:fs";
import { isAbsolute, join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

import { EXTENSION_PAYLOAD_FILES } from "./lib/companion-extension-package.mjs";

assert.equal(process.argv.length, 3);
assert.ok(isAbsolute(process.argv[2]));
const root = realpathSync(process.argv[2]);
const manifest = JSON.parse(readFileSync(join(root, "preview-manifest.json"), "utf8"));
assert.equal(manifest.platform, process.platform);
assert.equal(manifest.arch, process.arch);
assert.equal(manifest.signed, false);
assert.equal(manifest.nodeBundled, false);
assert.equal(manifest.extensionBundled, true);
assert.equal(manifest.extensionDistribution, "unpacked-development");
assert.equal(manifest.liveGuiVerified, false);
assert.equal(manifest.minimumNodeVersion, "22.13.0");
assert.equal(manifest.formatVersion, 1);
const paths = new Set();
for (const file of manifest.files) {
  assert.ok(typeof file.path === "string" && !isAbsolute(file.path) && file.path.split("/").every((part) => part && part !== "." && part !== ".."));
  assert.ok(!paths.has(file.path));
  paths.add(file.path);
  const stat = lstatSync(join(root, file.path));
  assert.ok(stat.isFile() && !stat.isSymbolicLink());
  assert.equal(stat.mode & 0o777, file.mode);
  assert.equal(createHash("sha256").update(readFileSync(join(root, file.path))).digest("hex"), file.sha256);
}
const extensionPaths = manifest.files.filter(file => file.path.startsWith("extension/")).map(file => file.path.slice(10)).sort();
assert.deepEqual(extensionPaths, [...EXTENSION_PAYLOAD_FILES].sort());
const extension = JSON.parse(readFileSync(join(root, "extension/manifest.json"), "utf8"));
assert.equal(extension.manifest_version, 3);
assert.equal(extension.version, "0.3.0");
assert.equal(extension.version_name, manifest.version);
assert.deepEqual(extension.permissions, ["storage", "nativeMessaging"]);
assert.equal(extension.content_scripts, undefined);
assert.equal(extension.host_permissions, undefined);
assert.equal(extension.key, undefined);
assert.deepEqual(extension.background, { service_worker: "background.js", type: "module" });
assert.deepEqual(extension.optional_host_permissions, [
  "https://chatgpt.com/*", "https://chat.openai.com/*", "https://claude.ai/*",
  "https://gemini.google.com/*", "https://copilot.microsoft.com/*", "https://www.perplexity.ai/*",
]);

const directory = mkdtempSync(join(tmpdir(), "nexus-preview-verify-"));
try {
  const home = join(directory, "home");
  mkdirSync(home);
  const env = { ...process.env, HOME: home, NODE_NO_WARNINGS: "1" };
  let desktop;
  if (process.platform === "darwin") {
    desktop = join(root, "desktop/NEXUS Companion.app/Contents/MacOS/nexus-companion");
    assert.ok(paths.has("desktop/NEXUS Companion.app/Contents/Info.plist"));
  } else {
    const packages = readdirSync(join(root, "desktop")).filter((name) => name.endsWith(".deb"));
    assert.equal(packages.length, 1);
    const archive = spawnSync("ar", ["p", join(root, "desktop", packages[0]), "data.tar.gz"], { timeout: 10000, maxBuffer: 64 * 1024 * 1024 });
    assert.equal(archive.status, 0);
    const extracted = join(directory, "desktop");
    mkdirSync(extracted);
    const extraction = spawnSync("tar", ["-xzf", "-", "-C", extracted], { input: archive.stdout, timeout: 10000 });
    assert.equal(extraction.status, 0);
    desktop = join(extracted, "usr/bin/nexus-companion");
  }
  assert.ok(lstatSync(desktop).isFile());
  assert.ok(lstatSync(desktop).mode & 0o111);
  const output = join(directory, "launchers with spaces");
  const setup = spawnSync(process.execPath, [join(root, "installer/apps/companion-native-host/bin/nexus-companion-runtime-launchers.mjs"),
    "--runtime-root", join(root, "host"), "--output", output, "--node-path", process.execPath, "--desktop-path", desktop],
  { env, encoding: "utf8", timeout: 15000 });
  assert.equal(setup.status, 0, setup.stderr);
  assert.equal(JSON.parse(setup.stdout).ok, true);
  for (const browser of ["chrome", "edge"]) {
    const message = Buffer.from("{}");
    const prefix = Buffer.alloc(4);
    prefix.writeUInt32LE(message.length);
    const result = spawnSync(join(output, `nexus-companion-native-host-${browser}`), [], {
      env: { ...env, PATH: "/nonexistent-nexus-path" }, input: Buffer.concat([prefix, message]), timeout: 10000,
    });
    assert.equal(result.status, 0);
    assert.equal(result.stderr.length, 0);
    assert.equal(result.stdout.readUInt32LE(0), result.stdout.length - 4);
    assert.deepEqual(JSON.parse(result.stdout.subarray(4)), { schema_version: 1, ok: false });
  }
  const helper = spawnSync(process.execPath, [join(root, "host/tools/mcp/companion-data.mjs"), "status"], { env, encoding: "utf8", timeout: 10000 });
  assert.equal(helper.status, 1);
  assert.deepEqual(JSON.parse(helper.stdout), { schemaVersion: 1, ok: false, error: "companion_store_unavailable" });
  assert.equal(existsSync(join(home, ".config")), false);
  const initialized = spawnSync(process.execPath, [join(root, "host/tools/mcp/companion-data.mjs"), "initialize", "--confirm"], { env, encoding: "utf8", timeout: 10000 });
  assert.equal(initialized.status, 0);
  assert.deepEqual(JSON.parse(initialized.stdout), { schemaVersion: 1, ok: true, action: "initialize", retentionDays: 14, storedSpans: 0 });
  const database = new DatabaseSync(join(home, ".config/nexus/logs/observability.sqlite"), { readOnly: true });
  try {
    assert.equal(database.prepare("SELECT collection_enabled FROM companion_settings WHERE id=1").get().collection_enabled, 0);
    assert.equal(database.prepare("SELECT COUNT(*) n FROM companion_tool_consents").get().n, 0);
  } finally { database.close(); }
  const repeated = spawnSync(process.execPath, [join(root, "host/tools/mcp/companion-data.mjs"), "initialize", "--confirm"], { env, encoding: "utf8", timeout: 10000 });
  assert.equal(repeated.status, 1);
  assert.equal(JSON.parse(repeated.stdout).error, "companion_store_exists");
  // Run the same old-store process regressions against this exact archived host,
  // rather than staging another copy from the checkout.
  const upgradeTests = new URL('../../apps/companion-native-host/test/packaged-upgrade.test.mjs', import.meta.url);
  const upgrade = spawnSync(process.execPath, ['--test', fileURLToPath(upgradeTests)], {
    env: {...env, NEXUS_TEST_HOST_PAYLOAD: join(root, 'host')}, encoding: 'utf8', timeout: 30000, maxBuffer: 65536,
  });
  assert.equal(upgrade.status, 0, upgrade.stdout + upgrade.stderr);
  process.stdout.write("Preview hashes/modes, unpacked extension policy/version, packaged executable, runtime binding, fail-closed host/helper and confirmed fresh setup and old-store upgrade checks passed; GUI not launched.\n");
} finally { rmSync(directory, { recursive: true, force: true }); }
