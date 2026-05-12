# AI Agent Kubernetes Abstraction - Project Guide

## Overview

This project implements a Kubernetes-native abstraction layer for running AI agents from multiple frameworks (ADK-Go, OpenClaw, LangChain, etc.) in a unified manner. It defines three core CRD objects that abstract any AI agent framework while externalizing platform engineering capabilities.

**E2E Tests Verified (2026-05-12)**:
- ✓ ADK Shared Mode: 2 AIAgents → 1 Framework process
- ✓ ADK Isolated Mode: 3 AIAgents → 3 Framework processes  
- ✓ OpenClaw Gateway Mode: 2 AIAgents → 2 Gateway processes

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
- `spec.agentConfig`: Agent-specific configuration files (ConfigMap/Secret references)
- `spec.volumePolicy`: PVC lifecycle (`retain` or `delete`)

**Lifecycle Phases:** `Pending | Scheduling | Running | Migrating | Failed | Terminated`

### 2. AgentRuntime (`api/v1/agentruntime_types.go`)

Runtime carrier that hosts AI Agents, maps to a Pod instance.

**Key Fields:**
- `spec.agentHandler`: Handler container spec (image, command, args, env, resources)
- `spec.agentFramework`: Framework container spec (image, type, command, args)
- `spec.harness`: References to Harness CRDs
- `spec.agentConfig`: Runtime-level public configuration
- `spec.processMode`: `shared` (single process multi-agent) or `isolated` (process per agent)
- `spec.replicas`: Number of Pod instances

**Lifecycle Phases:** `Pending | Creating | Running | Updating | Terminating | Failed`

### 3. Harness (`api/v1/harness_types.go`)

Independent CRD for AI Agent scaffolding capabilities.

**Supported Types:** `model | mcp | skills | knowledge | memory | state | guardrail | security | policy | sandbox`

**Key Harness Specs:**
- `ModelHarnessSpec`: LLM provider, endpoint, API key, allowed models, rate limits
- `MCPHarnessSpec`: MCP registry type, servers, discovery settings
- `MemoryHarnessSpec`: Backend type (inmemory/redis/file), TTL, persistence
- `SandboxHarnessSpec`: Type (gvisor/docker/kata), mode (external/embedded), endpoint, timeout
- `SkillsHarnessSpec`: Hub type, skill list, auto-update settings

## Key Packages

### `pkg/controller/` - Kubernetes Controllers

Framework-agnostic controllers that manage CRD lifecycles.

**AgentRuntimeReconciler (`agentruntime_controller.go`):**
- Creates/updates Pods based on AgentRuntime spec
- Resolves Harness references to ConfigMaps
- Uses ImageVolume pattern (K8s 1.35+) to mount Framework image to Handler
- `ShareProcessNamespace: true` for Handler to manage Framework processes

**Pod Architecture (ImageVolume Pattern - Verified):**
```
Pod (AgentRuntime)
├── Handler Container (process manager)
│   ├── Starts Framework processes via exec.Command
│   ├── Controls process lifecycle (start/stop/monitor)
│   └── VolumeMounts:
│       ├── /framework-rootfs -> ImageVolume (Framework image)
│       ├── /etc/harness/<name> -> Harness ConfigMaps
│       ├── /shared/workdir -> EmptyDir (agent workspace)
│       ├── /shared/config -> EmptyDir (runtime configs)
│       └── /etc/agent-config -> hostPath (Config Daemon)
│
└── Framework Container (DUMMY)
│   └── ENTRYPOINT: sleep infinity (just sleeps)
│   └── Provides image content for ImageVolume
│   └── Contains adk-go library integration
```

### `pkg/handler/` - Handler Interface

**Core Interface (`handler.go`):**

Handler's 4 Core Responsibilities:
1. **Configuration Transformation**: AIAgentSpec + HarnessConfig → Framework-specific config
2. **Framework Process Management**: Start/Stop/Restart framework processes
3. **Harness Adaptation**: Standard Harness → Framework-specific Harness config
4. **Agent Lifecycle**: Load/Start/Stop agents (via Framework)

