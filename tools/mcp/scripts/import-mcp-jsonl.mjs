#!/usr/bin/env node
/**
 * Import a preserved MCP JSONL snapshot into the local observability database.
 *
 * The importer never writes the source. Re-running a preserved snapshot or an
 * append-only source is idempotent. For a truncated or rotated log, keep a
 * copy and import it under a distinct path: an external rewrite is not claimed
 * to be exactly-once for historically indistinguishable rows.
 */
import { createObservabilityStore } from "../lib/observability-store.mjs";

function usage() {
  return `Usage: node scripts/import-mcp-jsonl.mjs [--input <local-jsonl-path>] [--database <local-sqlite-path>]

Imports a local MCP JSONL snapshot without changing it. Defaults:
  input:    ~/.config/nexus/logs/mcp-tasks.jsonl
  database: ~/.config/nexus/logs/observability.sqlite

The same preserved snapshot and normal append-only growth are safe to import
again. For a truncated or rotated log, import a saved copy under a distinct
path. An external rewrite cannot distinguish historically identical rows, so
the importer does not claim exactly-once semantics for that case.`;
}

function parseArguments(argumentsList) {
  const options = {};
  for (let index = 0; index < argumentsList.length; index += 1) {
    const argument = argumentsList[index];
    if (argument === "--help" || argument === "-h") return { help: true };
    if (argument !== "--input" && argument !== "--database") {
      throw new Error(`Unknown option: ${argument}`);
    }
    const value = argumentsList[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`${argument} requires a path`);
    options[argument === "--input" ? "inputPath" : "databasePath"] = value;
    index += 1;
  }
  return options;
}

try {
  const options = parseArguments(process.argv.slice(2));
  if (options.help) {
    console.log(usage());
  } else {
    const result = createObservabilityStore({ databasePath: options.databasePath }).importLegacyMcpJsonl(options);
    console.log(JSON.stringify(result));
  }
} catch (error) {
  console.error(`NEXUS MCP JSONL import failed: ${error instanceof Error ? error.message : String(error)}`);
  console.error(usage());
  process.exitCode = 1;
}
