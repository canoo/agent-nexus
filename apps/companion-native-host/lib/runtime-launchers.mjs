import { accessSync, chmodSync, constants, lstatSync, mkdirSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { isAbsolute, join } from "node:path";
import { spawnSync } from "node:child_process";
import { NATIVE_HOST_PAYLOAD_FILES } from "./package.mjs";
import { nativeHostRuntimeSupported } from "./platform-support.mjs";
import { renderRuntimeLaunchers } from "./launcher-render.mjs";

const ERROR_CODES = new Set(["launcher_input_invalid", "launcher_output_exists", "launcher_payload_invalid", "launcher_runtime_unavailable", "launcher_environment_unsupported", "launcher_write_failed"]);
function failure(code) { return new Error(code); }
function boundedRead(path, maxBytes) {
  const stat = lstatSync(path);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size > maxBytes) throw failure("launcher_payload_invalid");
  return readFileSync(path);
}
function verifiedRoot(runtimeRoot) {
  try {
    const root = realpathSync(runtimeRoot);
    const manifest = JSON.parse(boundedRead(join(root, "package-manifest.json"), 65536));
    if (!manifest || manifest.formatVersion !== 1 || manifest.minimumNodeVersion !== "22.13.0"
        || !Array.isArray(manifest.platforms) || !manifest.platforms.includes(process.platform)
        || !Array.isArray(manifest.files) || manifest.files.length > 128) throw failure("launcher_payload_invalid");
    const seen = new Set();
    for (const file of manifest.files) {
      if (!file || typeof file.path !== "string" || seen.has(file.path)
          || (!NATIVE_HOST_PAYLOAD_FILES.includes(file.path) && !/^tools\/mcp\/migrations\/\d{3}_[a-z0-9_-]+\.sql$/.test(file.path))
          || typeof file.sha256 !== "string" || !/^[a-f0-9]{64}$/.test(file.sha256)
          || file.mode !== (file.path.startsWith("apps/companion-native-host/bin/") ? 0o755 : 0o644)) throw failure("launcher_payload_invalid");
      seen.add(file.path);
      let path = root;
      const parts = file.path.split("/");
      for (const part of parts.slice(0, -1)) {
        path = join(path, part);
        const stat = lstatSync(path);
        if (stat.isSymbolicLink() || !stat.isDirectory()) throw failure("launcher_payload_invalid");
      }
      path = join(path, parts.at(-1));
      const bytes = boundedRead(path, 1024 * 1024);
      if (createHash("sha256").update(bytes).digest("hex") !== file.sha256) throw failure("launcher_payload_invalid");
    }
    if (NATIVE_HOST_PAYLOAD_FILES.some((path) => !seen.has(path))
        || !manifest.files.some(({ path }) => path.startsWith("tools/mcp/migrations/"))) throw failure("launcher_payload_invalid");
    return root;
  } catch { throw failure("launcher_payload_invalid"); }
}
function executable(path) {
  const resolved = realpathSync(path);
  if (!lstatSync(resolved).isFile()) throw failure("launcher_input_invalid");
  accessSync(resolved, constants.X_OK);
  return resolved;
}
function verifiedNode(path) {
  try {
    const node = executable(path);
    const result = spawnSync(node, ["--input-type=module", "-e", "import {DatabaseSync} from 'node:sqlite';process.stdout.write(JSON.stringify({version:process.version,sqlite:typeof DatabaseSync==='function'}));"], {
      encoding: "utf8", timeout: 10000, maxBuffer: 4096, stdio: ["ignore", "pipe", "ignore"],
    });
    if (result.error || result.status !== 0) throw failure("launcher_runtime_unavailable");
    const reply = JSON.parse(result.stdout);
    const match = /^v(\d+)\.(\d+)\.(\d+)$/.exec(reply.version);
    if (!match || reply.sqlite !== true || Number(match[1]) < 22 || (Number(match[1]) === 22 && Number(match[2]) < 13)) {
      throw failure("launcher_runtime_unavailable");
    }
    return node;
  } catch { throw failure("launcher_runtime_unavailable"); }
}

/** Explicit install-time binding; it does not run the GUI or register any host. */
export function createRuntimeLaunchers(options = {}) {
  if (!options || typeof options !== "object" || Array.isArray(options)
      || Object.keys(options).some((key) => !["runtimeRoot", "outputDir", "nodePath", "desktopPath"].includes(key))) throw failure("launcher_input_invalid");
  const { runtimeRoot, outputDir, nodePath, desktopPath } = options;
  if (![runtimeRoot, outputDir, nodePath, desktopPath].every((path) => typeof path === "string" && isAbsolute(path) && !path.includes("\0"))) {
    throw failure("launcher_input_invalid");
  }
  if (!nativeHostRuntimeSupported()) throw failure("launcher_environment_unsupported");
  try {
    lstatSync(outputDir);
    throw failure("launcher_output_exists");
  } catch (error) {
    if (error.message === "launcher_output_exists") throw error;
    if (error.code !== "ENOENT") throw failure("launcher_input_invalid");
  }
  const root = verifiedRoot(runtimeRoot);
  const node = verifiedNode(nodePath);
  let desktop;
  try { desktop = executable(desktopPath); } catch { throw failure("launcher_input_invalid"); }
  const scripts = renderRuntimeLaunchers({ runtimeRoot: root, nodePath: node, desktopPath: desktop });
  const names = { chrome: "nexus-companion-native-host-chrome", edge: "nexus-companion-native-host-edge", desktop: "nexus-companion" };
  try { mkdirSync(outputDir, { mode: 0o755 }); }
  catch (error) { throw failure(error.code === "EEXIST" ? "launcher_output_exists" : "launcher_write_failed"); }
  try {
    chmodSync(outputDir, 0o755);
    for (const [key, name] of Object.entries(names)) {
      const path = join(outputDir, name);
      writeFileSync(path, scripts[key], { flag: "wx", mode: 0o755 });
      chmodSync(path, 0o755);
    }
  } catch { throw failure("launcher_write_failed"); }
  return { schemaVersion: 1, ok: true, launchers: Object.values(names) };
}

export function launcherErrorCode(error) {
  return ERROR_CODES.has(error?.message) ? error.message : "launcher_write_failed";
}
