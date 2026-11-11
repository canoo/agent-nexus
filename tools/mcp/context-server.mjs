#!/usr/bin/env node
/**
 * NEXUS Context MCP Server (issue #84)
 *
 * Exposes agent memory and personas as MCP tools so any tool with MCP
 * support (Claude Code, Cursor, Windsurf, Cline, Continue.dev) gets
 * real-time, bidirectional project context — no steering-file dependency,
 * no file polling.
 *
 * Runs locally alongside nexus-ollama (tools/mcp/server.mjs) over stdio.
 * All reads/writes go through ./context-store.mjs, which mirrors the Go
 * memory store (tools/tui/memory_store.go): same directory convention
 * (~/.config/nexus/agent-memory/<project>/), same Markdown +
 * YAML-frontmatter format, so `nexus memory show` and this server read
 * each other's entries.
 *
 * Tools:
 *   nexus_get_context(project?)       → newest agent-memory for a project
 *   nexus_log_decision(what, why)     → append a decision entry to memory
 *   nexus_get_persona(name)           → full instructions for a persona
 *   nexus_list_personas()             → available personas + descriptions
 *   nexus_get_session_summary()       → roll-up of stored context
 *
 * Env overrides (same pattern as settings.mjs):
 *   NEXUS_MEMORY_ROOT   → agent-memory root (default ~/.config/nexus/agent-memory)
 *   NEXUS_PERSONAS_DIR  → personas dir (default ~/.config/nexus/personas,
 *                          else the repo-bundled personas/)
 *   NEXUS_PROJECT       → default project when a tool omits it
 *
 * NOT in scope here: `nexus sync` auto-adding this server to tool MCP
 * configs is gated on #81 (milestone #8, not done). Until then, add the
 * entry manually:
 *   { "mcpServers": { "nexus-context": {
 *       "command": "node",
 *       "args": ["~/.config/nexus/tools/mcp/context-server.mjs"] } } }
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import {
  defaultProject,
  getContext,
  getPersona,
  getSessionSummary,
  listPersonas,
  resolveMemoryRoot,
  resolvePersonasDir,
  saveDecision,
} from "./context-store.mjs";

const MEMORY_ROOT = resolveMemoryRoot();
const PERSONAS_DIR = resolvePersonasDir();

function textResult(text) {
  return { content: [{ type: "text", text }] };
}

function errorResult(message) {
  return { content: [{ type: "text", text: message }], isError: true };
}

function resolveProject(project) {
  return project && project.trim() !== "" ? project.trim() : defaultProject();
}

function formatEntry(entry) {
  const lines = [
    `## ${entry.title}`,
    `File: ${entry.filename}`,
    entry.date ? `Date: ${entry.date}` : null,
    entry.tags.length > 0 ? `Tags: ${entry.tags.join(", ")}` : null,
    "",
    entry.body,
    entry.truncated ? "\n… (entry truncated to 4000 characters)" : null,
  ];
  return lines.filter((l) => l !== null).join("\n");
}

const server = new McpServer({
  name: "nexus-context",
  version: "1.0.0",
});

server.tool(
  "nexus_get_context",
  "Return relevant agent-memory for a project: the newest entries (title, date, tags, body), newest first. Defaults to NEXUS_PROJECT or the current directory name. A project with no memory yet returns an empty list plus the projects that do have memory — that is normal, not an error.",
  { project: z.string().max(200).optional().describe("Project name, as in ~/.config/nexus/agent-memory/<project>/") },
  async ({ project }) => {
    try {
      const name = resolveProject(project);
      const context = getContext(MEMORY_ROOT, name);
      if (!context.exists) {
        return textResult(
          `No agent-memory recorded yet for project "${name}".\n\n` +
            (context.projects.length > 0
              ? `Projects with memory: ${context.projects.join(", ")}`
              : "No projects have memory yet — log a decision with nexus_log_decision to start one."),
        );
      }
      if (context.entries.length === 0) {
        return textResult(`Project "${name}" has a memory directory but no entries yet.`);
      }
      return textResult(
        `# Agent memory — ${name}\n\n${context.entries.map(formatEntry).join("\n\n---\n\n")}`,
      );
    } catch (error) {
      return errorResult(`nexus_get_context failed: ${error.message}`);
    }
  },
);

server.tool(
  "nexus_log_decision",
  "Write a decision entry back to agent-memory so it is immediately visible to every other tool and to `nexus memory show`. Writes Markdown with YAML frontmatter (title, date, tags: [decision]) to ~/.config/nexus/agent-memory/<project>/, filename YYYYMMDD-HHMMSS-<slug>.md.",
  {
    what: z.string().min(1).max(200).describe("The decision, in one line (becomes the entry title)"),
    why: z.string().min(1).max(10000).describe("Why this decision was made — context a future session needs"),
    project: z.string().max(200).optional().describe("Project name; defaults to NEXUS_PROJECT or the current directory name"),
  },
  async ({ what, why, project }) => {
    try {
      const name = resolveProject(project);
      const path = saveDecision(MEMORY_ROOT, name, what, why);
      return textResult(
        `Decision logged for project "${name}".\n\nFile: ${path}\nTitle: ${what.trim()}`,
      );
    } catch (error) {
      return errorResult(`nexus_log_decision failed: ${error.message}`);
    }
  },
);

server.tool(
  "nexus_get_persona",
  "Return the full instructions for a named persona (the persona file's complete Markdown). Match by filename (e.g. engineering-code-reviewer) or display name (e.g. Code Reviewer). Personas come from the installed ~/.config/nexus/personas/ directory, falling back to the repo-bundled personas/.",
  { name: z.string().min(1).max(200).describe("Persona filename stem or display name") },
  async ({ name }) => {
    try {
      const persona = getPersona(PERSONAS_DIR, name);
      return textResult(persona.content);
    } catch (error) {
      return errorResult(`nexus_get_persona failed: ${error.message}`);
    }
  },
);

server.tool(
  "nexus_list_personas",
  "List the available specialist personas with their descriptions. Use nexus_get_persona to read one in full.",
  {},
  async () => {
    try {
      const personas = listPersonas(PERSONAS_DIR);
      if (personas.length === 0) {
        return textResult(`No personas found in ${PERSONAS_DIR}.`);
      }
      const lines = personas.map(
        (p) => `- ${p.id} — ${p.name}${p.emoji ? ` ${p.emoji}` : ""}: ${p.description || "(no description)"}`,
      );
      return textResult(`Available personas (${personas.length}):\n\n${lines.join("\n")}`);
    } catch (error) {
      return errorResult(`nexus_list_personas failed: ${error.message}`);
    }
  },
);

server.tool(
  "nexus_get_session_summary",
  "Return a summary of the current session context: project totals, the newest decisions and entries, and the persona count. This is a roll-up of stored agent-memory — there is no live session transcript store, so nothing here is invented.",
  { project: z.string().max(200).optional().describe("Project to scope the summary to; defaults to NEXUS_PROJECT or the current directory name") },
  async ({ project }) => {
    try {
      const name = resolveProject(project);
      const summary = getSessionSummary(MEMORY_ROOT, PERSONAS_DIR, name);
      const lines = [
        `# Session summary — ${summary.project}`,
        "",
        `Generated: ${summary.generatedAt}`,
        `Projects with memory: ${summary.projects.length} (${summary.totalEntries} entries total)`,
        ...summary.projects.map((p) => `  - ${p.name}: ${p.count} entries`),
        `Personas available: ${summary.personaCount}`,
        "",
        "## Recent decisions",
        ...(summary.recentDecisions.length > 0
          ? summary.recentDecisions.map((d) => `- ${d.title}${d.date ? ` (${d.date})` : ""} — ${d.filename}`)
          : ["(none recorded)"]),
        "",
        "## Recent entries",
        ...(summary.recentEntries.length > 0
          ? summary.recentEntries.map((e) => `- ${e.title}${e.date ? ` (${e.date})` : ""} — ${e.filename}`)
          : ["(none recorded)"]),
        "",
        `Memory root: ${MEMORY_ROOT}`,
        `Personas dir: ${PERSONAS_DIR}`,
      ];
      return textResult(lines.join("\n"));
    } catch (error) {
      return errorResult(`nexus_get_session_summary failed: ${error.message}`);
    }
  },
);

const transport = new StdioServerTransport();
await server.connect(transport);
