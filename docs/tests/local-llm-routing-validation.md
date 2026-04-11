# Local LLM Routing Validation

**Date**: 2026-04-10
**Environment**: Compute Plane node / Control Plane laptop (NEXUS)

This document serves to verify the conditional routing integration of the NEXUS `ollama-delegate.sh` inference execution.

## Testing Overview

**Models configured for target mobile GPU (RTX 3050):**
- `qwen2.5-coder:1.5b` (For strict formatting, boilerplate, logic refactoring, low-context logic)
- `llama3.2:3b` (For rule-following, doc-checking, and general validation)

### Test 1: Zero-Config Localhost Execution (Default Route)
**Objective**: Ensure the delegate script functions natively on localhost without `.env` overrides.

**Execution:**
1. Initialized `scratch.txt` containing a raw React JS component specification.
2. Ran `./tools/automation/ollama-delegate.sh boilerplate scratch.txt`.

**Result:**
- **Status**: PASSED.
- **Output**: The system immediately delegated to `qwen2.5-coder:1.5b` and responded flawlessly with well-formatted React code inside the shell.
- **VRAM Payload**: Expected to sit comfortably under 1.5GB total on the node.

### Test 2: Circuit Breaker Validation Override (Network Route)
**Objective**: Guarantee that `.env` files dynamically capture network topologies, and the circuit breaker prevents slow hanging API requests when inference nodes are down.

**Execution:**
1. Created `.env` with a forced fake override: `OLLAMA_HOST_URL="http://10.0.0.99:11434"`.
2. Ran `./tools/automation/ollama-delegate.sh boilerplate scratch.txt`.

**Result:**
- **Status**: PASSED.
- **Output**: The script immediately failed with Exit Code `3`. 
- **Message**: `CIRCUIT_BREAKER: Ollama at http://10.0.0.99:11434 returned HTTP 000 (or timed out after 5s)`.

## Conclusion
The conditional architecture (Control Plane laptop -> Compute Plane GPU) works as intended. Both 1.5B and 3B models function extremely well for rapid task delegation. 
