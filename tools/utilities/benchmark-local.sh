#!/usr/bin/env bash
# benchmark-local.sh
# Tests multiple local LLM models across different task complexities to 
# evaluate parameter boundaries (up to ~9B) on a Mobile 4GB RTX 3050.

set -euo pipefail

OLLAMA_HOST="${OLLAMA_HOST_URL:-http://localhost:11434}"

# Escalating model sizes to test VRAM spillover and parameter bounds
MODELS=(
  "qwen2.5:0.5b"
  "qwen2.5-coder:1.5b"
  "gemma2:2b"
  "llama3.2:3b"
  "qwen2.5:3b"
  "qwen:4b"  # 4B scale
  "qwen2.5:7b" # Top bound (VRAM spill expected)
)

declare -a PROMPTS=(
  "Tier 1 (Trivial Boilerplate): Generate a modern React functional component named 'SubmitButton' that takes an onClick prop. Only return code."
  "Tier 2 (Moderate Algorithmic): Refactor the following nested loop into optimized vectorized Python utilizing numpy: 'for i in range(100): for j in range(100): a[i][j]=i*j'. Return only code."
  "Tier 3 (Complex Architecture): Architect a secure multithreaded database connection pool class in C++17. Use std::mutex and RAII. Provide only the header implementation."
)

echo "Starting Local Telemetry Benchmarking Suite..."
echo "Node: $OLLAMA_HOST"
echo ""

DOCS_FILE="docs/tests/performance-benchmarks.md"
mkdir -p "$(dirname "$DOCS_FILE")"

cat << 'EOF' > "$DOCS_FILE"
# Local LLM Routing Telemetry Benchmarks

## hardware Context
- **Control Plane**: Local Application
- **Compute Node**: Mobile RTX 3050 (4GB VRAM)
- **Objective**: Identify generation speed (Tokens/sec) dropoffs across 0.5B -> 9B scales over three distinct complexities.

## Results Table

| Model | Size | Prompt Tier | Eval Count | Eval Duration (s) | Speed (Tokens/s) |
|---|---|---|---|---|---|
EOF

for model in "${MODELS[@]}"; do
  echo ">>> Ensuring model is pulled: $model"
  # Pre-pull silently so pull time doesn't affect TTFT metrics
  curl --silent --fail -X POST "$OLLAMA_HOST/api/pull" -d "{\"name\": \"$model\"}" > /dev/null || { echo "Failed to pull $model, skipping."; continue; }

  for i in "${!PROMPTS[@]}"; do
    tier=$((i+1))
    prompt="${PROMPTS[$i]}"
    
    echo "  > Testing [$model] | Tier $tier"
    
    # JSON payload
    payload=$(jq -n --arg m "$model" --arg p "$prompt" '{model: $m, prompt: $p, stream: false}')
    
    # Request
    resp=$(curl --silent --fail -X POST "$OLLAMA_HOST/api/generate" \
      -H "Content-Type: application/json" \
      -d "$payload" 2>/dev/null) || { echo "    -> Failed generation."; continue; }
      
    # Extract telemetry
    eval_count=$(echo "$resp" | jq -r '.eval_count // 0')
    eval_dur_ns=$(echo "$resp" | jq -r '.eval_duration // 0')
    
    if [[ "$eval_count" == "0" || "$eval_dur_ns" == "0" ]]; then
       echo "    -> Invalid telemetry received."
       continue
    fi
    
    # Convert ns to seconds
    eval_dur_s=$(awk -v ns="$eval_dur_ns" 'BEGIN { printf "%.2f", ns / 1000000000 }')
    
    # Calculate Tokens/Sec
    tps=$(awk -v c="$eval_count" -v d="$eval_dur_s" 'BEGIN { if(d>0) printf "%.2f", c/d; else print "0.00" }')
    
    echo "    -> Completed. Eval Count: $eval_count, Duration: ${eval_dur_s}s, Speed: ${tps} t/s"
    
    # Write directly to markdown table
    echo "| \`$model\` | ${model##*:} | $tier | $eval_count | $eval_dur_s | **$tps** |" >> "$DOCS_FILE"
    
  done
  echo "---------------------------------------------------"
done

echo ""
echo "Benchmarks completed successfully! Output written to $DOCS_FILE."
