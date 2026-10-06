#!/usr/bin/env node
'use strict';
// mcp-remove.js — canonical MCP config remover for the shell teardown path.
//
// Shares the fail-closed contract of mcp-merge.js (see its spec comment):
// refuse (no write, non-zero exit) when the file is unparseable, parses to
// null or a non-object, or when mcpServers is present but not an object.
// Unknown top-level keys and other servers are always preserved. When the
// nexus-ollama entry is the last server and no other top-level keys exist,
// the file itself is removed (it was created by the installer).
// Prints one of: removed | removed-file | unchanged.
//
// Usage: node mcp-remove.js <config-file>
// Exit: 0 on success, 1 on refusal/error, 2 on usage error.

const fs = require('fs');

const SERVER_NAME = 'nexus-ollama';

function fail(configPath, msg) {
  console.error('ERROR: refusing to modify ' + configPath + ': ' + msg);
  process.exit(1);
}

function main() {
  const args = process.argv.slice(2);
  if (args.length !== 1 || !args[0]) {
    console.error('usage: mcp-remove.js <config-file>');
    process.exit(2);
  }
  const configPath = args[0];

  if (!fs.existsSync(configPath)) {
    console.log('unchanged');
    return;
  }
  const raw = fs.readFileSync(configPath, 'utf8').trim();
  if (raw.length === 0) {
    console.log('unchanged');
    return;
  }

  let config;
  try {
    config = JSON.parse(raw);
  } catch (err) {
    fail(configPath, 'unparseable JSON: ' + err.message);
  }
  if (config === null || typeof config !== 'object' || Array.isArray(config)) {
    fail(configPath, 'top-level value is not an object');
  }

  if (!Object.prototype.hasOwnProperty.call(config, 'mcpServers')) {
    console.log('unchanged');
    return;
  }
  const servers = config.mcpServers;
  if (servers === null || typeof servers !== 'object' || Array.isArray(servers)) {
    fail(configPath, 'mcpServers is present but not an object');
  }
  if (!Object.prototype.hasOwnProperty.call(servers, SERVER_NAME)) {
    console.log('unchanged');
    return;
  }
  delete servers[SERVER_NAME];

  const remainingServers = Object.keys(servers).length;
  const otherKeys = Object.keys(config).filter(function (k) { return k !== 'mcpServers'; }).length;
  if (remainingServers === 0 && otherKeys === 0) {
    fs.unlinkSync(configPath);
    console.log('removed-file');
    return;
  }
  const content = JSON.stringify(config, null, 2) + '\n';
  try {
    fs.writeFileSync(configPath, content);
  } catch (err) {
    fs.chmodSync(configPath, 0o644);
    fs.writeFileSync(configPath, content);
  }
  console.log('removed');
}

main();