**Handler Interface Methods:**
```go
type Handler interface {
    // Framework identification
    Type() HandlerType
    GetFrameworkInfo() *FrameworkInfo
    
    // Configuration Transformation
    GenerateFrameworkConfig(spec *v1.AIAgentSpec, harness *HarnessConfig) ([]byte, error)
    GenerateAgentConfig(spec *v1.AIAgentSpec, harness *HarnessConfig) ([]byte, error)
    
    // Process Management
    StartFramework(ctx, frameworkBin, workDir, configPath) error
    StartFrameworkInstance(ctx, instanceID, configPath) error
    StopFramework(ctx) error
    
    // Agent Lifecycle
    LoadAgent(ctx, spec, harness) (agent.Agent, error)
    StartAgent(ctx, agent, config) error
    
    // Capability Queries
    SupportsMultiAgent() bool
    SupportsMultiInstance() bool
}
```

**HandlerTypes:** `adk | openclaw | langchain | hermes | custom`

### `pkg/handler/adk/` - ADK-Go Handler (Verified)

Supports two process modes:
- **shared**: Single Framework process, multiple agents in same process (tested: 2 agents → 1 process)
- **isolated**: Each agent runs in its own Framework process (tested: 3 agents → 3 processes)

**Key Files:**
- `handler.go`: Main handler implementation
- `converter.go`: Converts AIAgentSpec to ADK YAML config

### `pkg/handler/openclaw/` - OpenClaw Handler (Verified)

OpenClaw Gateway architecture:
- Each AIAgent → One Gateway process (tested: 2 agents → 2 gateway processes)
- Handler manages multiple Gateway instances
- Communicates via HTTP API with Gateway

**Key Files:**
- `handler.go`: Gateway process management
- `converter.go`: Converts to openclaw.json config
- `bridge.go`: HTTP communication with Gateway
- `plugin_generator.go`: Generates harness-bridge plugins

### `cmd/adk-framework/` - ADK Framework with adk-go Integration

The adk-framework now integrates with adk-go library:

```go
import (
    adkagent "google.golang.org/adk/agent"
    "google.golang.org/adk/agent/llmagent"
    "google.golang.org/adk/runner"
    "google.golang.org/adk/session"
)

// Create agent using adk-go
agent, err := llmagent.New(llmagent.Config{
    Name:        config.Name,
    Model:       model,  // customModel implements model.LLM interface
    Description: config.Description,
    Instruction: instruction,
})

// Execute via runner
r, err := runner.New(runner.Config{
    Agent:           rootAgent,
    SessionService:  session.InMemoryService(),
    AutoCreateSession: true,
})

for event, err := range r.Run(ctx, userID, sessionID, msg, runConfig) {
    // Process events
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
Config Daemon (DaemonSet on all nodes)
├── Watches AIAgent CRDs via Informer
├── Writes to hostPath: /var/lib/aiagent/configs/<namespace>/<agent-name>/
│   ├── agent-config.json
│   └── agent-meta.yaml
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

Manages all harness instances for an AgentRuntime.

**HarnessManager (`harness.go`):**
- Initializes harnesses from HarnessSpec
- Provides unified access via `GetModelHarness()`, `GetMCPHarness()`, etc.
- Health checking for External Sandbox
- Workspace creation for session isolation

**Individual Harnesses:**
- `model.go`: LLM provider integration
- `mcp.go`: MCP registry and servers
- `memory.go`: Session/state storage
- `sandbox.go`: Execution isolation (embedded/external modes)
- `skills.go`: Skill/tool modules

### `pkg/scheduler/` - Agent Scheduling

**DefaultScheduler (`scheduler.go`):**
- Strategies: `binpack` (pack tightly), `spread` (distribute evenly), `firstfit`
- Scoring based on agent count, framework type match, runtime health
- CanSchedule checks: phase, namespace, framework type

**MigrationScheduler:**
- Handles agent migration between runtimes
- Migration phases: `Prepare | Transfer | Activate | Cleanup`

### `pkg/agent/` - Agent Core Abstraction

**Agent Interface (`agent.go`):**
```go
type Agent interface {
    Name() string
    Description() string
    Type() AgentType  // llm | sequential | parallel | loop | remote | custom
    Run(ctx InvocationContext) iter.Seq2[*Event, error]
    SubAgents() []Agent
    FindAgent(name string) Agent
    BeforeAgentCallbacks() []BeforeAgentCallback
    AfterAgentCallbacks() []AfterAgentCallback
}
```

**Content/Part Types:**
- `Content`: Message with role ("user" or "model") and parts
- `Part`: Text, InlineData, FunctionCall, FunctionResponse, CodeExecutionResult

## Configuration Files

### Mount Paths

| Source | Mount Path |
|--------|-----------|
| Runtime agentConfig | `/etc/agent-config/runtime/` |
| AIAgent agentConfig | `/etc/agent-config/agent/<name>/` |
| Harness ConfigMaps | `/etc/harness/<harness-name>/` |
| Shared workspace | `/shared/workdir/` |
| Shared config | `/shared/config/` |
| Framework image | `/framework-rootfs/` |

### agentConfig vs Harness

| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| Positioning | Platform engineering capabilities | Agent/Handler/Framework config |
| Examples | Model, MCP, Sandbox, Skills | Prompt, protocol config |
| Processing | Platform-level by Agent ID | Handler determines format |
| Responsibility | Platform manages | Handler processes |

## Sample YAML Configurations

### Harness Example
```yaml
apiVersion: agent.ai/v1
kind: Harness
metadata:
  name: model-harness-deepseek
