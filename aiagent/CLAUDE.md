# AI Agent Kubernetes Abstraction - Project Guide

## Overview

This project implements a Kubernetes-native abstraction layer for running AI agents from multiple frameworks (ADK-Go, OpenClaw, LangChain, etc.) in a unified manner. It defines three core CRD objects that abstract any AI agent framework while externalizing platform engineering capabilities.

**E2E Tests Verified (2026-05-12)**:
- ✓ ADK Shared Mode: 2 AIAgents → 1 Framework process
- ✓ ADK Isolated Mode: 3 AIAgents → 3 Framework processes
- ✓ OpenClaw Gateway Mode: 2 AIAgents → 2 Gateway processes
- ✓ OpenClaw Discord Integration: tokenSecretRef → Gateway subprocess env vars

## Project Structure

```
aiagent/
├── api/v1/                    # CRD type definitions
├── cmd/                       # Binary entry points
│   ├── manager/               # Controller Manager (runs all K8s controllers)
│   ├── config-daemon/         # Config Daemon (syncs agent configs to hostPath, resolves secrets)
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
│    - agentConfig with tokenSecretRef │
└─────────────────────────────────────┘
              │
              │ Scheduling/Mapping
              ▼
┌─────────────────────────────────────┐
│      AgentRuntime (Runtime Carrier)  │
│    - Agent Handler + Agent Framework │
│    - Binds public Harness configs    │
│    - 1:1 mapping to Pod              │
│    - Per-runtime hostPath mount      │
└─────────────────────────────────────┘
              │
              │ Reference
              ▼
┌─────────────────────────────────────┐
│         Harness (Scaffolding)        │
│    - Namespace-level independent CRD │
│    - Model, Memory, Sandbox, etc.    │
│    - Generated as harness.json       │
└─────────────────────────────────────┘
```

## Core CRD Objects

### 1. AIAgent (`api/v1/aigent_types.go`)

Business-level object representing an individual AI Agent instance.

**Key Fields:**
- `spec.runtimeRef`: Scheduling reference (type-based auto scheduling or name-based fixed binding)
- `spec.harnessOverride`: Customize inherited harness capabilities (cannot append new, only override/deny)
- `spec.agentConfig`: Agent-specific configuration (framework-specific format, can include tokenSecretRef)
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

## Pod Architecture (ImageVolume Pattern + Per-Runtime hostPath)

```
Pod (AgentRuntime)
├── Handler Container (process manager)
│   ├── Starts Framework processes via exec.Command
│   ├── Controls process lifecycle (start/stop/monitor)
│   ├── No resource limits (shares Pod quota)
│   └── VolumeMounts:
│       ├── /framework-rootfs -> ImageVolume (Framework image)
│       ├── /etc/harness/<name> -> Harness ConfigMaps (harness.json)
│       ├── /shared/workdir -> EmptyDir (agent workspace)
│       ├── /shared/config -> EmptyDir (runtime configs)
│       └── /etc/agent-config -> hostPath (PER-RUNTIME Config Daemon output)
│           ├── agent-index.yaml (agents for THIS runtime only)
│           └── <agent-name>/agent-config.json (resolved secrets)
│
├── Framework Container (DUMMY)
│   └── ENTRYPOINT: sleep infinity
│   └── Provides image content for ImageVolume
│
├── Config Daemon (DaemonSet on same node)
│   ├── Watches AIAgent CRDs via Informer
│   ├── Writes AgentConfig to PER-RUNTIME hostPath
│   ├── Path: /var/lib/aiagent/configs/<ns>/<runtime-name>/
│   ├── Resolves tokenSecretRef to actual token values
│   └── Creates agent-index.yaml per runtime
│
└── ShareProcessNamespace: true
└── ShareNetworkNamespace: true (implicit)
```

**Design Note:** Handler and Framework containers share Pod resource quota. No individual container resource limits are set. Define Pod-level resources via `spec.agentHandler.resources` in AgentRuntime CRD.

## Config Daemon Architecture (Solution M - Per-Runtime hostPath)

Config Daemon watches AIAgent CRDs and syncs AgentConfig to hostPath with secret resolution:

```
Config Daemon (DaemonSet)
├── Watches AIAgent CRDs via Informer
├── Writes to hostPath: /var/lib/aiagent/configs/<namespace>/<runtime-name>/
│   ├── agent-index.yaml        # Agents for THIS runtime only
│   └── <agent-name>/           # Per-agent directory
│       ├── agent-config.json   # Resolved agentConfig (secrets replaced)
│       └── agent-meta.yaml     # Metadata (name, phase, runtime)
│
├── resolveTokenSecretRefs()    # Framework-agnostic secret resolution
│   - Recursively walks agentConfig JSON
│   - Finds any tokenSecretRef field
│   - Resolves to actual token from K8s Secret
│   - Used for: Discord, Telegram, Slack, any channel tokens
│
Pod (AgentRuntime)
├── Mounts PER-RUNTIME hostPath as /etc/agent-config
├── Handler reads agent-index.yaml (only its agents)
└── Handler reads resolved agent-config.json (tokens already resolved)
```

