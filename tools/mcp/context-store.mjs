/**
 * NEXUS context store — file I/O behind the nexus-context MCP server.
 *
 * This module is deliberately MCP-free so it can be unit-tested without
 * spawning a server. It mirrors the Go memory store in tools/tui/memory_store.go
 * exactly: same directory convention (~/.config/nexus/agent-memory/<project>/),
 * same Markdown + YAML-frontmatter entry format, same YYYYMMDD-HHMMSS-slug.md
 * filenames, same 0700 dirs / 0600 files. A decision logged here is readable
 * by `nexus memory show`, and vice versa.
 *
 * Personas are read from the installed directory (~/.config/nexus/personas,
 * a symlink to the repo personas/ created by `nexus install`) with a fallback
 * to the repo-bundled personas/ next to this server. Persona files are the
 * real Markdown files with YAML frontmatter (name, description, ...); nothing
 * here invents persona content.
 *
 * Path safety: project names are single path components (same rule as the Go
 * store), so memories can never escape their project directory. Persona
 * lookup never turns user input into a path — it lists the directory and
 * matches filename stems / frontmatter names.
 */

import {
  closeSync,
  existsSync,
  mkdirSync,
  openSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir } from "node:os";

// ── Resolution ──────────────────────────────────────────────────────────────
// Env overrides exist for tests and for installs that keep NEXUS data outside
// $HOME; defaults follow the install layout.

export function resolveMemoryRoot(env = process.env) {
  if (env.NEXUS_MEMORY_ROOT) return resolve(env.NEXUS_MEMORY_ROOT);
  return join(homedir(), ".config", "nexus", "agent-memory");
}

export function resolvePersonasDir(env = process.env) {
  if (env.NEXUS_PERSONAS_DIR) return resolve(env.NEXUS_PERSONAS_DIR);
  const installed = join(homedir(), ".config", "nexus", "personas");
  if (existsSync(installed)) return installed;
  // Repo-bundled fallback: <repo>/personas, relative to tools/mcp/.
  return resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "personas");
}

/** Default project: explicit arg, else NEXUS_PROJECT, else the cwd dirname. */
export function defaultProject(env = process.env) {
  if (env.NEXUS_PROJECT) return env.NEXUS_PROJECT;
  return basename(resolve(process.cwd()));
}

// ── Memory entries (mirrors tools/tui/memory_store.go) ─────────────────────

export function validMemoryComponent(name) {
  return (
    typeof name === "string" &&
    name !== "" &&
    name !== "." &&
    name !== ".." &&
    !name.includes("/") &&
    !name.includes("\\") &&
    !name.includes("\x00") &&
    !name.includes("\r") &&
    !name.includes("\n")
  );
}

function projectDir(root, project) {
  if (!validMemoryComponent(project)) {
    throw new Error(`invalid project name ${JSON.stringify(project)}`);
  }
  return join(root, project);
}

/** Parse a memory file the same way Go parseMemory does. */
export function parseMemoryFile(filename, content) {
  const entry = {
    filename,
    title: "",
    date: "",
    tags: [],
    preview: "",
    body: content,
    content,
  };
  const normalized = content.replace(/\r\n/g, "\n");
  if (normalized.startsWith("---\n")) {
    const lines = normalized.split("\n");
    for (let i = 1; i < lines.length; i++) {
      if (lines[i] === "---" || lines[i] === "...") {
        const meta = parseFrontmatter(lines.slice(1, i).join("\n"));
        if (meta.title) entry.title = meta.title;
        if (meta.date) entry.date = meta.date;
        if (meta.tags) entry.tags = meta.tags;
        entry.body = lines.slice(i + 1).join("\n");
        break;
      }
    }
  }
  for (const line of entry.body.split("\n")) {
    const trimmed = line.trim();
    if (trimmed !== "" && entry.preview === "") entry.preview = trimmed;
    if (entry.title === "" && trimmed.startsWith("# ")) {
      entry.title = trimmed.slice(2).trim();
    }
  }
  if (entry.title === "") entry.title = filename;
  return entry;
}

/**
 * Minimal frontmatter reader for the scalar fields the Go store writes
 * (title, date, tags list). Only `key: value` lines and `  - item` list
 * entries are understood; anything else is ignored rather than guessed at.
 */
