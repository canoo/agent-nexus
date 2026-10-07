#!/usr/bin/env node
// Verify trusted locally built artifacts; never launch the desktop GUI or install packages.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync } from "node:fs";
import { isAbsolute, join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";

assert.equal(process.argv.length, 3);
assert.ok(isAbsolute(process.argv[2]));
const root = realpathSync(process.argv[2]);
const manifest = JSON.parse(readFileSync(join(root, "preview-manifest.json"), "utf8"));
assert.equal(manifest.platform, process.platform);
assert.equal(manifest.arch, process.arch);
assert.equal(manifest.signed, false);
assert.equal(manifest.nodeBundled, false);
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
  process.stdout.write("Preview hashes/modes, packaged executable, runtime binding and fail-closed host/helper checks passed; GUI not launched.\n");
} finally { rmSync(directory, { recursive: true, force: true }); }
