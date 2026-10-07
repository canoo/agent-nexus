import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, isAbsolute, join } from "node:path";
import { nativeHostRuntimeSupported } from "./platform-support.mjs";

export const NATIVE_HOST_NAME = "com.codelogiic.nexus.companion";
const EXTENSION_ID = /^[a-p]{32}$/;
const BROWSERS = new Set(["chrome", "edge"]);

function assertBrowser(browser) {
  if (!BROWSERS.has(browser)) throw new TypeError("browser must be chrome or edge");
  return browser;
}

function validatedExtensionIds(extensionIds) {
  if (!Array.isArray(extensionIds) || extensionIds.length === 0) {
    throw new TypeError("at least one published extension ID is required");
  }
  const unique = new Set();
  for (const extensionId of extensionIds) {
    if (typeof extensionId !== "string" || !EXTENSION_ID.test(extensionId)) {
      throw new TypeError("extension IDs must be Chrome-format published IDs");
    }
    unique.add(extensionId);
  }
  return [...unique];
}

/** Chrome and Edge are deliberately registered to different browser-owned paths. */
export function nativeMessagingDirectory({ browser, platform = process.platform, home = homedir() } = {}) {
  assertBrowser(browser);
  if (typeof home !== "string" || home.length === 0) throw new TypeError("home must be a local directory");
  if (platform === "linux") {
    return browser === "chrome"
      ? join(home, ".config", "google-chrome", "NativeMessagingHosts")
      : join(home, ".config", "microsoft-edge", "NativeMessagingHosts");
  }
  if (platform === "darwin") {
    return browser === "chrome"
      ? join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts")
      : join(home, "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts");
  }
  throw new Error("native host registration currently supports Linux and macOS only");
}

export function manifestPath(options) {
  return join(nativeMessagingDirectory(options), `${NATIVE_HOST_NAME}.json`);
}

export function createNativeHostManifest({ browser, extensionIds, hostPath } = {}) {
  assertBrowser(browser);
  if (typeof hostPath !== "string" || !isAbsolute(hostPath)) {
    throw new TypeError("hostPath must be an absolute executable path");
  }
  return Object.freeze({
    name: NATIVE_HOST_NAME,
    description: `NEXUS Companion ${browser} activity host (local-only)`,
    path: hostPath,
    type: "stdio",
    allowed_origins: validatedExtensionIds(extensionIds)
      .map((extensionId) => `chrome-extension://${extensionId}/`),
  });
}

/**
 * Registration is intentionally opt-in. Package setup and tests only build
 * manifests; this explicit action is the sole writer of browser-owned paths.
 */
export function installNativeHost(options) {
  if (!nativeHostRuntimeSupported()) throw new Error("native_host_environment_unsupported");
  const path = manifestPath(options);
  const manifest = createNativeHostManifest(options);
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  writeFileSync(path, `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 });
  return path;
}

/** Uninstall removes only this exact browser-specific manifest, never history. */
export function uninstallNativeHost(options) {
  if (!nativeHostRuntimeSupported()) throw new Error("native_host_environment_unsupported");
  const path = manifestPath(options);
  if (existsSync(path)) rmSync(path);
  return path;
}
