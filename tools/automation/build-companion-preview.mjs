#!/usr/bin/env node
// Build native development artifacts from an isolated, fixed source allowlist.
import { chmodSync, copyFileSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { stageNativeHostPackage } from "../../apps/companion-native-host/lib/package.mjs";
import { nativeHostRuntimeSupported } from "../../apps/companion-native-host/lib/platform-support.mjs";

const ROOT = fileURLToPath(new URL("../../", import.meta.url));
const DESKTOP = join(ROOT, "apps/companion-desktop");
const SOURCE_FILES = [
  "package.json", "src-tauri/Cargo.toml", "src-tauri/Cargo.lock", "src-tauri/build.rs",
  "src-tauri/tauri.conf.json", "src-tauri/tauri.linux.conf.json", "src-tauri/tauri.macos.conf.json", "src-tauri/capabilities/default.json", "src-tauri/icons/icon.png",
  "src-tauri/src/main.rs", "src-tauri/src/companion_data.rs", "src-tauri/src/companion_runtime.rs", "src-tauri/src/companion_registration.rs",
  "ui/index.html", "ui/dashboard.js", "ui/styles.css",
];
const INSTALLER_FILES = [
  "bin/nexus-companion-runtime-launchers.mjs", "lib/runtime-launchers.mjs",
  "lib/launcher-render.mjs", "lib/platform-support.mjs", "lib/package.mjs",
];
function copyRegular(source, target, mode = 0o644) {
  if (!lstatSync(source).isFile() || lstatSync(source).isSymbolicLink()) throw new Error("preview_source_invalid");
  mkdirSync(dirname(target), { recursive: true });
  copyFileSync(source, target);
  chmodSync(target, mode);
}
function run(args, cwd) {
  const cli = join(DESKTOP, "node_modules/@tauri-apps/cli/tauri.js");
  const result = spawnSync(process.execPath, [cli, ...args], { cwd, stdio: "inherit" });
  if (result.error || result.status !== 0) throw new Error("preview_build_failed");
}
function filesAt(root, path = "") {
  const result = [];
  for (const entry of readdirSync(join(root, path)).sort()) {
    const name = path ? `${path}/${entry}` : entry;
    const stat = lstatSync(join(root, name));
    if (stat.isSymbolicLink()) throw new Error("preview_source_invalid");
    if (stat.isDirectory()) result.push(...filesAt(root, name));
    else if (stat.isFile()) result.push(name);
    else throw new Error("preview_source_invalid");
  }
  return result;
}
function main() {
  const args = process.argv.slice(2);
  const options = {};
  if (args.length !== 4) throw new Error("preview_input_invalid");
  for (let i = 0; i < args.length; i += 2) {
    const key = { "--version": "version", "--output": "output" }[args[i]];
    if (!key || Object.hasOwn(options, key)) throw new Error("preview_input_invalid");
    options[key] = args[i + 1];
  }
  const { version, output } = options;
  // Development artifacts always have a prerelease identifier; no release publishing.
  if (!/^0\.3\.0-(?:[a-zA-Z-][a-zA-Z0-9-]*)(?:\.(?:0|[1-9]\d*))*$/.test(version)
      || !isAbsolute(output) || existsSync(output) || !nativeHostRuntimeSupported()) throw new Error("preview_input_invalid");
  const destination = join(realpathSync(dirname(output)), relative(dirname(output), resolve(output)));
  const sourceRelative = relative(realpathSync(ROOT), destination);
  if (!sourceRelative || (!sourceRelative.startsWith("../") && !isAbsolute(sourceRelative))) throw new Error("preview_input_invalid");
  if (process.env.CARGO_TARGET_DIR && !isAbsolute(process.env.CARGO_TARGET_DIR)) throw new Error("preview_input_invalid");
  const temporary = mkdtempSync(join(tmpdir(), "nexus-preview-build-"));
  try {
    for (const path of SOURCE_FILES) copyRegular(join(DESKTOP, path), join(temporary, path));
    const cargoPath = join(temporary, "src-tauri/Cargo.toml");
    writeFileSync(cargoPath, readFileSync(cargoPath, "utf8").replace(/^version = "[^"]+"$/m, `version = "${version}"`));
    const lockPath = join(temporary, "src-tauri/Cargo.lock");
    writeFileSync(lockPath, readFileSync(lockPath, "utf8").replace(/(name = "nexus-companion"\nversion = )"[^"]+"/, `$1"${version}"`));
    const configPath = join(temporary, "src-tauri/tauri.conf.json");
    const config = JSON.parse(readFileSync(configPath, "utf8"));
    config.version = version;
    config.bundle = { active: true, icon: [join(temporary, "icons/icon.png"), join(temporary, "icons/icon.icns")] };
    writeFileSync(configPath, JSON.stringify(config));
    run(["icon", join(temporary, "src-tauri/icons/icon.png"), "--output", join(temporary, "icons")], temporary);
    const bundle = process.platform === "darwin" ? "app" : "deb";
    run(["build", "--ci", "--no-sign", "--bundles", bundle, "--", "--locked"], temporary);
    const target = process.env.CARGO_TARGET_DIR || join(temporary, "src-tauri/target");
    const bundleRoot = join(target, "release/bundle", bundle === "app" ? "macos" : "deb");
    const expectedName = "NEXUS Companion.app";
    const artifacts = readdirSync(bundleRoot).filter((name) => bundle === "app" ? name === expectedName : name.endsWith(".deb") && name.includes(version));
    if (artifacts.length !== 1) throw new Error("preview_artifact_invalid");
    // Exclusive output: never replace an existing directory or registration.
    mkdirSync(destination, { mode: 0o755 });
    const artifact = join(bundleRoot, artifacts[0]);
    if (bundle === "app") {
      for (const path of filesAt(artifact)) {
        const mode = lstatSync(join(artifact, path)).mode & 0o111 ? 0o755 : 0o644;
        copyRegular(join(artifact, path), join(destination, "desktop", expectedName, path), mode);
      }
    } else copyRegular(artifact, join(destination, "desktop", artifacts[0]));
    stageNativeHostPackage({ outputDir: join(destination, "host"), version });
    for (const path of INSTALLER_FILES) copyRegular(join(ROOT, "apps/companion-native-host", path), join(destination, "installer/apps/companion-native-host", path));
    copyRegular(join(ROOT, "docs/companion-preview-install.md"), join(destination, "README.md"));
    copyRegular(join(ROOT, "LICENSE"), join(destination, "LICENSE"));
    const manifest = {
      formatVersion: 1, version, platform: process.platform, arch: process.arch,
      minimumNodeVersion: "22.13.0", nodeBundled: false, signed: false, liveGuiVerified: false,
      files: filesAt(destination).map((path) => ({ path, sha256: createHash("sha256").update(readFileSync(join(destination, path))).digest("hex"), mode: lstatSync(join(destination, path)).mode & 0o777 })),
    };
    writeFileSync(join(destination, "preview-manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, { flag: "wx", mode: 0o644 });
    process.stdout.write(`${JSON.stringify({ ok: true, version, platform: process.platform, arch: process.arch })}\n`);
  } finally { rmSync(temporary, { recursive: true, force: true }); }
}
try { main(); }
catch (error) {
  const allowed = ["preview_source_invalid", "preview_build_failed", "preview_input_invalid", "preview_artifact_invalid"];
  process.stderr.write(`${JSON.stringify({ ok: false, error: allowed.includes(error?.message) ? error.message : "preview_build_failed" })}\n`);
  process.exitCode = 1;
}
