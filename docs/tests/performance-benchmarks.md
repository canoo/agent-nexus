# Local LLM Routing Telemetry Benchmarks

## hardware Context
- **Control Plane**: Local Application
- **Compute Node**: Mobile RTX 3050 (4GB VRAM)
- **Objective**: Identify generation speed (Tokens/sec) dropoffs across 0.5B -> 9B scales over three distinct complexities.

## Results Table

| Model | Size | Prompt Tier | Eval Count | Eval Duration (s) | Speed (Tokens/s) |
|---|---|---|---|---|---|
| `qwen2.5:0.5b` | 0.5b | 1 | 32 | 0.14 | **228.57** |
| `qwen2.5:0.5b` | 0.5b | 2 | 217 | 0.83 | **261.45** |
| `qwen2.5:0.5b` | 0.5b | 3 | 775 | 3.03 | **255.78** |
| `qwen2.5-coder:1.5b` | 1.5b | 1 | 107 | 0.87 | **122.99** |
| `qwen2.5-coder:1.5b` | 1.5b | 2 | 123 | 1.00 | **123.00** |
| `qwen2.5-coder:1.5b` | 1.5b | 3 | 437 | 3.59 | **121.73** |
| `gemma2:2b` | 2b | 1 | 38 | 0.44 | **86.36** |
| `gemma2:2b` | 2b | 2 | 285 | 3.37 | **84.57** |
| `gemma2:2b` | 2b | 3 | 640 | 7.64 | **83.77** |
| `llama3.2:3b` | 3b | 1 | 71 | 0.97 | **73.20** |
| `llama3.2:3b` | 3b | 2 | 34 | 0.45 | **75.56** |
| `llama3.2:3b` | 3b | 3 | 717 | 10.11 | **70.92** |
| `qwen2.5:3b` | 3b | 1 | 70 | 0.96 | **72.92** |
| `qwen2.5:3b` | 3b | 2 | 53 | 0.72 | **73.61** |
| `qwen2.5:3b` | 3b | 3 | 888 | 12.48 | **71.15** |
| `qwen:4b` | 4b | 1 | 61 | 2.08 | **29.33** |
| `qwen:4b` | 4b | 2 | 73 | 2.58 | **28.29** |
| `qwen:4b` | 4b | 3 | 248 | 8.86 | **27.99** |
| `qwen2.5:7b` | 7b | 1 | 45 | 3.43 | **13.12** |
| `qwen2.5:7b` | 7b | 2 | 81 | 6.53 | **12.40** |
| `qwen2.5:7b` | 7b | 3 | 823 | 82.72 | **9.95** |
