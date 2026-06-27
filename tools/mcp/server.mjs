#!/usr/bin/env node
/**
 * NEXUS Ollama MCP Server
 *
 * Exposes local Ollama model delegation as MCP tools so that Claude Code,
 * Gemini CLI, and Kiro CLI can route micro-tasks to local models automatically.
 *
 * Mirrors the task types and model routing from ollama-delegate.sh.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { appendFileSync, mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const OLLAMA_HOST_URL = process.env.OLLAMA_HOST_URL || "http://localhost:11434";
const CONNECT_TIMEOUT_MS = 5000;
const REQUEST_TIMEOUT_MS = 120000;

// ── Task log ────────────────────────────────────────────────────────────────
// Writes JSONL entries to ~/.config/nexus/logs/mcp-tasks.jsonl for TUI display.
const LOG_DIR = join(homedir(), ".config", "nexus", "logs");
const LOG_FILE = join(LOG_DIR, "mcp-tasks.jsonl");
try { mkdirSync(LOG_DIR, { recursive: true }); } catch {}

function logTask(entry) {
  try {
    appendFileSync(LOG_FILE, JSON.stringify(entry) + "\n");
  } catch {}
}

// ── Model routing table ─────────────────────────────────────────────────────
// Matches ollama-delegate.sh: supervisor band (1.5B) for fast tasks,
// logic band (3B) for heavier reasoning.
//
// Override any route via environment variables:
//   NEXUS_SUPERVISOR_MODEL  → replaces all supervisor band routes
//   NEXUS_LOGIC_MODEL       → replaces all logic band routes
//   NEXUS_MODEL_<TASK>      → replaces a single task route (e.g. NEXUS_MODEL_COMMIT_MSG)
//
// See docs/model-configuration.md for hardware-specific presets.
const DEFAULT_SUPERVISOR = "qwen2.5-coder:1.5b";
const DEFAULT_LOGIC = "llama3.2:3b";

const supervisorModel = process.env.NEXUS_SUPERVISOR_MODEL || DEFAULT_SUPERVISOR;
const logicModel = process.env.NEXUS_LOGIC_MODEL || DEFAULT_LOGIC;

const MODEL_ROUTES = {
  "commit-msg": process.env.NEXUS_MODEL_COMMIT_MSG || supervisorModel,
  boilerplate: process.env.NEXUS_MODEL_BOILERPLATE || supervisorModel,
  "test-scaffold": process.env.NEXUS_MODEL_TEST_SCAFFOLD || supervisorModel,
  "lint-fix": process.env.NEXUS_MODEL_LINT_FIX || logicModel,
  "logic-refactor": process.env.NEXUS_MODEL_LOGIC_REFACTOR || logicModel,
};

// ── Prompt templates ────────────────────────────────────────────────────────
// Identical to ollama-delegate.sh so behavior is consistent whether called
// via MCP or via the shell script directly.
const PROMPTS = {
  "commit-msg": (context) =>
    `You are a git commit message writer. Given the following git diff, write a single conventional commit message.

Rules:
- Format: <type>(<optional-scope>): <short description>
- Types: feat, fix, refactor, chore, docs, style, test, perf
- Under 72 characters total
- Lowercase
- No period at the end
- Return ONLY the commit message — no explanation, no alternatives

Git diff:
${context}`,

  boilerplate: (context) =>
    `You are a code boilerplate generator. Generate a clean, minimal boilerplate file based on the specification below.

Rules:
- Return ONLY the file content — no explanation, no markdown fences
- Follow the framework and patterns shown in the example/context
- No placeholder comments like "// TODO: implement"
- Production-quality structure, empty function bodies are fine

Specification:
${context}`,

  "test-scaffold": (context) =>
    `You are a test scaffolding generator. Given the source file below, generate a test file scaffold.

Rules:
- Return ONLY the test file content — no explanation, no markdown fences
- Include describe blocks and test names that reflect real behavior
- Leave test bodies empty (no implementation) — just the structure
- Use the framework implied by the file extension and imports (jest/vitest/pytest/etc.)
- Import the module under test correctly

Source file:
${context}`,

  "lint-fix": (context) =>
    `You are a lint error fixer. Given the file content and lint errors below, return the corrected file.

Rules:
- Return ONLY the corrected file content — no explanation, no markdown fences
- Fix ONLY the reported lint errors — do not refactor or reformat unrelated code
- Preserve all logic and behavior exactly

File and lint errors:
${context}`,

  "logic-refactor": (context) =>
    `You are a code refactoring assistant. Refactor the code below to improve clarity and maintainability.

Rules:
- Return ONLY the refactored code — no explanation, no markdown fences
- Preserve all existing behavior exactly
- Do not change function signatures or public interfaces
- Do not add new features or error handling beyond what exists

Code to refactor:
${context}`,
};

// ── Ollama HTTP client ──────────────────────────────────────────────────────

async function checkOllama() {
  try {
    const res = await fetch(`${OLLAMA_HOST_URL}/api/tags`, {
      signal: AbortSignal.timeout(CONNECT_TIMEOUT_MS),
    });
    if (!res.ok) {
      return { ok: false, error: `Ollama returned HTTP ${res.status}` };
    }
    const data = await res.json();
    const models = (data.models || []).map((m) => m.name);
    return { ok: true, models };
  } catch (e) {
    return {
      ok: false,
      error: `Ollama unreachable at ${OLLAMA_HOST_URL}: ${e.message}`,
    };
  }
}

// JSON schema for structured output tasks. Ollama's native format parameter
// guarantees schema-valid responses without prompt engineering.
const STRUCTURED_SCHEMAS = {
  "commit-msg": {
    type: "object",
    properties: { message: { type: "string" } },
    required: ["message"],
  },
};

async function callOllama(model, prompt, task) {
  const body = {
    model,
    prompt,
    stream: false,
    options: { temperature: 0.1 },
  };

  // Use native structured output for tasks with a defined schema
  if (task && STRUCTURED_SCHEMAS[task]) {
    body.format = STRUCTURED_SCHEMAS[task];
  }

  const res = await fetch(`${OLLAMA_HOST_URL}/api/generate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });

  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`Ollama returned HTTP ${res.status}: ${text}`);
  }

  const data = await res.json();
  const output = data.response;

  if (!output) {
    throw new Error("Ollama returned empty response");
  }

  // For structured output, parse JSON and extract the value
  if (task && STRUCTURED_SCHEMAS[task]) {
    try {
      const parsed = JSON.parse(output);
      return parsed.message || output;
    } catch {
      // Fallback: model returned plain text despite format param
      return output.replace(/^```[\w]*\n?|```$/gm, "").trim();
    }
  }

  // Strip accidental markdown fences for free-form code output
  return output.replace(/^```[\w]*\n?|```$/gm, "").trim();
}

// ── Fast-path pre-checks ────────────────────────────────────────────────────
// Bypass LLM inference for diffs that match deterministic patterns.
// Returns a commit message string if matched, null otherwise.

function fastPathCommitMsg(diff) {
  const lines = diff.split("\n");
  const fileChanges = lines.filter((l) => l.startsWith("diff --git"));

  // Single-file rename with no content change
  if (fileChanges.length === 1) {
    const rename = lines.find((l) => l.startsWith("rename from "));
    const renameTo = lines.find((l) => l.startsWith("rename to "));
    if (rename && renameTo) {
      const from = rename.replace("rename from ", "");
      const to = renameTo.replace("rename to ", "");
      return `chore: rename ${from} to ${to}`;
    }
  }

  // Version bump in package.json (only "version" field changed)
  if (fileChanges.length === 1 && fileChanges[0].includes("package.json")) {
    const added = lines.filter((l) => l.startsWith("+") && !l.startsWith("+++"));
    const removed = lines.filter((l) => l.startsWith("-") && !l.startsWith("---"));
    if (
      added.length === 1 &&
      removed.length === 1 &&
      removed[0].includes('"version"') &&
      added[0].includes('"version"')
    ) {
      const ver = added[0].match(/"version":\s*"([^"]+)"/);
      if (ver) return `chore: bump version to ${ver[1]}`;
    }
  }

  // Lock file only (package-lock.json, yarn.lock, pnpm-lock.yaml, go.sum)
  const lockPatterns = ["package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum"];
  if (
    fileChanges.length > 0 &&
    fileChanges.every((f) => lockPatterns.some((p) => f.includes(p)))
  ) {
    return "chore: update lock file";
  }

  // Deleted files only
  if (fileChanges.length > 0) {
    const deletions = lines.filter((l) => l.startsWith("deleted file mode"));
    if (deletions.length === fileChanges.length) {
      const count = deletions.length;
      return count === 1
        ? `chore: remove ${fileChanges[0].match(/b\/(.+)/)?.[1] || "file"}`
        : `chore: remove ${count} files`;
    }
  }

  return null;
}

// ── MCP Server ──────────────────────────────────────────────────────────────

const server = new McpServer({
  name: "nexus-ollama",
  version: "1.0.0",
});

// Health check tool — lets the AI verify the local compute plane is up.
server.tool(
  "ollama_health",
  "Check if the local Ollama instance is reachable and list available models",
  {},
  async () => {
    const status = await checkOllama();
    if (!status.ok) {
      return {
        content: [
          {
            type: "text",
            text: `CIRCUIT_BREAKER: ${status.error}\n\nOllama is not available. Tasks cannot be delegated to the local compute plane.`,
          },
        ],
      };
    }
    return {
      content: [
        {
          type: "text",
          text: `Ollama is running at ${OLLAMA_HOST_URL}\n\nAvailable models:\n${status.models.map((m) => `  - ${m}`).join("\n")}`,
        },
      ],
    };
  }
);

// Commit message generator — supervisor band (1.5B)
server.tool(
  "ollama_commit_msg",
  "Generate a conventional commit message from a git diff using a local 1.5B model. Use this instead of writing commit messages with cloud models.",
  { diff: z.string().max(500000).describe("The git diff to summarize") },
  async ({ diff }) => {
    // Fast-path: deterministic commit messages for trivial diffs
    const fastResult = fastPathCommitMsg(diff);
    if (fastResult) {
      logTask({ tool: "ollama_commit_msg", model: "fast-path", ms: 0, ok: true, ts: Date.now() });
      return { content: [{ type: "text", text: fastResult }] };
    }
    const model = MODEL_ROUTES["commit-msg"];
    const prompt = PROMPTS["commit-msg"](diff);
    try {
      const start = Date.now();
      const result = await callOllama(model, prompt, "commit-msg");
      logTask({ tool: "ollama_commit_msg", model, ms: Date.now() - start, ok: true, ts: Date.now() });
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
      logTask({ tool: "ollama_commit_msg", model, ms: 0, ok: false, error: e.message, ts: Date.now() });
      return {
        content: [
          { type: "text", text: `CIRCUIT_BREAKER: ${e.message}` },
        ],
        isError: true,
      };
    }
  }
);

// Boilerplate generator — supervisor band (1.5B)
server.tool(
  "ollama_boilerplate",
  "Generate a clean boilerplate file from a specification using a local 1.5B model. Use for component/route/model scaffolding.",
  { specification: z.string().max(500000).describe("Description of the boilerplate to generate, including framework and patterns to follow") },
  async ({ specification }) => {
    const model = MODEL_ROUTES["boilerplate"];
    const prompt = PROMPTS["boilerplate"](specification);
    try {
      const start = Date.now();
      const result = await callOllama(model, prompt, "boilerplate");
      logTask({ tool: "ollama_boilerplate", model, ms: Date.now() - start, ok: true, ts: Date.now() });
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
      logTask({ tool: "ollama_boilerplate", model, ms: 0, ok: false, error: e.message, ts: Date.now() });
      return {
        content: [
          { type: "text", text: `CIRCUIT_BREAKER: ${e.message}` },
        ],
        isError: true,
      };
    }
  }
);

// Test scaffold generator — supervisor band (1.5B)
server.tool(
  "ollama_test_scaffold",
  "Generate a test file scaffold (describe blocks, test names, no implementations) from a source file using a local 1.5B model.",
  { source_code: z.string().max(500000).describe("The source file content to generate tests for") },
  async ({ source_code }) => {
    const model = MODEL_ROUTES["test-scaffold"];
    const prompt = PROMPTS["test-scaffold"](source_code);
    try {
      const start = Date.now();
      const result = await callOllama(model, prompt, "test-scaffold");
      logTask({ tool: "ollama_test_scaffold", model, ms: Date.now() - start, ok: true, ts: Date.now() });
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
      logTask({ tool: "ollama_test_scaffold", model, ms: 0, ok: false, error: e.message, ts: Date.now() });
      return {
        content: [
          { type: "text", text: `CIRCUIT_BREAKER: ${e.message}` },
        ],
        isError: true,
      };
    }
  }
);

// Lint fixer — logic band (3B)
server.tool(
  "ollama_lint_fix",
  "Fix lint errors in a file using a local 3B model. Preserves all logic and behavior — only fixes reported errors.",
  { file_and_errors: z.string().max(500000).describe("The file content followed by the lint errors to fix") },
  async ({ file_and_errors }) => {
    const model = MODEL_ROUTES["lint-fix"];
    const prompt = PROMPTS["lint-fix"](file_and_errors);
    try {
      const start = Date.now();
      const result = await callOllama(model, prompt, "lint-fix");
      logTask({ tool: "ollama_lint_fix", model, ms: Date.now() - start, ok: true, ts: Date.now() });
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
      logTask({ tool: "ollama_lint_fix", model, ms: 0, ok: false, error: e.message, ts: Date.now() });
      return {
        content: [
          { type: "text", text: `CIRCUIT_BREAKER: ${e.message}` },
        ],
        isError: true,
      };
    }
  }
);

// Logic refactor — logic band (3B)
server.tool(
  "ollama_logic_refactor",
  "Refactor a code block for clarity and maintainability using a local 3B model. Preserves behavior and public interfaces.",
  { code: z.string().max(500000).describe("The code to refactor") },
  async ({ code }) => {
    const model = MODEL_ROUTES["logic-refactor"];
    const prompt = PROMPTS["logic-refactor"](code);
    try {
      const start = Date.now();
      const result = await callOllama(model, prompt, "logic-refactor");
      logTask({ tool: "ollama_logic_refactor", model, ms: Date.now() - start, ok: true, ts: Date.now() });
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
      logTask({ tool: "ollama_logic_refactor", model, ms: 0, ok: false, error: e.message, ts: Date.now() });
      return {
        content: [
          { type: "text", text: `CIRCUIT_BREAKER: ${e.message}` },
        ],
        isError: true,
      };
    }
  }
);

// ── Start ───────────────────────────────────────────────────────────────────
const transport = new StdioServerTransport();
await server.connect(transport);
