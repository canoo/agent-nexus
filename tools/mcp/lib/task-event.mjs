/**
 * Pure task-event builders for the MCP observability boundary.
 *
 * Extracted from server.mjs so the metadata computations are unit-testable
 * without starting the MCP server. Everything here is metadata only: sizes,
 * hashes, token estimates, and cost estimates. Raw prompt/response content
 * must never leave the caller — only the derived measurements below.
 */
import { createHash } from "node:crypto";

// Early cost tracking uses a conservative cloud-equivalent estimate. Local
// Ollama tasks cost $0 here; this value answers "what would this have cost if
// routed to a typical cloud coding model?" until provider-specific pricing lands.
export const CLOUD_INPUT_USD_PER_1M = 3.0;
export const CLOUD_OUTPUT_USD_PER_1M = 15.0;

export function estimateTokens(text) {
  if (!text) return 0;
  return Math.ceil(text.length / 4);
}

export function estimateCloudCost(tokensIn, tokensOut) {
  return (tokensIn / 1_000_000) * CLOUD_INPUT_USD_PER_1M +
    (tokensOut / 1_000_000) * CLOUD_OUTPUT_USD_PER_1M;
}

function sha256Hex(text) {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

/**
 * Builds a store-ready MCP task event from a completed tool call.
 * prompt/response are measured (bytes, token estimate, input hash) but never
 * included: the observability store accepts metadata only.
 */
export function taskLogEntry({ tool, model, ms, ok, prompt = "", response = "", error }) {
  const tokensIn = estimateTokens(prompt);
  const tokensOut = estimateTokens(response);
  const routing = model === "fast-path" ? "deterministic" : "local";
  const entry = {
    tool,
    model,
    routing,
    tokens_in: tokensIn,
    tokens_out: tokensOut,
    cloud_cost_equivalent: estimateCloudCost(tokensIn, tokensOut),
    input_bytes: Buffer.byteLength(prompt, "utf8"),
    output_bytes: Buffer.byteLength(response, "utf8"),
    input_hash: sha256Hex(prompt),
    ms,
    ok,
    ts: Date.now(),
  };
  if (error) entry.error = error;
  return entry;
}