export function parseFrontmatter(text) {
  const meta = {};
  let listKey = null;
  for (const rawLine of text.split("\n")) {
    const line = rawLine.trimEnd();
    const listMatch = line.match(/^\s+-\s+(.*)$/);
    if (listMatch && listKey) {
      if (!Array.isArray(meta[listKey])) meta[listKey] = [];
      meta[listKey].push(unquote(listMatch[1].trim()));
      continue;
    }
    const match = line.match(/^([A-Za-z][A-Za-z0-9_-]*):\s*(.*)$/);
    if (!match) {
      listKey = null;
      continue;
    }
    const value = match[2].trim();
    if (value === "") {
      listKey = match[1];
      meta[listKey] = [];
    } else {
      listKey = null;
      meta[match[1]] = unquote(value);
    }
  }
  return meta;
}

 /** Frontmatter of a persona/memory file, parsed; {} when absent. */
export function readFrontmatter(content) {
  if (!content.startsWith("---\n")) return {};
  const lines = content.split("\n");
  const end = lines.findIndex((l, i) => i > 0 && l === "---");
  if (end === -1) return {};
  return parseFrontmatter(lines.slice(1, end).join("\n"));
}

function unquote(value) {
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    try {
      return JSON.parse(value);
    } catch {
      return value.slice(1, -1);
    }
  }
  return value;
}

function isRegularFile(path) {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
}

function readMemoryEntry(dir, filename) {
  const content = readFileSync(join(dir, filename), "utf8");
  const entry = parseMemoryFile(filename, content);
  entry.modified = statSync(join(dir, filename)).mtime;
  return entry;
}

/** Projects present under the memory root, with entry counts. */
export function listProjects(root) {
  if (!existsSync(root)) return [];
  const result = [];
  for (const dirent of readdirSync(root, { withFileTypes: true })) {
    if (!dirent.isDirectory()) continue;
    const entries = listMemories(root, dirent.name);
    result.push({
      name: dirent.name,
      count: entries.length,
      modified: entries.reduce(
        (latest, e) => (e.modified > latest ? e.modified : latest),
        new Date(0),
      ),
    });
  }
  return result.sort((a, b) => a.name.localeCompare(b.name));
}

/** All .md memories of a project, newest first. Missing dir → empty list. */
export function listMemories(root, project) {
  const dir = projectDir(root, project);
  if (!existsSync(dir)) return [];
  const result = [];
  for (const dirent of readdirSync(dir, { withFileTypes: true })) {
    if (!dirent.isFile() || !dirent.name.endsWith(".md")) continue;
    result.push(readMemoryEntry(dir, dirent.name));
  }
  return result.sort((a, b) => b.modified - a.modified);
}

export function memorySlug(title) {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 80)
    .replace(/-+$/, "");
  return slug === "" ? "memory" : slug;
}

function pad(n) {
  return String(n).padStart(2, "0");
}

