/**
 * Agent-memory decision logging (issue #87).
 *
 * Node-side mirror of the Go memory store in tools/tui/memory_store.go.
 * The on-disk format is the contract both sides share, so a decision
 * written by the MCP server is readable by `nexus memory decisions`
 * and vice versa:
 *
 *   ~/.config/nexus/agent-memory/<project>/<YYYYMMDD-HHMMSS>-<slug>.md
 *
 *   ---
 *   title: JWT over sessions
 *   date: 2026-11-10T19:00:00.000Z
 *   tags:
 *       - decision
 *   ---
 *
 *   <why>
 *
 *   Ruled out: <alternatives>.
 *
 * No new dependencies: frontmatter here is only ever written and read in
 * this shape, so parsing is a small line scanner, not a YAML parser.
 */

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

export function memoryRoot() {
  return path.join(os.homedir(), ".config", "nexus", "agent-memory");
}

// Names are single path components; memories never escape their project directory.
export function validMemoryComponent(name) {
  return (
    typeof name === "string" &&
    name !== "" &&
    name !== "." &&
    name !== ".." &&
    !/[/\\\x00\r\n]/.test(name) &&
    !path.isAbsolute(name)
  );
}

function projectDir(root, project) {
  if (!validMemoryComponent(project)) {
    throw new Error(`invalid project name ${JSON.stringify(project)}`);
  }
  const dir = path.join(root, project);
  let info;
  try {
    info = fs.lstatSync(dir);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    return dir;
  }
  if (info.isSymbolicLink() || !info.isDirectory()) {
    throw new Error(`project is not a regular directory: ${project}`);
  }
  return dir;
}

// Mirrors Go memorySlug: lowercase letter/digit runs joined by single
// dashes, capped at 80 characters, "memory" when nothing survives.
export function slugify(title) {
  const lowered = String(title).toLowerCase();
  let out = "";
  let separator = false;
  for (const ch of lowered) {
    if (/[\p{L}\p{N}]/u.test(ch)) {
      if (separator && out.length > 0) out += "-";
      out += ch;
      separator = false;
    } else {
      separator = true;
    }
  }
  if (out.length === 0) return "memory";
  if ([...out].length > 80) {
    out = [...out].slice(0, 80).join("").replace(/-+$/, "");
  }
  return out;
}

function stamp(date) {
  const p = (n) => String(n).padStart(2, "0");
  return (
    `${date.getFullYear()}${p(date.getMonth() + 1)}${p(date.getDate())}` +
    `-${p(date.getHours())}${p(date.getMinutes())}${p(date.getSeconds())}`
  );
}

// Double-quoted YAML scalar; JSON quoting is valid YAML and round-trips
// through Go's yaml parser, including titles with colons or quotes.
function yamlQuote(value) {
  return JSON.stringify(value);
}

function parseScalar(raw) {
  const value = raw.trim();
  if (value.startsWith('"')) {
    try {
      return JSON.parse(value);
    } catch {
      return value;
    }
  }
  if (value.startsWith("'") && value.endsWith("'")) {
    return value.slice(1, -1).replace(/''/g, "'");
  }
  return value;
}

export function parseMemory(filename, content) {
  const entry = { filename, title: "", date: "", tags: [], body: content, content };
  const normalized = content.replace(/\r\n/g, "\n");
  let body = normalized;
  if (normalized.startsWith("---\n")) {
    const lines = normalized.split("\n");
    for (let i = 1; i < lines.length; i++) {
      if (lines[i] === "---" || lines[i] === "...") {
        let inTags = false;
        for (const line of lines.slice(1, i)) {
          if (/^\s*-\s+/.test(line) && inTags) {
            entry.tags.push(parseScalar(line.replace(/^\s*-\s+/, "")));
            continue;
          }
          inTags = false;
          const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
          if (!match) continue;
          if (match[1] === "title") entry.title = parseScalar(match[2]);
          if (match[1] === "date") entry.date = parseScalar(match[2]);
          if (match[1] === "tags") inTags = match[2].trim() === "";
        }
        body = lines.slice(i + 1).join("\n");
        break;
      }
    }
  }
  entry.body = body;
  let preview = "";
  for (const line of body.split("\n")) {
    const trimmed = line.trim();
    if (trimmed !== "" && preview === "") preview = trimmed;
    if (entry.title === "" && trimmed.startsWith("# ")) {
      entry.title = trimmed.slice(2).trim();
    }
  }
  entry.preview = preview;
  if (entry.title === "") entry.title = filename;
  return entry;
}

