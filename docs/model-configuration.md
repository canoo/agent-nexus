# Model Configuration Guide

How to choose and configure local models for the nexus-ollama MCP server based on your hardware.

## How Model Routing Works

The MCP server routes tasks to two model bands:

| Band | Default Model | Tasks |
|------|--------------|-------|
| **Supervisor** | `qwen2.5-coder:1.5b` | commit-msg, boilerplate, test-scaffold |
| **Logic** | `llama3.2:3b` | lint-fix, logic-refactor |

These defaults target the lowest common denominator (4GB VRAM). If you have better hardware, you should upgrade to larger models for significantly better output quality.

## Overriding Models

Models are configured via environment variables — either in your `.env` file or in the MCP server config.

### Option 1: `.env` file

```bash
# .env (in repo root)
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="qwen2.5:14b"
```

### Option 2: MCP server config (per-CLI)

```json
{
  "mcpServers": {
    "nexus-ollama": {
      "command": "node",
      "args": ["~/.config/nexus/tools/mcp/server.mjs"],
      "env": {
        "NEXUS_SUPERVISOR_MODEL": "qwen2.5-coder:7b",
        "NEXUS_LOGIC_MODEL": "qwen2.5:14b"
      }
    }
  }
}
```

### Option 3: Per-task override

Override a single task without changing the whole band:

```bash
NEXUS_MODEL_COMMIT_MSG="qwen2.5-coder:3b"
NEXUS_MODEL_LOGIC_REFACTOR="qwen2.5:7b"
```

### Override Priority

```
Per-task env var  →  Band-level env var  →  Built-in default
```

## Hardware Profiles

### RTX 3050 Mobile / 4GB VRAM (Default)

The current default configuration. Fits entirely in 4GB VRAM with no spillover.

```bash
# No overrides needed — these are the defaults
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:1.5b"
NEXUS_LOGIC_MODEL="llama3.2:3b"
```

| Band | Model | VRAM | Speed |
|------|-------|------|-------|
| Supervisor | qwen2.5-coder:1.5b | ~1.2 GB | ~120 t/s |
| Logic | llama3.2:3b | ~2.0 GB | ~73 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:1.5b
ollama pull llama3.2:3b
```

---

### RTX 3060 / RTX 4060 / 8GB VRAM

The 8GB sweet spot. Run 7-8B models fully in VRAM at 40-60 t/s.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="llama3.1:8b"
```

| Band | Model | VRAM | Speed |
|------|-------|------|-------|
| Supervisor | qwen2.5-coder:7b | ~4.7 GB | ~45 t/s |
| Logic | llama3.1:8b | ~4.9 GB | ~40 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:7b
ollama pull llama3.1:8b
```

---

### MacBook Air / Pro M3 (Base) — 8GB Unified Memory

Apple Silicon shares memory between CPU and GPU. With 8GB total (minus OS overhead), stick to smaller models.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:3b"
NEXUS_LOGIC_MODEL="llama3.2:3b"
```

| Band | Model | Memory | Speed |
|------|-------|--------|-------|
| Supervisor | qwen2.5-coder:3b | ~1.9 GB | ~50 t/s |
| Logic | llama3.2:3b | ~2.0 GB | ~45 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:3b
ollama pull llama3.2:3b
```

---

### MacBook Pro M3 Pro — 18GB Unified Memory

With 18GB unified memory (~14GB usable for models), you can comfortably run 7-8B models.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="llama3.1:8b"
```

| Band | Model | Memory | Speed |
|------|-------|--------|-------|
| Supervisor | qwen2.5-coder:7b | ~4.7 GB | ~30 t/s |
| Logic | llama3.1:8b | ~4.9 GB | ~25 t/s |

> Apple Silicon has lower memory bandwidth (~150 GB/s on M3 Pro) than discrete GPUs, so expect ~40-60% of the t/s you'd see on an equivalent NVIDIA card. The tradeoff is massive unified memory capacity.

**Pull commands:**
```bash
ollama pull qwen2.5-coder:7b
ollama pull llama3.1:8b
```

---

### MacBook Pro M3 Pro — 36GB Unified Memory

36GB opens up 14B models comfortably, with room for the OS and other apps.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="qwen2.5:14b"
```

| Band | Model | Memory | Speed |
|------|-------|--------|-------|
| Supervisor | qwen2.5-coder:7b | ~4.7 GB | ~30 t/s |
| Logic | qwen2.5:14b | ~9.0 GB | ~20 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:7b
ollama pull qwen2.5:14b
```

---

### RTX 4060 Ti 16GB / RTX A4000 16GB

