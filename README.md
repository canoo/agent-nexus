# NEXUS Environment (Agentic Framework)

NEXUS (Network of EXperts, Unified in Strategy) is a central repository for defining multi-model agentic behaviors, personas, prompts, and orchestration tools.

## Architecture

This repository operates by decoupling monolithic agent `.md` files into a lightweight, structural format. Once instantiated out to your local environment (via `setup-nexus.sh`), the OS hot-swaps dotfiles to point straight into this centralized workflow.

## Developer Workflow

Here is how the NEXUS Orchestrator routes interactions:

```mermaid
graph TD
    %% Define styles
    classDef user fill:#6c5ce7,stroke:#333,stroke-width:2px,color:#fff;
    classDef orchestrator fill:#00b894,stroke:#333,stroke-width:2px,color:#fff;
    classDef agentT1 fill:#fdcb6e,stroke:#333,stroke-width:2px,color:#111;
    classDef agentT2 fill:#e17055,stroke:#333,stroke-width:2px,color:#fff;
    classDef agentT3 fill:#d63031,stroke:#333,stroke-width:2px,color:#fff;
    classDef memory fill:#0984e3,stroke:#333,stroke-width:2px,color:#fff;

    %% Nodes
    A[User Request]:::user --> B(NEXUS Orchestrator<br/><br/>core/NEXUS.md):::orchestrator
    
    B -->|Check Context < 50%| M[(Local Repo<br/>agent-memory/)]:::memory

    B -->|Routing Analysis| R{Select Tool/Model}
    
    %% Tier 1
    R -->|Deep Architecture| T1[Tier 1 High-Context Model<br/><br/>Gemini Pro / Claude Opus]:::agentT1
    T1 -->|Spawn Specialist| S1((Persona<br/>e.g. toolkiit-migrator))
    
    %% Tier 2
    R -->|Summaries/Basic UI| T2[Tier 2 Fast Cloud<br/><br/>Gemini Flash]:::agentT2
    T2 -->|Spawn Specialist| S2((Persona<br/>e.g. build-agent))

    %% Tier 3
    R -->|Linting/Simple Scripts| T3[Tier 3 Local Setup<br/><br/>Ollama / Llama 3]:::agentT3
    T3 -->|Execute locally| S3((Tool<br/>e.g. ollama-delegate.sh))

    %% Result cycle
    S1 --> Final[Task Complete]
    S2 --> Final
    S3 --> Final
    Final --> M
```

## Structure
- `core/`: Core instructions (`NEXUS.md` replacing `GEMINI.md`).
- `personas/`: Granular agent personas.
- `tools/`: Utility Python and Bash scripts.
- `prompts/`: Standard engineering rules and quality gates.
- `mcp-configs/`: Server configuration standards.
- `agent-memory/`: Locally tracked storage structure (not synced to source control).

## Installation
Run `./setup-nexus.sh` to initialize symlinking to `~/.gemini/` and `~/.config/nexus/`.
Run `./teardown-nexus.sh` to revert to baseline static files.