**Benefits:**
- Handler doesn't need K8s API access
- No RBAC permissions required for Handler
- Channel tokens resolved by Config Daemon (framework-agnostic)
- Handler injects resolved tokens as subprocess env vars

## Secret Resolution Flow (Framework-Agnostic)

```
AIAgent CRD
└── spec.agentConfig
    └── channels.discord.tokenSecretRef: "discord-bot-token-secret"
        │
        ▼ Config Daemon (resolveTokenSecretRefs)
        └── Read Secret "discord-bot-token-secret"
        └── Replace tokenSecretRef with actual token
        └── Write agent-config.json with resolved token
        │
        ▼ OpenClaw Handler (extractOpenClawChannelEnvVars)
        └── Read agent-config.json from hostPath
        └── Extract channels.discord.token
        └── Inject as DISCORD_BOT_TOKEN=<token> subprocess env var
        │
        ▼ Gateway Process
        └── Receives DISCORD_BOT_TOKEN in environment
        └── Reads openclaw.json with SecretInput pointing to env var
```

## Key Packages

### `pkg/controller/` - Kubernetes Controllers

Framework-agnostic controllers that manage CRD lifecycles.

| File | Description |
|------|-------------|
| `agentruntime_controller.go` | Creates Pods with ImageVolume + ShareProcessNamespace + Per-Runtime hostPath |
| `aigent_controller.go` | Schedules AIAgents to AgentRuntimes |
| `harness_controller.go` | Manages Harness CRDs, generates harness.json ConfigMaps |

**AgentRuntime Controller Key Functions:**
- `resolveHarnessReferences`: Fetches Harness CRDs, generates ConfigMaps with harness.json
- `collectHarnessEnvVars`: Injects PROVIDER_API_KEY from Model Harness authSecretRef
- `buildPodSpec`: Creates Pod with per-runtime hostPath mount

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
| `harness_loader.go` | Harness config loading from ConfigMaps (harness.json) |
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

**Gateway Architecture (Per-Instance Isolation):**
- Each AIAgent → One Gateway process (tested: 2 agents → 2 gateway processes)
- Each Gateway gets isolated subdirectories:
  - `<workDir>/<instanceID>/config/` → openclaw.json
  - `<workDir>/<instanceID>/workspace/` → cron, tasks
  - `<workDir>/<instanceID>/state/` → plugins, registry, logs
- Handler copies base state from ImageVolume cache to instance state dir

**Gateway Startup Parameters:**
```bash
openclaw gateway \
  --allow-unconfigured \
  --bind loopback \
  --port <port>           # basePort + instanceIndex (18789, 18790...)
  --auth none \
  --force
```

**Environment Variables:**
- `OPENCLAW_CONFIG_DIR=<instanceConfigDir>`    # Per-instance config
- `OPENCLAW_WORKSPACE_DIR=<instanceWorkspaceDir>` # Per-instance workspace
- `OPENCLAW_STATE_DIR=<instanceStateDir>`      # Per-instance state/plugins
- `DISCORD_BOT_TOKEN=<token>`                  # Injected by Handler
- `TELEGRAM_BOT_TOKEN=<token>`                 # Injected by Handler
- `SLACK_BOT_TOKEN=<token>`                    # Injected by Handler

**Key Files:**
| File | Description |
|------|-------------|
| `handler.go` | Gateway process management, per-instance isolation, health monitoring |
| `converter.go` | Converts agentConfig to openclaw.json, channels config, models merge mode |
| `state.go` | CopyDir, CopyFile, RewriteRegistryPaths for state initialization |

**Per-Instance State Initialization:**
1. Handler caches base state from ImageVolume to `.openclaw-base/`
2. For each Gateway instance:
   - Copy `.openclaw-base/` to `<instanceID>/state/`
   - Rewrite registry paths (`/openclaw-state` → actual instance path)
   - Copy openclaw.json to both config/ and state/ directories

### `cmd/config-daemon/` - Config Daemon

**Per-Runtime Index Structure:**
```
/var/lib/aiagent/configs/
├── <namespace>/
│   ├── <runtime-name-1>/        # AgentRuntime specific directory
│   │   ├── agent-index.yaml     # Only agents bound to this runtime
│   │   ├── agent-1/
│   │   │   ├── agent-config.json # Resolved secrets
│   │   │   └── agent-meta.yaml
│   │   └── agent-2/
│   │       ├── agent-config.json
│   │       └── agent-meta.yaml
│   ├── <runtime-name-2>/        # Different AgentRuntime
│   │   ├── agent-index.yaml
│   │   └── agent-3/
│   └── all-agents.yaml          # Debug/debugging only (all agents in ns)
```