16GB VRAM is the inflection point for 14B models running fully on GPU.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="qwen2.5:14b"
```

| Band | Model | VRAM | Speed |
|------|-------|------|-------|
| Supervisor | qwen2.5-coder:7b | ~4.7 GB | ~55 t/s |
| Logic | qwen2.5:14b | ~9.0 GB | ~35 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:7b
ollama pull qwen2.5:14b
```

---

### RTX 3090 / RTX 4090 — 24GB VRAM

24GB is the enthusiast sweet spot. Run 14B models with headroom, or push into 32B with tight context.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:14b"
NEXUS_LOGIC_MODEL="qwen2.5:32b"
```

| Band | Model | VRAM | Speed (4090) | Speed (3090) |
|------|-------|------|-------------|-------------|
| Supervisor | qwen2.5-coder:14b | ~9.0 GB | ~70 t/s | ~50 t/s |
| Logic | qwen2.5:32b | ~19.8 GB | ~30 t/s | ~20 t/s |

> The 32B model at Q4_K_M needs ~22GB with 8k context. This fits on 24GB cards but leaves minimal headroom. If you need longer contexts, drop to 14B for the logic band.

**Alternative (safer, more headroom):**
```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:7b"
NEXUS_LOGIC_MODEL="qwen2.5:14b"
```

**Pull commands:**
```bash
ollama pull qwen2.5-coder:14b
ollama pull qwen2.5:32b
```

---

### MacBook Pro M3 Max — 36-48GB Unified Memory

M3 Max has 400 GB/s memory bandwidth — significantly better than M3 Pro. 32B models run well.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:14b"
NEXUS_LOGIC_MODEL="qwen2.5:32b"
```

| Band | Model | Memory | Speed |
|------|-------|--------|-------|
| Supervisor | qwen2.5-coder:14b | ~9.0 GB | ~35 t/s |
| Logic | qwen2.5:32b | ~19.8 GB | ~18 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:14b
ollama pull qwen2.5:32b
```

---

### MacBook Pro M3 Max — 96GB Unified Memory

96GB unified memory is workstation territory. You can run 70B models, though slowly.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:32b"
NEXUS_LOGIC_MODEL="llama3.3:70b"
```

| Band | Model | Memory | Speed |
|------|-------|--------|-------|
| Supervisor | qwen2.5-coder:32b | ~19.8 GB | ~20 t/s |
| Logic | llama3.3:70b | ~42.5 GB | ~8 t/s |

> 70B models at ~8 t/s are usable for batch tasks but too slow for interactive commit messages. Consider keeping the supervisor band at 14B for speed.

**Balanced alternative:**
```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:14b"
NEXUS_LOGIC_MODEL="qwen2.5:32b"
```

**Pull commands:**
```bash
ollama pull qwen2.5-coder:32b
ollama pull llama3.3:70b
```

---

### RTX 5090 — 32GB VRAM

