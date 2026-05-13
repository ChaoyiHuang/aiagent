# AI Agent Kubernetes Abstraction - Project Guide

## Overview

This project implements a Kubernetes-native abstraction layer for running AI agents from multiple frameworks (ADK-Go, OpenClaw, LangChain, etc.) in a unified manner. It defines three core CRD objects that abstract any AI agent framework while externalizing platform engineering capabilities.

**E2E Tests Verified (2026-05-12)**:
- ✓ ADK Shared Mode: 2 AIAgents → 1 Framework process
- ✓ ADK Isolated Mode: 3 AIAgents → 3 Framework processes
- ✓ OpenClaw Gateway Mode: 2 AIAgents → 2 Gateway processes

## Project Structure

```
aiagent/
├── api/v1/                    # CRD type definitions
├── cmd/                       # Binary entry points
│   ├── manager/               # Controller Manager (runs all K8s controllers)
│   ├── config-daemon/         # Config Daemon (syncs agent configs to hostPath)
│   ├── adk-framework/         # ADK Framework (JSON-RPC server, adk-go integration)
│   ├── adk-handler/           # ADK Handler (process manager for ADK)
│   └── openclaw-handler/      # OpenClaw Handler (Gateway process manager)
├── config/                    # Kubernetes config files
│   ├── crd/bases/             # CRD YAML definitions
│   ├── rbac/                  # RBAC role definitions
│   └── samples/               # Sample YAML configurations
├── pkg/
│   ├── controller/            # Kubernetes controllers
│   ├── handler/               # Handler interface and implementations
│   │   ├── base/              # Base handler utilities
│   │   ├── adk/               # ADK-Go handler implementation
│   │   └── openclaw/          # OpenClaw handler implementation
│   ├── harness/               # Harness manager and implementations
│   ├── scheduler/             # Agent scheduling logic
│   └── agent/                 # Agent core abstraction
├── test/e2e/kind/             # E2E test scripts and manifests
└── Dockerfile.*               # Docker images for all components
```

## Architecture Layers

```
┌─────────────────────────────────────┐
│         AIAgent (Business Object)    │
│    - Independent CRD, schedulable    │
│    - Binds Harness customization     │
└─────────────────────────────────────┘
              │
              │ Scheduling/Mapping
              ▼
┌─────────────────────────────────────┐
│      AgentRuntime (Runtime Carrier)  │
│    - Agent Handler + Agent Framework │
│    - Binds public Harness configs    │
│    - 1:1 mapping to Pod              │
└─────────────────────────────────────┘
              │
              │ Reference
              ▼
┌─────────────────────────────────────┐
│         Harness (Scaffolding)        │
│    - Namespace-level independent CRD │
│    - Model, Memory, Sandbox, etc.    │
└─────────────────────────────────────┘
```

## Core CRD Objects

### 1. AIAgent (`api/v1/aigent_types.go`)

Business-level object representing an individual AI Agent instance.

**Key Fields:**
- `spec.runtimeRef`: Scheduling reference (type-based auto scheduling or name-based fixed binding)
- `spec.harnessOverride`: Customize inherited harness capabilities (cannot append new, only override/deny)
- `spec.agentConfig`: Agent-specific configuration (framework-specific format)
- `spec.volumePolicy`: PVC lifecycle (`retain` or `delete`)

**Lifecycle Phases:** `Pending | Scheduling | Running | Migrating | Failed | Terminated`

### 2. AgentRuntime (`api/v1/agentruntime_types.go`)

Runtime carrier that hosts AI Agents, maps to a Pod instance.

**Key Fields:**
- `spec.agentHandler`: Handler container spec (image, command, args, env, resources)
- `spec.agentFramework`: Framework container spec (image, type)
- `spec.harness`: References to Harness CRDs
- `spec.processMode`: `shared` (single process multi-agent) or `isolated` (process per agent)
- `spec.replicas`: Number of Pod instances

**Lifecycle Phases:** `Pending | Creating | Running | Updating | Terminating | Failed`

### 3. Harness (`api/v1/harness_types.go`)

Independent CRD for AI Agent scaffolding capabilities.

**Supported Types:** `model | mcp | skills | knowledge | memory | state | guardrail | security | policy | sandbox`

## Pod Architecture (ImageVolume Pattern)

