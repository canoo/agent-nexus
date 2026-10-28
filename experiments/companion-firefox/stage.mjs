#!/usr/bin/env node
import { chmodSync, copyFileSync, lstatSync, mkdirSync, realpathSync, writeFileSync } from "node:fs";
import { endianness } from "node:os";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const SOURCE = fileURLToPath(new URL("./", import.meta.url));
const REPO = fileURLToPath(new URL("../../", import.meta.url));
const HOST = "com.codelogiic.nexus.firefox_probe";
const ADDON_ID = "nexus-firefox-probe@codelogiic.local";
function quote(value) { return `'${value.replaceAll("'", "'\\''")}'`; }
function copy(source, target) {
  const stat = lstatSync(source);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error("stage_source_invalid");
  copyFileSync(source, target);
}
export function stageFirefoxProbe(output) {
  if (process.platform !== "linux" || endianness() !== "LE" || typeof output !== "string" || !isAbsolute(output)) throw new Error("stage_input_invalid");
  const destination = join(realpathSync(dirname(output)), resolve(output).split("/").at(-1));
  const fromRepo = relative(realpathSync(REPO), destination);
  if (!fromRepo || (!fromRepo.startsWith("../") && !isAbsolute(fromRepo))) throw new Error("stage_input_invalid");
  // Exclusive output root; all registration examples live under this new root.
  mkdirSync(destination, { mode: 0o700 });
  mkdirSync(join(destination, "extension"));
  mkdirSync(join(destination, "host"));
  for (const name of ["manifest.json", "background.js"]) copy(join(SOURCE, "extension", name), join(destination, "extension", name));
  copy(join(SOURCE, "fixture-host.mjs"), join(destination, "host/fixture-host.mjs"));
  copy(join(REPO, "apps/companion-native-host/lib/native-messaging.mjs"), join(destination, "host/native-messaging.mjs"));
  const launcher = join(destination, "host/nexus-firefox-probe");
  writeFileSync(launcher, `#!/bin/sh\nexec ${quote(realpathSync(process.execPath))} ${quote(join(destination, "host/fixture-host.mjs"))} "$@"\n`, { flag: "wx", mode: 0o700 });
  chmodSync(launcher, 0o700);
  const directory = join(destination, "isolated-home/.mozilla/native-messaging-hosts");
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  const manifestPath = join(directory, `${HOST}.json`);
  writeFileSync(manifestPath, `${JSON.stringify({ name: HOST, description: "NEXUS Firefox transport fixture only", path: launcher, type: "stdio", allowed_extensions: [ADDON_ID] }, null, 2)}\n`, { flag: "wx", mode: 0o600 });
  return { extension: join(destination, "extension"), manifestPath, launcher, isolatedHome: join(destination, "isolated-home") };
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv.length !== 4 || process.argv[2] !== "--output") throw new Error("stage_input_invalid");
    process.stdout.write(`${JSON.stringify(stageFirefoxProbe(process.argv[3]))}\n`);
  } catch {
    process.stderr.write("Firefox experiment staging failed; use a new absolute destination outside the repository.\n");
    process.exitCode = 1;
  }
}
