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

const OLLAMA_HOST_URL = process.env.OLLAMA_HOST_URL || "http://localhost:11434";
const CONNECT_TIMEOUT_MS = 5000;
const REQUEST_TIMEOUT_MS = 120000;

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

async function callOllama(model, prompt) {
  const res = await fetch(`${OLLAMA_HOST_URL}/api/generate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      model,
      prompt,
      stream: false,
      options: { temperature: 0.1 },
    }),
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`Ollama returned HTTP ${res.status}: ${body}`);
  }

  const data = await res.json();
  const output = data.response;

  if (!output) {
    throw new Error("Ollama returned empty response");
  }

  // Strip accidental markdown fences (same as ollama-delegate.sh)
  return output.replace(/^```[\w]*\n?|```$/gm, "").trim();
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
    const model = MODEL_ROUTES["commit-msg"];
    const prompt = PROMPTS["commit-msg"](diff);
    try {
      const result = await callOllama(model, prompt);
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
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
      const result = await callOllama(model, prompt);
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
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
      const result = await callOllama(model, prompt);
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
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
      const result = await callOllama(model, prompt);
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
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
      const result = await callOllama(model, prompt);
      return {
        content: [
          { type: "text", text: result },
        ],
      };
    } catch (e) {
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