```
Pod (AgentRuntime)
├── Handler Container (process manager)
│   ├── Starts Framework processes via exec.Command
│   ├── Controls process lifecycle (start/stop/monitor)
│   ├── No resource limits (shares Pod quota)
│   └── VolumeMounts:
│       ├── /framework-rootfs -> ImageVolume (Framework image)
│       ├── /etc/harness/<name> -> Harness ConfigMaps
│       ├── /shared/workdir -> EmptyDir (agent workspace)
│       ├── /shared/config -> EmptyDir (runtime configs)
│       └── /etc/agent-config -> hostPath (Config Daemon)
│
└── Framework Container (DUMMY)
│   └── ENTRYPOINT: sleep infinity
│   └── Provides image content for ImageVolume
│   ├── No resource limits (shares Pod quota)
│
└── ShareProcessNamespace: true
└── ShareNetworkNamespace: true (implicit)
```

**Design Note:** Handler and Framework containers share Pod resource quota. No individual container resource limits are set. Define Pod-level resources via `spec.agentHandler.resources` in AgentRuntime CRD.

## Key Packages

### `pkg/controller/` - Kubernetes Controllers

Framework-agnostic controllers that manage CRD lifecycles.

| File | Description |
|------|-------------|
| `agentruntime_controller.go` | Creates Pods with ImageVolume + ShareProcessNamespace |
| `aigent_controller.go` | Schedules AIAgents to AgentRuntimes |
| `harness_controller.go` | Manages Harness CRDs |

### `pkg/handler/` - Handler Interface

**Core Interface (`handler.go`):**

Handler's 4 Core Responsibilities:
1. **Configuration Transformation**: AIAgentSpec + HarnessConfig → Framework-specific config
2. **Framework Process Management**: Start/Stop/Restart framework processes
3. **Harness Adaptation**: Standard Harness → Framework-specific config
4. **Agent Lifecycle**: Load/Start/Stop agents

**HandlerTypes:** `adk | openclaw | langchain | hermes | custom`

### `pkg/handler/base/` - Base Handler Utilities

| File | Description |
|------|-------------|
| `config.go` | Configuration loading utilities |
| `executor.go` | Process execution helpers |
| `harness_loader.go` | Harness config loading from ConfigMaps |
| `jsonrpc.go` | JSON-RPC communication utilities |
| `process.go` | Process lifecycle management |

### `pkg/handler/adk/` - ADK-Go Handler (Verified)

**Process Modes:**
- **shared**: Single Framework process, multiple agents (tested: 2 agents → 1 process)
- **isolated**: Each agent in own Framework process (tested: 3 agents → 3 processes)

**Key Files:**
- `handler.go`: Main handler implementation, process management
- `converter.go`: Converts AIAgentSpec to ADK config

### `pkg/handler/openclaw/` - OpenClaw Handler (Verified)

**Gateway Architecture:**
- Each AIAgent → One Gateway process (tested: 2 agents → 2 gateway processes)
- Handler manages multiple Gateway instances
- Each Gateway can manage internal sub-agents (invisible to Kubernetes)

**Gateway Startup Parameters:**
```bash
openclaw gateway \
  --allow-unconfigured \
  --bind loopback \
  --port <port>           # basePort + instanceIndex (18789, 18790...)
  --auth none \
  --force \
  --config <configPath>   # Generated JSON config
```

**Environment Variables:**
- `OPENCLAW_CONFIG_DIR=<configDir>`
- `OPENCLAW_STATE_DIR=<workDir>`

**Key Files:**
- `handler.go`: Gateway process management, health monitoring
- `converter.go`: Converts agentConfig to openclaw.json
- `bridge.go`: HTTP communication with Gateway
- `plugin_generator.go`: Generates harness-bridge plugins

### `cmd/adk-framework/` - ADK Framework

**adk-go Integration:**
```go
import (
    "google.golang.org/adk/agent/llmagent"
    "google.golang.org/adk/runner"
    "google.golang.org/adk/session"
)

// Create agent
agent, err := llmagent.New(llmagent.Config{
    Name:        config.Name,
    Model:       customModel,  // Implements model.LLM interface
    Description: config.Description,
    Instruction: instruction,
})

// Execute via runner
r, err := runner.New(runner.Config{
    Agent:           rootAgent,
    SessionService:  session.InMemoryService(),
})

for event, err := range r.Run(ctx, userID, sessionID, msg) {
    // Process event stream
}
```

**JSON-RPC Methods:**
- `agent.run`: Execute agent with user message
- `agent.status`: Query agent status
- `agent.list`: List all agents
- `framework.status`: Framework health info

### `cmd/config-daemon/` - Config Daemon (Solution M)

