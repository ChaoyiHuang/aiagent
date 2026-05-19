# AI Agent Abstraction

A Kubernetes-native abstraction that makes AI Agent a first-class citizen through three-layer CRD architecture.

## Three-Layer Architecture

**AIAgent** (Business Object) - Independent, schedulable CRD. Binds Harness customization via override (cannot add new capabilities).

**AgentRuntime** (Runtime Carrier) - Agent Handler + Agent Framework, 1:1 mapping to Pod. Binds public Harness configs.

**Harness** (Scaffolding) - Namespace-level independent CRD for platform capabilities: Model, MCP, Memory, Sandbox, Skills.

## Agent Handler Pattern

Handler bridges platform (Controller) from framework. Responsibilities: framework startup, config transformation, AI Agent lifecycle. Platform knows nothing about specific frameworks.

## Process Modes

**Shared** - Single Framework process, multiple Agents. Verified: 2 AIAgents → 1 process (ADK-Go).

**Isolated** - One Framework process per Agent. Verified: 3 AIAgents → 3 processes (ADK-Go), 2 AIAgents → 2 Gateway processes (OpenClaw).

## ImageVolume Pattern (K8s 1.35+)

Pod uses ShareProcessNamespace. Handler Container manages Framework processes. Framework Container is DUMMY (sleep infinity), provides image content via ImageVolume mount.

## Hands-on: E2E Tests

**ADK Test** - Validates Handler pattern and process modes:
```bash
cd aiagent/test/e2e/kind && ./run-e2e-test.sh
```
Verifies: ADK Shared mode (2 agents → 1 process), ADK Isolated mode (3 agents → 3 processes).

**OpenClaw Discord Test** - Validates Gateway mode and secret resolution:
```bash
export DISCORD_BOT_TOKEN="token" DISCORD_USER_IDS="ids" DEEPSEEK_API_KEY="key"
cd aiagent/test/e2e/kind && ./run-e2e-test-discord.sh
```
Verifies: OpenClaw Gateway mode (per-agent isolation), Config Daemon secret resolution flow.

Requirements: Docker, Kind v0.31.0, Kubernetes v1.35.0 (ImageVolume feature gate).