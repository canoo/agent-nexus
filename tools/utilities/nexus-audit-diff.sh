#!/usr/bin/env bash
# -------------------------------------------------------------
# nexus-audit-diff.sh
# 
# Usage: ./nexus-audit-diff.sh <repo_A_path> <repo_B_path>
# Description: Takes two repository paths, maps their structure,
# and pipes them to a Tier 1 model for architectural gap analysis.
# -------------------------------------------------------------

set -e

REPO_A="$1"
REPO_B="$2"

if [ -z "$REPO_A" ] || [ -z "$REPO_B" ]; then
    echo "Error: Must provide two repository paths."
    echo "Usage: $0 <repo_A_path> <repo_B_path>"
    exit 1
fi

if [ ! -d "$REPO_A" ] || [ ! -d "$REPO_B" ]; then
    echo "Error: Both arguments must be valid directory paths."
    exit 1
fi

echo "Mapping structure for baseline ($REPO_A) and target ($REPO_B)..."

# Use tree to map the directory structure, excluding common noise
map_structure() {
    local repo_path="$1"
    if command -v tree >/dev/null 2>&1; then
        tree -L 3 -a -I '.git|node_modules|dist|build|vendor' "$repo_path"
    else
        # Fallback if tree is not installed
        find "$repo_path" -maxdepth 3 -not -path '*/.*' | sed "s|$repo_path||"
    fi
}

STRUCTURE_A=$(map_structure "$REPO_A")
STRUCTURE_B=$(map_structure "$REPO_B")

echo "Generating analysis prompt for Tier 1 Agent..."

cat <<EOF > nexus-audit-prompt.md
# NEXUS Architectural Gap Analysis

You are a senior systems architect. Your task is to perform a gap analysis between two repositories to identify architectural differences, missing patterns, and integration opportunities.

## Repository A (Baseline)
Location: $REPO_A
Structure:
\`\`\`
$STRUCTURE_A
\`\`\`

## Repository B (Target)
Location: $REPO_B
Structure:
\`\`\`
$STRUCTURE_B
\`\`\`

## Task
1. **Structural Audit**: Compare the directory layouts. Identify key architectural patterns present in A but missing in B (and vice-versa).
2. **Gap Identification**: Highlight missing core components, utility layers, or configuration patterns in Repository B that are established in Repository A.
3. **Integration Strategy**: Suggest how Repository B could adopt the strengths of Repository A without introducing unnecessary complexity.
4. **Consistency Check**: Look for naming convention drifts and standard violations.

Output your analysis in Markdown format.
EOF

echo "Analysis prompt generated at: nexus-audit-prompt.md"
echo "You can now pipe this to your Tier 1 Agent (e.g., 'cat nexus-audit-prompt.md | claude' or 'gemini-audit.sh nexus-audit-prompt.md .')"
