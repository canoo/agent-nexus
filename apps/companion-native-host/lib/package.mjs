import { chmodSync, lstatSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_SOURCE_ROOT = fileURLToPath(new URL("../../../", import.meta.url));
const FIXED_FILES = [
  "LICENSE",
  "docs/companion-support-matrix.md",
  "docs/nexus-companion.md",
  "docs/observability-schema.md",
  "docs/v0.3.0-work-tracker.md",
  "apps/companion-native-host/README.md",
  "apps/companion-native-host/package.json",
  "apps/companion-native-host/bin/nexus-companion-native-host-chrome.mjs",
  "apps/companion-native-host/bin/nexus-companion-native-host-edge.mjs",
  "apps/companion-native-host/bin/nexus-companion-native-host-registration.mjs",
  "apps/companion-native-host/lib/host.mjs",
  "apps/companion-native-host/lib/native-messaging.mjs",
  "apps/companion-native-host/lib/platform-support.mjs",
  "apps/companion-native-host/lib/registration.mjs",
  "tools/mcp/companion-data.mjs",
  "tools/mcp/lib/observability-store.mjs",
];
const MIGRATIONS_PATH = "tools/mcp/migrations";
const SAFE_CODES = new Set(["package_input_invalid", "package_output_exists", "package_source_invalid", "package_write_failed"]);

function failure(code) { return new Error(code); }

function validVersion(value) {
  if (typeof value !== "string" || value.length > 128) return false;
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*))?$/.exec(value);
  return Boolean(match) && (!match[4] || match[4].split(".").every((part) => !/^0\d+$/.test(part)));
}

// Only allowlisted regular files can enter the payload. Never follow a source
// symlink to logs, secrets or an unrelated tree, including parent directories.
function assertSourcePath(root, path, directory = false) {
  const parts = path.split("/");
  let current = root;
  for (let index = 0; index < parts.length; index += 1) {
    current = join(current, parts[index]);
    const stat = lstatSync(current);
    const expectDirectory = index < parts.length - 1 || directory;
    if (stat.isSymbolicLink() || (expectDirectory ? !stat.isDirectory() : !stat.isFile())) {
      throw failure("package_source_invalid");
    }
  }
  return current;
}

/** Stages a relocatable source payload; no registration or store access occurs. */
export function stageNativeHostPackage(options = {}) {
  if (!options || typeof options !== "object" || Array.isArray(options)
      || Object.keys(options).some((key) => !["outputDir", "version", "sourceRoot"].includes(key))) {
    throw failure("package_input_invalid");
  }
  const { outputDir, version, sourceRoot = DEFAULT_SOURCE_ROOT } = options;
  if (typeof outputDir !== "string" || !isAbsolute(outputDir) || !validVersion(version)
      || typeof sourceRoot !== "string" || !isAbsolute(sourceRoot)) throw failure("package_input_invalid");
  const root = resolve(sourceRoot);
  const output = resolve(outputDir);
  const outputRelative = relative(root, output);
  if (!outputRelative || (!outputRelative.startsWith("../") && !isAbsolute(outputRelative))) {
    throw failure("package_input_invalid");
  }
  try {
    lstatSync(output);
    throw failure("package_output_exists");
  } catch (error) {
    if (error.message === "package_output_exists") throw error;
    if (error.code !== "ENOENT") throw failure("package_input_invalid");
  }

  let payload;
  try {
    const rootStat = lstatSync(root);
    if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw failure("package_source_invalid");
    const migrationsDirectory = assertSourcePath(root, MIGRATIONS_PATH, true);
    const migrations = readdirSync(migrationsDirectory).sort();
    if (!migrations.length || migrations.some((name) => !/^\d{3}_[a-z0-9_-]+\.sql$/.test(name))) {
      throw failure("package_source_invalid");
    }
    const paths = [...FIXED_FILES, ...migrations.map((name) => `${MIGRATIONS_PATH}/${name}`)].sort();
    payload = paths.map((path) => {
      const bytes = readFileSync(assertSourcePath(root, path));
      const mode = path.startsWith("apps/companion-native-host/bin/") ? 0o755 : 0o644;
      return { path, bytes, mode, sha256: createHash("sha256").update(bytes).digest("hex") };
    });
  } catch {
    throw failure("package_source_invalid");
  }
  const manifest = {
    formatVersion: 1, version, minimumNodeVersion: "22.13.0", platforms: ["linux", "darwin"],
    files: payload.map(({ path, sha256, mode }) => ({ path, sha256, mode })),
  };
  // Exclusive creation: existing files/directories are never replaced.
  try {
    mkdirSync(output, { mode: 0o755 });
  } catch (error) {
    throw failure(error.code === "EEXIST" ? "package_output_exists" : "package_write_failed");
  }
  try {
    chmodSync(output, 0o755);
    for (const { path, bytes, mode } of payload) {
      let directory = output;
      for (const part of path.split("/").slice(0, -1)) {
        directory = join(directory, part);
        mkdirSync(directory, { recursive: true, mode: 0o755 });
        chmodSync(directory, 0o755);
      }
      const target = join(output, path);
      writeFileSync(target, bytes, { flag: "wx", mode });
      chmodSync(target, mode);
    }
    const manifestPath = join(output, "package-manifest.json");
    writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, { flag: "wx", mode: 0o644 });
    chmodSync(manifestPath, 0o644);
  } catch {
    throw failure("package_write_failed");
  }
  return manifest;
}

export function packageErrorCode(error) {
  return SAFE_CODES.has(error?.message) ? error.message : "package_write_failed";
}
