# AI Agent Abstraction in Kubernetes - Concise Version

## 1. Core Problem and Design Purpose

**Resource Efficiency Challenge**: AI Agents exhibit long idle periods, bursty task execution, and varying resource demands. Traditional 1 Agent = 1 Pod approach wastes resources.

**Design Purpose**: Define a core Kubernetes resource (like Pod) that abstracts ANY Agent framework (LangChain, ADK, OpenClaw, Hermes, etc.), externalizes platform capabilities, and enables fine-grained resource optimization at individual Agent level.

---

## 2. Three-Layer Architecture

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

---

## 3. AgentRuntime Design

AgentRuntime = Agent Handler + Agent Framework, corresponding to one Pod.

- **Agent Handler**: Provided by the agent framework community, responsible for specific framework startup, configuration conversion, and AI Agent lifecycle.
- **Agent Framework**: Agent framework like LangChain, ADK-Go, OpenClaw, Hermes which run AI agent

### 3.1 Process Mapping Modes (Verified in E2E Tests)

| Mode | Description | Framework Support |
|------|-------------|-------------------|
| **Shared** | Single Framework process, multiple Agents | ADK-Go (tested) |
| **Isolated** | One Framework process per Agent | ADK-Go, OpenClaw (tested) |

**Test Results** (E2E verified):
- ADK Shared Mode: 2 AIAgents → 1 Framework process ✓
- ADK Isolated Mode: 3 AIAgents → 3 Framework processes ✓
- OpenClaw Gateway Mode: 2 AIAgents → 2 Gateway processes ✓

### 3.2 Pod Architecture (ImageVolume Pattern)

```
Pod (AgentRuntime)
├── Handler Container (process manager)
│   ├── Starts Framework processes via exec.Command
│   ├── Controls process lifecycle (start/stop/monitor)
│   └── VolumeMounts:
│       ├── /framework-rootfs -> ImageVolume (Framework image)
│       ├── /etc/harness/<name> -> Harness ConfigMaps
│       ├── /shared/workdir -> EmptyDir (agent workspace)
│       └── /etc/agent-config -> hostPath (Config Daemon)
│
└── Framework Container (DUMMY - provides image content only)
│   └── ENTRYPOINT: sleep infinity
│   └── Provides image content for ImageVolume
│
└── ShareProcessNamespace: true (Handler manages Framework processes)
```

**ImageVolume Benefits** (K8s 1.35+):
- Handler accesses Framework's complete filesystem
- Independent image releases (Handler/Framework decoupled)
- No binary copying or init containers needed

### 3.3 Pod Container Configuration

| Namespace | Decision | Reason |
|-----------|----------|--------|
| PID | Shared | Handler monitors/manages Framework processes |
| Network | Shared | Internal communication without cross-stack overhead |
| Filesystem | Isolated | Independent image release and upgrade |

---

## 4. AIAgent Design

**Core Concept**: Independent business object, schedulable and migrateble across AgentRuntimes.

### 4.1 Scheduling Modes

| User Specification | Behavior |
|--------------------|----------|
| `runtimeRef.type: adk` | Auto-schedule to matching type |
| `runtimeRef.name: runtime-001` | Direct binding to instance |

### 4.2 agentConfig: Business Configuration Delivery

Platform defines delivery mechanism; AgentHandler determines content format.

| Dimension | Design |
|-----------|--------|
| File Source | hostPath via Config Daemon (Solution M) |
| Declaration | AgentRuntime declares public config; AIAgent appends specific config |
| Mount Path | `/etc/agent-config/runtime/` (public), `/etc/agent-config/agent/<name>/` (specific) |
| Update | Config Daemon watches CRDs, Handler monitors file changes |
| Scope | Same Namespace only |

**Config Daemon Architecture**:
```
Config Daemon (DaemonSet)
├── Watches AIAgent CRDs via Informer
├── Writes to hostPath: /var/lib/aiagent/configs/<namespace>/<agent-name>/
├── Creates agent-index.yaml in namespace directory
│
Pod (AgentRuntime)
├── Mounts hostPath as /etc/agent-config
└── Handler reads agent-index.yaml to discover agents
```

**Key Distinction from Harness**:
| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| **Positioning** | Platform engineering capabilities | Agent/Handler/Framework configuration |
| **Examples** | Model, MCP, Sandbox, Skills | Prompt, protocol config |
| **Processing** | Platform-level by Agent ID | Handler determines format |
| **Responsibility** | Platform manages | Handler processes |

---

## 5. Harness Design

**Core Concept**: Independent CRD for platform scaffolding capabilities, reused across AgentRuntimes/AIAgents.

### 5.1 Standard Capabilities (Extensible)

| Type | Description |
|------|-------------|
| model | LLM model integration (DeepSeek, OpenAI, Gemini) |
| mcp | Model Context Protocol / tool registry |
| memory | Memory storage (inmemory, redis, file) |
| sandbox | Execution isolation (gvisor, docker, kata) |
| skills | Skill modules |

### 5.2 Binding Mode

**AgentRuntime Reference** (public capabilities):
```yaml
harness:
  - name: model-deepseek-default
  - name: mcp-registry-default
```

**AIAgent Customization** (override only, no append):
```yaml
harnessOverride:
  model:
    - name: model-deepseek-default
      allowedModels: [deepseek-chat]
```

**Security Constraint**: AIAgent can only override/deny capabilities provided by Runtime—cannot add new ones.

---

## 6. ADK-Go Integration (Verified)

The adk-framework now integrates with adk-go library for real agent execution:

```
adk-framework
├── Imports google.golang.org/adk (local replace)
├── Uses llmagent.New() for agent creation
├── Uses runner.Runner for agent execution
├── Uses session.InMemoryService() for session management
│
└── JSON-RPC Methods:
    ├── agent.run - Execute agent with user message
    ├── agent.status - Query agent status
    ├── agent.list - List all agents
    ├── framework.status - Framework health
```

**Custom Model Support**: Handler can use OpenAI-compatible APIs (DeepSeek) via custom model implementation.

---

## 7. Summary

This design makes AI Agent a first-class Kubernetes citizen through:

1. **Agent Handler Pattern**: Decouples platform (Controller) from framework (Handler)
2. **AIAgent/Runtime Separation**: Enables dynamic scheduling and migration
3. **Flexible Process Modes**: Handler decides shared/isolated architecture (verified)
4. **Harness Standardization**: Platform capabilities externalized, inheritance+override customization
5. **agentConfig Abstraction**: Business config separated from platform capabilities
6. **ImageVolume Pattern**: K8s 1.35+ feature for Handler-to-Framework filesystem access
7. **Config Daemon**: Solution M for agent config distribution without Pod K8s API access

**E2E Test Results**: All 3 modes (ADK Shared, ADK Isolated, OpenClaw Gateway) verified ✓