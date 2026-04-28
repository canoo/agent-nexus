# Cloud API Billing Guide

How to manage costs when using NEXUS with cloud AI APIs, and how local delegation saves money.

> Last verified: April 2026. Check the official pricing links below for current rates.

## The Billing Landscape

Most AI CLI tools now require pay-as-you-go API billing. Personal subscriptions no longer cover third-party harness usage. Every task your AI tools perform has a direct cost.

NEXUS helps by routing high-volume, low-complexity tasks to local models — removing them from your cloud bill entirely.

## Official Pricing References

Always check the source for current rates — pricing pages may move or update without notice:

- **Anthropic (Claude)**: https://anthropic.com/pricing
- **Google (Gemini)**: https://ai.google.dev/pricing
- **OpenAI**: https://openai.com/api/pricing

## The Volume Math

You don't need exact prices to understand the savings. Think in terms of tasks per day:

| Task Type | Typical Volume/Day | Tokens per Task | Routed By NEXUS |
|-----------|-------------------|----------------|-----------------|
| Commit messages | 15–30 | ~500 | ✓ Local (free) |
| Boilerplate scaffolds | 3–5 | ~800 | ✓ Local (free) |
| Test scaffolds | 3–5 | ~1,200 | ✓ Local (free) |
| Lint fixes | 5–10 | ~1,800 | ✓ Local (free) |
| Simple refactors | 2–5 | ~2,500 | ✓ Local (free) |
| **Micro-task subtotal** | **28–55 tasks** | **~40K–80K tokens** | **$0** |
| Architecture, planning, complex code | 5–15 | varies | Cloud (billed) |

Multiply your daily micro-task token volume by your provider's per-token rate to see what you're saving. For a team of 5, the savings compound quickly.

The cost savings are real, but the latency improvement matters more in practice — micro-tasks complete in 2–5 seconds locally with no rate limiting.

## When to Use Cloud vs Local

| Use Cloud | Use Local (NEXUS MCP) |
|-----------|----------------------|
| Architecture decisions | Commit messages |
| Complex multi-file refactors | Single-file lint fixes |
| Code review with full context | Boilerplate generation |
| Planning and design | Test scaffolding |
| Debugging complex issues | Simple refactors |

Rule of thumb: if the task has a predictable output format and doesn't require understanding a large codebase, route it locally.

## Setting Up Cost-Efficient Routing

### 1. Enable local delegation

Configure the NEXUS MCP server in your AI CLI tool:

```json
{
  "mcpServers": {
    "nexus-ollama": {
      "command": "node",
      "args": ["~/.config/nexus/tools/mcp/server.mjs"]
    }
  }
}
```

### 2. Verify routing

Ask your AI tool to generate a commit message — it should use `ollama_commit_msg` instead of the cloud model. If it doesn't, check that the orchestrator config is symlinked (`nexus` → Health Check).

### 3. Right-size your models

Use the TUI Health Check (`nexus` → Health Check) to see your detected hardware and recommended models. See [model-configuration.md](model-configuration.md) for hardware-specific presets.

## Monitoring Your Spend

**Today:** Check your provider dashboards directly using the pricing links above.

**NEXUS v0.2.0 (planned):** Session logging will track which model handled each task, response time, and estimated cost. The TUI dashboard will show cloud vs local routing splits.

**NEXUS v0.2.1 (planned):** [Tokscale](https://github.com/junhoyeo/tokscale) integration will aggregate usage across all your AI CLI tools into a single view alongside NEXUS routing savings.

## FAQ

**Does local delegation affect output quality?**
For micro-tasks (commit messages, scaffolds), quality is equivalent. For complex reasoning, cloud models are significantly better — that's why NEXUS routes those to cloud. See the [MCP test results](model-configuration.md#mcp-test-results-rtx-3050-mobile-default-config) for measured quality assessments.

**What if Ollama is down?**
The MCP server returns `CIRCUIT_BREAKER` and your AI CLI falls back to the cloud model automatically.

**Can I force everything through cloud?**
Set `NEXUS_LOCAL_AI=false` in `.env` or toggle it off in the TUI Configure screen.

**What about rate limits?**
Local models have no rate limits. By offloading micro-tasks locally, you reduce cloud API call volume during intensive sessions.
