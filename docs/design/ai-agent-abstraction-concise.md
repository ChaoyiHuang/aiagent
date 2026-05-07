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
- **Agent Framework**: Agent framework like LangChain, ADK, Sematic Kernel, OpenClaw, Hermes which run AI agent

The AgentRuntime controller manages the lifecycles of both AgentRuntime and AIAgent CRDs, and is agent framework agnotic.

For optimal resource utilization, both the Agent Framework and Agent Handler are deployed as lightweight containers. The Agent Handler is managed by the container runtime, whereas the Agent Framework and individual AI Agents are instantiated by the Handler or via event-driven mechanisms.

### 3.1 Process Mapping Modes

**Mode A: Single Process Multiple Agents**

```
Pod
├── Agent Handler process
└── Agent Framework process
        ├── AIAgent-1
        ├── AIAgent-2
        └── AIAgent-3
```

- Suitable for frameworks supporting multi-agent instances

**Mode B: Multiple Processes Single Agent**
```
Pod
├── Agent Handler process
│   └── Starts multiple Agent Framework processes
├── Agent Framework process-1 ──► AIAgent-1
├── Agent Framework process-2 ──► AIAgent-2
└── Agent Framework process-3 ──► AIAgent-3
```

**Decision Authority**: Agent Handler chooses mode based on framework characteristics and business requirements—platform doesn't enforce.

### 3.2 Agent Framework Running Modes

| Mode | Description | Applicable Scenarios |
|------|-------------|---------------------|
| Long Running | Long-running service, continuously providing service capabilities | Agents that need continuous response to requests and state maintenance |
| Event-triggered | Event-triggered on-demand execution, terminates after task completion | Agents that execute specific tasks without continuous running needs |
| Server Mode | Listening on port, providing services externally | Agent as server, receiving external requests |
| Client Mode | Actively connects to external services, similar to chat client | Agent as client, connecting to platform services (such as OpenClaw, WeChat's weixin-claw) |

**Decision Authority**: Agent Handler selects appropriate mode based on framework characteristics

### 3.3 Pod Container Configuration

| Namespace | Decision | Reason |
|-----------|----------|--------|
| PID | Shared | Handler monitors/manages Framework processes |
| Network | Shared | Internal communication without cross-stack overhead |
| Filesystem | Isolated | Independent image release and upgrade |

- **TBD**: need to design a mechanism for Agent Handler to manage Agent Framework and AI Agents

### 3.4 Sandbox Integration

Two mutually exclusive modes via Harness reference:

| Mode | Description | Use Case |
|------|-------------|----------|
| External | Sandbox as independent Pod, shared pool | Multiple Agents share warm pool |
| Embedded | AgentRuntime Pod itself is Sandbox | Strong isolation required |

Uses agent-sandbox project CRDs: SandboxTemplate, SandboxWarmPool, SandboxClaim, Sandbox.

### 3.5 Agent identification management

Some Agent Frameworks generate internal UUIDs for logging and protocol interactions etc during runtime. The Agent Handler should establish a mapping between the CRD-defined Agent ID/Name and these framework-specific identifiers. This correlation ensures that all dimensions of an agent's information can be unified across the entire platform engineering ecosystem.

---

## 4. AIAgent Design

**Core Concept**: Independent business object, schedulable and migrateble across AgentRuntimes.

### 4.1 Decoupling Design

- N:1 relationship: Multiple AIAgents can run in one AgentRuntime
- Supports migration for: failure recovery, resource consolidation, maintenance
- PVC follows AIAgent migration (requires network storage: NFS, Ceph, Longhorn)

### 4.2 Scheduling Modes

| User Specification | Behavior |
|--------------------|----------|
| `runtimeRef.type: adk` | Auto-schedule to matching type |
| `runtimeRef.name: runtime-001` | Direct binding to instance |
| Not specified | Default scheduling policy |

### 4.3 agentConfig: Business Configuration Delivery

Platform defines delivery mechanism; AgentHandler determines content format.

| Dimension | Design |
|-----------|--------|
| File Source | Reference ConfigMap/Secret |
| Declaration | AgentRuntime declares public config; AIAgent appends specific config |
| Mount Path | `/etc/agent-config/runtime/` (public), `/etc/agent-config/agent/` (specific) |
| Update | Handler monitors file changes via fsnotify |
| Scope | Same Namespace only |

**Key Distinction from Harness**:
| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| **Positioning** | Platform engineering capabilities | Agent/Handler/Framework configuration information |
| **Examples** | GAIE Gateway, MCP Registry, AgentGateway, Sandbox, etc. | Prompt, protocol config (A2A), etc. |
| **Processing Method** | Platform-level processing based on Agent ID | Configuration content needed by Handler/Framework startup and runtime |
| **Responsibility** | Platform layer manages and provides | Handler determines format and usage |
| **Focus** | Capability externalization, standardization | Business configuration, framework-specific requirements |

---

## 5. Harness Design

**Core Concept**: Independent CRD for platform scaffolding capabilities, reused across AgentRuntimes/AIAgents.

**Agent Handler as the bridge**: Actually, many gateways and registries already exist. When an Agent Framework cannot talk to them directly, the Agent Handler steps in to interact with them via a consistent interface. It then transforms these harness capabilities into a format the Agent Framework can understand. For instance, it can download skills from a Skill Hub for Agent Frameworks that only handle local configurations, effectively turning the Skill Hub into a universal provisioning center. **The Agent Handler essentially serves as a mediator between standardized resources and the specific needs of different agent frameworks**.

### 5.1 Standard Capabilities (Extensible)

| Type | Description |
|------|-------------|
| model | LLM model integration |
| mcp | Model Context Protocol / tool registry |
| memory | Memory storage |
| sandbox | Execution isolation |
| skills | Skill modules |
| knowledge | Knowledge base / RAG |
| guardrail, security, policy | Safety and policy control |

### 5.2 Binding Mode

**AgentRuntime Reference** (public capabilities):
```yaml
harness:
  model: [name: model-deepseek-default]
  mcp: [name: mcp-registry-default]
  sandbox: [name: gvisor-sandbox]
```

**AIAgent Customization** (override only, no append):
```yaml
harnessOverride:
  mcp:
    - name: mcp-registry-default
      allowedServers: [github, browser]
      deniedServers: [filesystem]
```

**Security Constraint**: AIAgent can only override/deny capabilities provided by Runtime—cannot add new ones. This prevents Agents from arbitrarily extending capabilities beyond security boundaries.

### 5.3 Configuration Delivery

Shared Volume mount at `/etc/harness/`, AgentHandler monitors via fsnotify. No K8s API access required for AgentHandler.

---

## 6. Summary

This design makes AI Agent a first-class Kubernetes citizen through:

1. **Agent Handler Pattern**: Decouples platform (Controller) from framework (Handler)
2. **AIAgent/Runtime Separation**: Enables dynamic scheduling and migration
3. **Flexible Process Modes**: Handler decides single/multi-process architecture
4. **Harness Standardization**: Platform capabilities externalized, inheritance+override customization
5. **agentConfig Abstraction**: Business config separated from platform capabilities
6. **Sandbox Integration**: Reuses agent-sandbox for execution isolation