**resolveTokenSecretRefs Function:**
```go
// Framework-agnostic secret resolution
// Walks JSON tree, finds tokenSecretRef at any path
// Replaces with actual token from K8s Secret
func (d *ConfigDaemon) resolveTokenSecretRefs(data interface{}, namespace string) interface{} {
    switch v := data.(type) {
    case map[string]interface{}:
        if ref, ok := v["tokenSecretRef"].(string); ok {
            secret := d.k8sClient.CoreV1().Secrets(namespace).Get(ref)
            v["token"] = string(secret.Data["token"])
            delete(v, "tokenSecretRef")
        }
        for key, val := range v {
            v[key] = d.resolveTokenSecretRefs(val, namespace)
        }
    // ... handles arrays recursively
    }
}
```

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
| Agent configs (hostPath per-runtime) | `/etc/agent-config/` (mount at runtime-level) |
| Agent index per-runtime | `/etc/agent-config/agent-index.yaml` |
| Agent config per-agent | `/etc/agent-config/<agent-name>/agent-config.json` |
| Harness ConfigMaps | `/etc/harness/<harness-name>/` |
| Harness JSON | `/etc/harness/<harness-name>/harness.json` |
| Shared workspace | `/shared/workdir/` |
| Shared config | `/shared/config/` |
| Framework image | `/framework-rootfs/` |
| OpenClaw instance config | `<workDir>/<instanceID>/config/` |
| OpenClaw instance state | `<workDir>/<instanceID>/state/` |

## agentConfig vs Harness

| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| Positioning | Platform engineering capabilities | Agent/Handler/Framework config |
| Examples | Model, MCP, Sandbox, Skills | Channels, gateway, internal agents |
| Processing | Platform-level by Agent ID | Handler determines format |
| Responsibility | Platform manages | Handler processes |
| Secret Handling | Model API keys via authSecretRef | Channel tokens via tokenSecretRef |

## OpenClaw agentConfig Example (Discord Integration)

```yaml
agentConfig:
  gateway:
    port: 18789
    bind: "loopback"
  channels:
    discord:
      enabled: true
      tokenSecretRef: "discord-bot-token-secret"  # Config Daemon resolves
      allowFrom: ["user-id-1", "user-id-2"]
      dmPolicy: "open"
      mentionRequired: false
  agents:               # Internal sub-agents (invisible to Kubernetes)
    defaults:
      model: "deepseek/deepseek-chat"
    list:
      - id: weather
        name: "Weather Agent"
        skills: ["get_weather"]
      - id: assistant
        name: "General Assistant"
  models:
    mergeModels: true   # Merge harness providers with built-in providers
```

## Discord Integration Flow

```
1. User creates AIAgent CRD with:
   agentConfig.channels.discord.tokenSecretRef: "discord-secret"

2. Config Daemon resolves:
   - Reads K8s Secret "discord-secret"
   - Replaces tokenSecretRef with actual token in agent-config.json

3. OpenClaw Handler:
   - Reads agent-config.json from /etc/agent-config/<agent>/agent-config.json
   - Extracts channels.discord.token
   - Injects DISCORD_BOT_TOKEN=<token> when starting Gateway subprocess

4. Gateway Process:
   - Receives DISCORD_BOT_TOKEN in environment
   - Config has: token: {source: "env", id: "DISCORD_BOT_TOKEN"}
   - Loads discord plugin from state directory
   - Connects to Discord API
```

## Docker Images

| Dockerfile | Description |
|------------|-------------|
| `Dockerfile.manager` | Controller Manager (runs K8s controllers) |
| `Dockerfile.config-daemon` | Config Daemon (syncs configs, resolves secrets) |
| `Dockerfile.adk-framework` | ADK Framework (DUMMY, provides image for ImageVolume) |
| `Dockerfile.adk-handler` | ADK Handler (process manager) |
| `Dockerfile.openclaw-framework` | OpenClaw Framework (Node.js, provides image + openclaw-state) |
| `Dockerfile.openclaw-handler` | OpenClaw Handler (Gateway manager, per-instance isolation) |

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
7. **Per-Runtime Config Daemon**: Each AgentRuntime gets isolated hostPath directory
8. **Secret Resolution**: Config Daemon resolves tokenSecretRef (framework-agnostic)
9. **Per-Instance State**: OpenClaw Gateway instances get isolated config/workspace/state directories
10. **Shared Resources**: Handler and Framework share Pod quota, no individual container limits
11. **Channel Integration**: Channel tokens resolved by Config Daemon, injected as subprocess env vars by Handler