#!/usr/bin/env node
import { createRuntimeLaunchers, launcherErrorCode } from "../lib/runtime-launchers.mjs";
try {
  const args = process.argv.slice(2);
  const options = {};
  if (args.length !== 8) throw new Error("launcher_input_invalid");
  for (let index = 0; index < args.length; index += 2) {
    const key = { "--runtime-root": "runtimeRoot", "--output": "outputDir", "--node-path": "nodePath", "--desktop-path": "desktopPath" }[args[index]];
    if (!key || Object.hasOwn(options, key)) throw new Error("launcher_input_invalid");
    options[key] = args[index + 1];
  }
  process.stdout.write(`${JSON.stringify(createRuntimeLaunchers(options))}\n`);
} catch (error) {
  const code = launcherErrorCode(error);
  process.stderr.write(`${JSON.stringify({ schemaVersion: 1, ok: false, error: code })}\n`);
  process.exitCode = code === "launcher_input_invalid" || code === "launcher_output_exists" ? 2 : 1;
}