/** Local-time stamp matching Go's "20060102-150405" layout. */
export function formatStamp(date) {
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

/** Local-time RFC 3339, matching Go's time.Format(time.RFC3339). */
export function formatRfc3339(date) {
  const offset = -date.getTimezoneOffset();
  const sign = offset >= 0 ? "+" : "-";
  const abs = Math.abs(offset);
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}

function yamlScalar(value) {
  // Plain when it cannot be misread; JSON double-quoting is valid YAML.
  return /^[A-Za-z0-9 _'.,!?()/-]+$/.test(value) ? value : JSON.stringify(value);
}

/**
 * Write a decision entry, byte-compatible in shape with Go saveMemory:
 * YAML frontmatter (title, date, tags: [decision]) + Markdown body,
 * filename YYYYMMDD-HHMMSS-<slug>.md, O_EXCL collision loop stepping one
 * second at a time. Returns the written path.
 */
export function saveDecision(root, project, what, why, now = new Date()) {
  if (typeof what !== "string" || what.trim() === "") {
    throw new Error("decision title (what) is required");
  }
  if (typeof why !== "string" || why.trim() === "") {
    throw new Error("decision reason (why) is required");
  }
  const dir = projectDir(root, project);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const title = what.trim();
  const body = `${title}\n\n## Why\n\n${why.trim()}\n`;
  const frontmatter =
    "---\n" +
    `title: ${yamlScalar(title)}\n` +
    `date: ${yamlScalar(formatRfc3339(now))}\n` +
    "tags:\n    - decision\n" +
    "---\n\n";
  const content = frontmatter + body;
  for (let i = 0; i < 3600; i++) {
    const stamp = formatStamp(new Date(now.getTime() + i * 1000));
    const path = join(dir, `${stamp}-${memorySlug(title)}.md`);
    let fd;
    try {
      fd = openSync(path, "wx", 0o600);
    } catch (error) {
      if (error.code === "EEXIST") continue;
      throw error;
    }
    try {
      writeFileSync(fd, content, "utf8");
    } finally {
      closeSync(fd);
    }
    return path;
  }
  throw new Error("too many filename collisions");
}


// ── Context reads ───────────────────────────────────────────────────────────

export const MAX_CONTEXT_ENTRIES = 10;
export const MAX_ENTRY_CHARS = 4000;
export const MAX_TOTAL_CHARS = 20000;

/**
 * Relevant agent-memory for a project: newest entries first, bounded so a
 * single call cannot flood the caller's context window. A missing project
 * is not an error — it means no memory has been recorded yet.
 */
export function getContext(root, project) {
  const entries = listMemories(root, project).slice(0, MAX_CONTEXT_ENTRIES);
  let total = 0;
  const clipped = [];
  for (const entry of entries) {
    let body = entry.body;
    let truncated = false;
    if (body.length > MAX_ENTRY_CHARS) {
      body = body.slice(0, MAX_ENTRY_CHARS);
      truncated = true;
    }
    total += body.length;
    if (total > MAX_TOTAL_CHARS) break;
    clipped.push({ ...entry, body, truncated });
  }
  return {
    project,
    exists: existsSync(projectDir(root, project)),
    entries: clipped,
    projects: listProjects(root).map((p) => p.name),
  };
}

// ── Personas ────────────────────────────────────────────────────────────────

function personaSlug(name) {
  return name.toLowerCase().replace(/[\s_]+/g, "-").replace(/[^a-z0-9-]/g, "");
}

/** Available personas: name + description from frontmatter, sorted by name. */
export function listPersonas(dir) {
  if (!existsSync(dir)) return [];
  const result = [];
  for (const dirent of readdirSync(dir, { withFileTypes: true })) {
    if (!dirent.isFile() || !dirent.name.endsWith(".md")) continue;
    const content = readFileSync(join(dir, dirent.name), "utf8");
    const meta = readFrontmatter(content);
    result.push({
      id: dirent.name.replace(/\.md$/, ""),
      name: meta.name || dirent.name.replace(/\.md$/, ""),
      description: meta.description || "",
      color: meta.color || "",
      emoji: meta.emoji || "",
      vibe: meta.vibe || "",
    });
  }
  return result.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Full instructions for a named persona. Matches the filename stem
 * (e.g. "engineering-code-reviewer") or the frontmatter name
 * (e.g. "Code Reviewer"), case-insensitively. Throws when unknown.
 */
export function getPersona(dir, name) {
  if (typeof name !== "string" || name.trim() === "") {
    throw new Error("persona name is required");
  }
  if (!existsSync(dir)) {
    throw new Error(`no personas installed (looked in ${dir})`);
  }
  const wanted = personaSlug(name.trim());
  for (const dirent of readdirSync(dir, { withFileTypes: true })) {
    if (!dirent.isFile() || !dirent.name.endsWith(".md")) continue;
    const content = readFileSync(join(dir, dirent.name), "utf8");
    const meta = readFrontmatter(content);
    const stem = dirent.name.replace(/\.md$/, "");
    if (personaSlug(stem) === wanted || (meta.name && personaSlug(meta.name) === wanted)) {
      return {
        id: stem,
        name: meta.name || stem,
        description: meta.description || "",
        color: meta.color || "",
        emoji: meta.emoji || "",
        vibe: meta.vibe || "",
        content,
      };
    }
  }
  const available = listPersonas(dir).map((p) => p.id);
  throw new Error(
    `unknown persona ${JSON.stringify(name)}; available: ${available.join(", ") || "(none)"}`,
  );
}

// ── Session summary ─────────────────────────────────────────────────────────
// There is no live session transcript store on main, so this is an honest
// roll-up of what is actually stored: project totals, the newest decisions
// and entries, and the persona count. It never invents session activity.

export function getSessionSummary(root, personasDir, project) {
  const projects = listProjects(root);
  const scoped = project ? listMemories(root, project) : projects.flatMap((p) => listMemories(root, p.name));
  const byModified = [...scoped].sort((a, b) => b.modified - a.modified);
  const decisions = byModified.filter((e) => e.tags.includes("decision")).slice(0, 5);
  return {
    project: project || null,
    generatedAt: new Date().toISOString(),
    projects: projects.map((p) => ({ name: p.name, count: p.count })),
    totalEntries: projects.reduce((n, p) => n + p.count, 0),
    recentDecisions: decisions.map((e) => ({
      title: e.title,
      date: e.date,
      filename: e.filename,
    })),
    recentEntries: byModified.slice(0, 5).map((e) => ({
      title: e.title,
      date: e.date,
      filename: e.filename,
      preview: e.preview,
    })),
    personaCount: existsSync(personasDir) ? listPersonas(personasDir).length : 0,
  };
}