The RTX 5090 has 1.79 TB/s memory bandwidth (78% more than the 4090) and 32GB GDDR7. This is the fastest consumer card for local LLM inference.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:14b"
NEXUS_LOGIC_MODEL="qwen2.5:32b"
```

| Band | Model | VRAM | Speed |
|------|-------|------|-------|
| Supervisor | qwen2.5-coder:14b | ~9.0 GB | ~100 t/s |
| Logic | qwen2.5:32b | ~19.8 GB | ~61 t/s |

> At these speeds, the logic band is faster than most setups' supervisor band. You could realistically use 32B for everything.

**Aggressive (all 32B):**
```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:32b"
NEXUS_LOGIC_MODEL="qwen2.5:32b"
```

**Pull commands:**
```bash
ollama pull qwen2.5-coder:14b
ollama pull qwen2.5:32b
```

---

### Dual GPU — 2× RTX 3090 / 4090 (48GB total)

Ollama supports multi-GPU via `CUDA_VISIBLE_DEVICES`. With 48GB combined VRAM, 70B models fit.

```bash
NEXUS_SUPERVISOR_MODEL="qwen2.5-coder:32b"
NEXUS_LOGIC_MODEL="llama3.3:70b"
```

| Band | Model | VRAM | Speed |
|------|-------|------|-------|
| Supervisor | qwen2.5-coder:32b | ~19.8 GB | ~40-60 t/s |
| Logic | llama3.3:70b | ~42.5 GB | ~15-25 t/s |

**Pull commands:**
```bash
ollama pull qwen2.5-coder:32b
ollama pull llama3.3:70b
```

---

## Quick Reference Table

| Hardware | VRAM / Memory | Supervisor Model | Logic Model | Expected Speed |
|----------|--------------|-----------------|-------------|---------------|
| RTX 3050 Mobile | 4 GB | qwen2.5-coder:1.5b | llama3.2:3b | 70–120 t/s |
| M3 Base | 8 GB unified | qwen2.5-coder:3b | llama3.2:3b | 45–50 t/s |
| RTX 3060 / 4060 | 8 GB | qwen2.5-coder:7b | llama3.1:8b | 40–60 t/s |
| M3 Pro 18GB | 18 GB unified | qwen2.5-coder:7b | llama3.1:8b | 25–30 t/s |
| M3 Pro 36GB | 36 GB unified | qwen2.5-coder:7b | qwen2.5:14b | 20–30 t/s |
| RTX 4060 Ti 16GB | 16 GB | qwen2.5-coder:7b | qwen2.5:14b | 35–55 t/s |
| RTX 3090 | 24 GB | qwen2.5-coder:14b | qwen2.5:32b | 20–50 t/s |
| RTX 4090 | 24 GB | qwen2.5-coder:14b | qwen2.5:32b | 30–70 t/s |
| M3 Max 48GB | 48 GB unified | qwen2.5-coder:14b | qwen2.5:32b | 18–35 t/s |
| M3 Max 96GB | 96 GB unified | qwen2.5-coder:32b | llama3.3:70b | 8–20 t/s |
| RTX 5090 | 32 GB | qwen2.5-coder:14b | qwen2.5:32b | 61–100 t/s |
| 2× RTX 3090/4090 | 48 GB | qwen2.5-coder:32b | llama3.3:70b | 15–60 t/s |

## VRAM Rules of Thumb

- **Q4_K_M VRAM** ≈ `parameters (B) × 0.57` + 0.5 GB overhead + KV cache
- **KV cache at 8k context** adds ~1-2 GB for 7-8B models, ~3-4 GB for 32B models
- **If a model spills to RAM**, expect 5-20× slower inference. Always fit the model fully in VRAM.
- **Apple Silicon** shares memory between OS and GPU — budget ~4 GB less than total for model headroom

## MCP Test Results (RTX 3050 Mobile, Default Config)

Tested 2026-04-13 via JSON-RPC stdio against the MCP server with default model routing.

| Tool | Model | Band | Time | Output Quality |
|------|-------|------|------|---------------|
| `ollama_health` | — | — | instant | ✅ Lists all models |
| `ollama_commit_msg` | qwen2.5-coder:1.5b | supervisor | ~4.5s | ✅ Clean conventional commit |
| `ollama_boilerplate` | qwen2.5-coder:1.5b | supervisor | ~7s | ✅ Reasonable Express+Zod scaffold |
| `ollama_test_scaffold` | qwen2.5-coder:1.5b | supervisor | ~3s | ✅ Correct describe/it blocks |
| `ollama_lint_fix` | llama3.2:3b | logic | ~6.5s | ⚠️ Fixed most errors, minor garble on edge case |
| `ollama_logic_refactor` | llama3.2:3b | logic | ~3s | ✅ Excellent — nested loops → filter+map |

All tools returned valid responses with no CIRCUIT_BREAKER errors. The 1.5B supervisor model handles structured generation tasks well. The 3B logic model produces good refactors but occasionally garbles complex multi-fix lint corrections — upgrading to 7B+ eliminates this.

## Choosing the Right Model

### Supervisor Band (speed-critical)

Commit messages, boilerplate, and test scaffolds are **structured generation** — the model follows a rigid format. Smaller models handle this well because the output pattern is predictable. Prioritize speed over size.

| Priority | Recommendation |
|----------|---------------|
| Speed-first | qwen2.5-coder:1.5b (>100 t/s on most hardware) |
| Balanced | qwen2.5-coder:3b or qwen2.5-coder:7b |
| Quality-first | qwen2.5-coder:14b (if VRAM allows) |

### Logic Band (quality-critical)

Lint fixes and refactors require **understanding code semantics** — the model needs to reason about what the code does. Larger models produce meaningfully better results here.

| Priority | Recommendation |
|----------|---------------|
| Minimum viable | llama3.2:3b (works, occasional errors) |
| Recommended | llama3.1:8b or qwen2.5:7b |
| Best quality | qwen2.5:14b or qwen2.5:32b |

## Verifying Your Setup

After changing models, verify everything works:

```bash
# 1. Pull the new models
ollama pull <supervisor-model>
ollama pull <logic-model>

# 2. Test the MCP server health
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ollama_health","arguments":{}}}' | \
  NEXUS_SUPERVISOR_MODEL="<your-model>" \
  NEXUS_LOGIC_MODEL="<your-model>" \
  node tools/mcp/server.mjs 2>/dev/null | tail -1 | python3 -m json.tool
```

The health check should list your models as available. If a model isn't pulled, the generate calls will fail with an Ollama error (not a CIRCUIT_BREAKER).
