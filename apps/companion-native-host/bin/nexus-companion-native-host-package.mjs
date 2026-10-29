#!/usr/bin/env node
import { packageErrorCode, stageNativeHostPackage } from "../lib/package.mjs";

function parseArguments(args) {
  const options = {};
  if (args.length !== 4) throw new Error("package_input_invalid");
  for (let index = 0; index < args.length; index += 2) {
    const key = { "--output": "outputDir", "--version": "version" }[args[index]];
    if (!key || Object.hasOwn(options, key) || !args[index + 1]) throw new Error("package_input_invalid");
    options[key] = args[index + 1];
  }
  return options;
}

try {
  const manifest = stageNativeHostPackage(parseArguments(process.argv.slice(2)));
  process.stdout.write(`${JSON.stringify({ ok: true, formatVersion: manifest.formatVersion, version: manifest.version, fileCount: manifest.files.length })}\n`);
} catch (error) {
  const code = packageErrorCode(error);
  process.stderr.write(`${JSON.stringify({ ok: false, error: code })}\n`);
  process.exitCode = code === "package_input_invalid" || code === "package_output_exists" ? 2 : 1;
}