spec:
  type: model
  model:
    provider: deepseek
    endpoint: https://api.deepseek.com
    defaultModel: deepseek-chat
    models:
    - name: deepseek-chat
      allowed: true
```

### AgentRuntime Example (ADK Isolated Mode)
```yaml
apiVersion: agent.ai/v1
kind: AgentRuntime
metadata:
  name: adk-isolated-runtime
spec:
  processMode: isolated
  agentHandler:
    image: aiagent/adk-handler:test
    env:
    - name: PROCESS_MODE
      value: isolated
  agentFramework:
    image: aiagent/adk-framework:test
    type: adk
  harness:
  - name: adk-model
  replicas: 1
```

### AIAgent Example
```yaml
apiVersion: agent.ai/v1
kind: AIAgent
metadata:
  name: isolated-agent-1
  labels:
    runtime: adk-isolated-runtime
spec:
  description: "Agent with isolated process"
  runtimeRef:
    type: adk
    name: adk-isolated-runtime
  agentConfig:
    instruction: "You are a specialized AI assistant."
    model: "deepseek-chat"
```

## Extending with New Frameworks

To add a new framework (e.g., Hermes):

1. Create `pkg/handler/hermes/` package
2. Implement `handler.Handler` interface:
   - `GenerateFrameworkConfig()` - Convert to Hermes config format
   - `StartFramework()` - Start Hermes process
   - `AdaptModelHarness()`, `AdaptMCPHarness()`, etc.
3. Create converter for AIAgentSpec → Hermes config
4. Register in handler registry (if used)
5. Create Dockerfile for handler image
6. Create E2E test case in `test/e2e/kind/manifests/`

## Testing

```bash
# Unit tests
make test-unit

# E2E tests (requires Kind cluster)
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
3. **Harness Externalization**: Platform capabilities (Model, MCP, Sandbox) referenced by name
4. **Dynamic Scheduling**: AIAgent can migrate between AgentRuntimes
5. **Process Isolation**: `ShareProcessNamespace: true` for Handler to manage Framework processes
6. **ImageVolume Pattern**: Framework image mounted to Handler for process execution (K8s 1.35+)
7. **Config Daemon**: Solution M for agent config distribution without Pod K8s API access