Config Daemon watches AIAgent CRDs and syncs AgentConfig to hostPath:

```
Config Daemon (DaemonSet)
├── Watches AIAgent CRDs via Informer
├── Writes to hostPath: /var/lib/aiagent/configs/<namespace>/<agent-name>/
│   ├── agent-config.json   # Agent-specific config
│   └── agent-meta.yaml     # Metadata (name, phase, runtime)
├── Creates agent-index.yaml in namespace directory
│
Pod (AgentRuntime)
├── Mounts hostPath as /etc/agent-config
└── Handler reads agent-index.yaml to discover agents
```

**Benefits:**
- Handler doesn't need K8s API access
- No RBAC permissions required for Handler
- Works with ShareProcessNamespace pattern

### `pkg/harness/` - Harness Manager

Manages harness instances from HarnessSpec.

| File | Description |
|------|-------------|
| `harness.go` | HarnessManager, initialization, unified access |
| `model.go` | LLM provider integration |
| `mcp.go` | MCP registry and servers |
| `memory.go` | Session/state storage |
| `sandbox.go` | Execution isolation (embedded/external) |
| `skills.go` | Skill/tool modules |

### `pkg/scheduler/` - Agent Scheduling

**DefaultScheduler:**
- Strategies: `binpack`, `spread`, `firstfit`
- Scoring: agent count, framework type, runtime health
- CanSchedule checks: phase, namespace, framework type

### `pkg/agent/` - Agent Core Abstraction

**Agent Interface:**
```go
type Agent interface {
    Name() string
    Description() string
    Type() AgentType  // llm | sequential | parallel | loop | remote | custom
    Run(ctx InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent
}
```

## Configuration Mount Paths

| Source | Mount Path |
|--------|-----------|
| Agent configs (hostPath) | `/etc/agent-config/<agent-name>/` |
| Harness ConfigMaps | `/etc/harness/<harness-name>/` |
| Shared workspace | `/shared/workdir/` |
| Shared config | `/shared/config/` |
| Framework image | `/framework-rootfs/` |

## agentConfig vs Harness

| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| Positioning | Platform engineering capabilities | Agent/Handler/Framework config |
| Examples | Model, MCP, Sandbox, Skills | Prompt, gateway config, internal agents |
| Processing | Platform-level by Agent ID | Handler determines format |
| Responsibility | Platform manages | Handler processes |

## OpenClaw agentConfig Example

```yaml
agentConfig:
  gateway:
    port: 18789
    host: "0.0.0.0"
  agents:               # Internal sub-agents (invisible to Kubernetes)
    - name: weather
      description: "Weather information agent"
      model: "gpt-4o"
      tools:
        - name: get_weather
          type: api_call
    - name: calculator
      description: "Math calculation agent"
      model: "gpt-4o"
```

## Docker Images

| Dockerfile | Description |
|------------|-------------|
| `Dockerfile.manager` | Controller Manager (runs K8s controllers) |
| `Dockerfile.config-daemon` | Config Daemon (syncs configs to hostPath) |
| `Dockerfile.adk-framework` | ADK Framework (DUMMY, provides image for ImageVolume) |
| `Dockerfile.adk-handler` | ADK Handler (process manager) |
| `Dockerfile.openclaw-framework` | OpenClaw Framework (Node.js, provides image) |
| `Dockerfile.openclaw-handler` | OpenClaw Handler (Gateway manager) |

**Note:** All Dockerfiles clone adk-go from `https://github.com/google/adk-go` during build.

## Testing

```bash
# E2E tests (requires Kind cluster with K8s 1.35+)
./test/e2e/kind/run-e2e-test.sh all

# Test specific modes
./test/e2e/kind/run-e2e-test.sh test
```

## Deployment to Kind

```bash
./test/e2e/kind/run-e2e-test.sh all  # Build and deploy everything
```

## Key Design Principles

1. **Framework Agnostic**: Controller doesn't know about ADK, OpenClaw - all comes from spec
2. **Handler Pattern**: Handler provided by framework community, adapts to unified interface
3. **Handler Direct Creation**: Handler created directly based on framework type (no registry)
4. **Harness Externalization**: Platform capabilities referenced by name
5. **Process Isolation**: `ShareProcessNamespace: true` for Handler to manage Framework processes
6. **ImageVolume Pattern**: Framework image mounted to Handler (K8s 1.35+)
7. **Config Daemon**: Solution M for agent config distribution without Pod K8s API access
8. **Shared Resources**: Handler and Framework share Pod quota, no individual container limits