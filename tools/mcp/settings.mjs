import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

export function parseSettings(text) {
  const values = Object.create(null);
  for (const line of text.split('\n')) {
    const match = /^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*=[ \t]*(.*?)[ \t\r]*$/.exec(line);
    if (!match) continue;
    let value = match[2];
    if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) value = value.slice(1, -1);
    values[match[1]] = value;
  }
  return values;
}
export function loadSettings(env = process.env) {
  const path = env.NEXUS_REPO ? resolve(env.NEXUS_REPO, '.env') : fileURLToPath(new URL('../../.env', import.meta.url));
  let values = {};
  try { values = parseSettings(readFileSync(path, 'utf8')); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  return { ...values, ...env };
}

// Guard every Ollama transport call, including health checks.
export function ollamaFetch(settings, ...args) {
  if (settings.NEXUS_LOCAL_AI === 'false') throw new Error('LOCAL_AI_DISABLED: Enable NEXUS_LOCAL_AI to use Ollama delegation.');
  return fetch(...args);
}
