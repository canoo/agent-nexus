#!/usr/bin/env node
'use strict';
// mcp-merge.js — canonical MCP config writer for the shell install path.
//
// CANONICAL MCP MERGE SPEC (all five writers conform: this script x2 call
// sites in setup-nexus.sh, mcp-remove.js x2 call sites in teardown-nexus.sh,
// and Go mergeMCPConfig/configureMCP in tools/tui/main.go):
//   - Input: a JSON object config file with an optional "mcpServers" object.
//   - Preserve: unknown top-level keys and all other servers are left
//     untouched. When the nexus-ollama entry already exists, only the managed
//     fields (command, args) are updated; per-entry extras (env, cwd, ...)
//     are preserved.
//   - Fail closed: refuse (no write, non-zero exit) when the file is
//     unparseable, parses to null or a non-object, when mcpServers is present
//     but not an object, or when the existing nexus-ollama entry is present
//     but not an object. Never coerce null into {}.
//   - Missing or blank file: create a fresh config.
//   - Idempotent: when nothing changes, the file is not rewritten.
//   - Prints one of: added | updated | unchanged.
//
// Usage: node mcp-merge.js <config-file> <server-path>
// Exit: 0 on success, 1 on refusal/error, 2 on usage error.

const fs = require('fs');
const path = require('path');

const SERVER_NAME = 'nexus-ollama';

function fail(configPath, msg) {
  console.error('ERROR: refusing to modify ' + configPath + ': ' + msg);
  process.exit(1);
}

function writeConfig(configPath, config) {
  const content = JSON.stringify(config, null, 2) + '\n';
  try {
    fs.writeFileSync(configPath, content);
  } catch (err) {
    // Retry once after normalizing permissions (read-only file, etc.).
    fs.chmodSync(configPath, 0o644);
    fs.writeFileSync(configPath, content);
  }
}

function main() {
  const args = process.argv.slice(2);
  if (args.length !== 2 || !args[0] || !args[1]) {
    console.error('usage: mcp-merge.js <config-file> <server-path>');
    process.exit(2);
  }
  const configPath = args[0];
  const serverPath = args[1];

  let config;
  if (fs.existsSync(configPath)) {
    const raw = fs.readFileSync(configPath, 'utf8').trim();
    if (raw.length === 0) {
      config = {};
    } else {
      let parsed;
      try {
        parsed = JSON.parse(raw);
      } catch (err) {
        fail(configPath, 'unparseable JSON: ' + err.message);
      }
      if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
        fail(configPath, 'top-level value is not an object');
      }
      config = parsed;
    }
  } else {
    config = {};
  }

  if (Object.prototype.hasOwnProperty.call(config, 'mcpServers')) {
    const ms = config.mcpServers;
    if (ms === null || typeof ms !== 'object' || Array.isArray(ms)) {
      fail(configPath, 'mcpServers is present but not an object');
    }
  } else {
    config.mcpServers = {};
  }
  const servers = config.mcpServers;

  let status;
  if (!Object.prototype.hasOwnProperty.call(servers, SERVER_NAME)) {
    servers[SERVER_NAME] = { command: 'node', args: [serverPath] };
    status = 'added';
  } else {
    const existing = servers[SERVER_NAME];
    if (existing === null || typeof existing !== 'object' || Array.isArray(existing)) {
      fail(configPath, 'existing "' + SERVER_NAME + '" entry is not an object');
    }
    const sameCommand = existing.command === 'node';
    const sameArgs = Array.isArray(existing.args) &&
      existing.args.length === 1 && existing.args[0] === serverPath;
    if (sameCommand && sameArgs) {
      status = 'unchanged';
    } else {
      // Merge: managed fields (command, args) updated, per-entry extras
      // (env, cwd, ...) preserved.
      servers[SERVER_NAME] = Object.assign({}, existing, {
        command: 'node',
        args: [serverPath],
      });
      status = 'updated';
    }
  }

  if (status !== 'unchanged') {
    fs.mkdirSync(path.dirname(configPath), { recursive: true });
    writeConfig(configPath, config);
  }
  console.log(status);
}

main();