/**
 * Write one architectural decision to agent-memory.
 * Returns the path of the file written. `ruledOut` names rejected
 * alternatives; empty entries are dropped.
 */
export function saveDecision({ root = memoryRoot(), project, what, why, ruledOut = [], now = new Date() } = {}) {
  if (!what || String(what).trim() === "") {
    throw new Error("decision title (what) is required");
  }
  if (!why || String(why).trim() === "") {
    throw new Error("decision rationale (why) is required");
  }
  const dir = projectDir(root, project);
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  let body = String(why).trim();
  const ruled = ruledOut.map((r) => String(r).trim()).filter((r) => r !== "");
  if (ruled.length > 0) body += `\n\nRuled out: ${ruled.join("; ")}.`;
  const frontmatter =
    `---\ntitle: ${yamlQuote(String(what).trim())}\n` +
    `date: ${now.toISOString()}\ntags:\n    - decision\n---\n\n`;
  let content = frontmatter + body;
  if (!content.endsWith("\n")) content += "\n";
  // Preserve the prescribed filename format without overwriting a same-second save.
  for (let i = 0; i < 3600; i++) {
    const when = new Date(now.getTime() + i * 1000);
    const file = path.join(dir, `${stamp(when)}-${slugify(what)}.md`);
    try {
      const fd = fs.openSync(file, "wx", 0o600);
      try {
        fs.writeFileSync(fd, content);
      } finally {
        fs.closeSync(fd);
      }
      return file;
    } catch (error) {
      if (error.code === "EEXIST") continue;
      throw error;
    }
  }
  throw new Error("too many filename collisions");
}

export function isDecision(entry) {
  return entry.tags.includes("decision");
}

// Decisions newest first. limit <= 0 (or omitted) means all.
export function listDecisions({ root = memoryRoot(), project, limit = 0 } = {}) {
  const dir = projectDir(root, project);
  const files = fs.readdirSync(dir);
  const decisions = [];
  for (const name of files) {
    if (!name.endsWith(".md")) continue;
    const file = path.join(dir, name);
    const info = fs.lstatSync(file);
    if (info.isSymbolicLink() || !info.isFile()) continue;
    const entry = parseMemory(name, fs.readFileSync(file, "utf8"));
    entry.modified = info.mtime;
    if (isDecision(entry)) decisions.push(entry);
  }
  decisions.sort((a, b) => {
    const byTime = b.modified.getTime() - a.modified.getTime();
    if (byTime !== 0) return byTime;
    return b.filename < a.filename ? -1 : b.filename > a.filename ? 1 : 0;
  });
  if (limit > 0) return decisions.slice(0, limit);
  return decisions;
}

// Renders decisions in the issue #87 read-back format:
//
//   ## 2026-04-28 — JWT over sessions
//   Chose JWT for auth because the API needs to be stateless for horizontal scaling.
//   Ruled out: cookie sessions (stateful), OAuth (too heavy for internal API).
export function formatDecisions(entries) {
  return entries
    .map((entry) => {
      const date = entry.date ? String(entry.date).slice(0, 10) : "";
      return `## ${date} — ${entry.title}\n${String(entry.body).trim()}\n`;
    })
    .join("\n");
}

// Project for MCP decision calls: explicit NEXUS_MEMORY_PROJECT wins,
// otherwise the directory the server was launched from (the project dir
// in normal MCP client setups).
export function defaultProject() {
  if (process.env.NEXUS_MEMORY_PROJECT) return process.env.NEXUS_MEMORY_PROJECT;
  return path.basename(process.cwd());
}
