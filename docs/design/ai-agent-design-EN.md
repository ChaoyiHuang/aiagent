# AI Agent Architecture Design Document

## 1. Overview

### 1.1 Purpose

Currently, the Kubernetes ecosystem lacks a core abstraction for AI Agents. This design aims to define a core resource similar to Pod that can uniformly abstract any Agent framework (such as LangChain, ADK, OpenClaw, CrewAI, Hermes, etc.), while externalizing various scaffolding capabilities (such as CLI Tools, MCP, Skills, Knowledge/RAG, Memory, State, Guardrail, Security, Policy, Gateway, Sandbox, etc.), which can be connected through the AI Agent ID.

### 1.2 Core Objectives

- **Framework Independence**: Support any Agent framework without requiring the platform layer to develop a separate Controller for each framework
- **Externalized Capabilities**: Scaffolding capabilities managed independently, reusable and customizable
- **Flexible Scheduling**: AI Agents can dynamically migrate to different runtime environments
- **Multi-tenancy Support**: Namespace-level resource isolation
- **Security Isolation**: Support multiple forms of Sandbox execution environments

---

## 2. Core Object Definitions

### 2.1 Architecture Layers

This design abstracts AI Agent into three core objects:

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
│    - Handler + Framework containers  │
│    - Binds public Harness configs    │
│    - 1:1 mapping to Pod              │
└─────────────────────────────────────┘
              │
              │ Reference
              ▼
┌─────────────────────────────────────┐
│         Harness (Scaffolding)        │
│    - Namespace-level independent CRD │
│    - MCP, Memory, Sandbox, etc.      │
└─────────────────────────────────────┘
```

**Analogy Relationship**:

| Object | Similar to K8s | Description |
|--------|-----------------|-------------|
| AgentRuntime | Node | Runtime carrier, hosts Agent execution |
| AIAgent | Pod | Schedulable workload |
| Harness | ConfigMap/Secret | External capability configuration |

---

## 3. AgentRuntime Design

### 3.1 Object Definition

AgentRuntime is a merged object of Handler and Agent Framework, corresponding to a Pod instance.

#### 3.1.1 Design Considerations

**Question**: How to avoid developing a separate Controller for each Agent framework?

**Decision**: Adopt the Agent Handler pattern.

- **Platform Layer Controller**: Uniformly manages AgentRuntime and AIAgent CRD lifecycles, does not perceive framework details
- **Agent Handler**: Provided by the framework community, responsible for specific framework startup, configuration conversion, and Agent management

**Advantages**:
- Only one Controller needed, platform layer responsibilities are clear
- Handlers can be provided by framework ecosystem, decoupling development responsibilities
- New framework integration only requires providing a Handler image, no platform code modification needed

#### 3.1.2 Pod Container Configuration

**Shared Namespace Decisions**:

| Dimension | Decision | Considerations |
|-----------|----------|----------------|
| Process Namespace | Shared | Handler needs to monitor Framework process; shared PID namespace reduces isolation overhead |
| Network Namespace | Shared | Handler and Framework communication doesn't need cross-network stack, minimal overhead |
| File System | Isolated | Handler and Framework images are released independently, need independent file spaces |

**Considerations**:
- Handler and Framework are tightly coupled process relationships, no strong security isolation requirement
- Lightweight container isolation, reduce overhead
- Requirement for independent image release and upgrade

#### 3.1.3 Container Merge Strategy (Embedded Sandbox)

When using embedded Sandbox mode, SandboxTemplate defines the Pod base specification, and AgentRuntime's Handler and Framework containers use **append mode** for merging.

**Considerations**:
- SandboxTemplate may already contain code execution containers, monitoring sidecars, etc.
- AgentRuntime's Handler and Framework are business core containers
- Append mode preserves SandboxTemplate integrity while overlaying Agent containers

#### 3.1.4 CRD Structure Example

```yaml
apiVersion: ai.k8s.io/v1
kind: AgentRuntime
metadata:
  name: runtime-001
  namespace: tenant-a
spec:
  handler:
    image: adk-handler:v1.2.0
    resources:
      limits:
        cpu: "500m"
        memory: "512Mi"
  framework:
    image: adk-runtime:v1.2.0
    resources:
      limits:
        cpu: "1000m"
        memory: "1Gi"

  harness:
    mcp:
      - name: filesystem-mcp
      - name: github-mcp
    memory:
      - name: redis-memory
    sandbox:
      - name: gvisor-sandbox
    knowledge:
      - name: custom-rag

  sandboxTemplateRef: secure-template  # Used in embedded Sandbox mode

  replicas: 1

  nodeSelector:
    node-type: agent-node
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: zone
                operator: In
                values: ["zone-a", "zone-b"]

status:
  phase: Running
  podIPs:
    - "10.0.0.1"
  readyReplicas: 1
```

---

## 4. AIAgent Design

### 4.1 Object Definition

AIAgent is an independent business object that can be scheduled to different AgentRuntimes for execution.

#### 4.1.1 Design Considerations

**Question**: Why separate AIAgent from AgentRuntime?

**Decision**: Decoupled design, supports dynamic scheduling and migration.

**Considerations**:
- One AgentRuntime can host multiple AIAgents (N:1 relationship)
- AIAgent may need to migrate to different Runtime (failure, resource adjustment, maintenance)
- PVC data needs to follow AIAgent migration

#### 4.1.2 Agent ID Design

| Field | Source | Purpose |
|-------|--------|---------|
| `metadata.name` | User specified | Unique within Namespace, human readable |
| `metadata.uid` | K8s auto-generated | Absolutely unique identifier |

**Considerations**:
- Follow K8s conventions, familiar to users
- name facilitates human recognition and kubectl operations
- uid for absolutely unique identifier, Handler can choose which to use

#### 4.1.3 Scheduling Mode

Adopt hybrid scheduling mode:

| User Specification | Controller Behavior |
|--------------------|---------------------|
| `runtimeRef.type: adk` | Automatically schedule to matching type Runtime |
| `runtimeRef.name: runtime-001` | Directly bind to specified instance |
| Not specified | Default scheduling policy |

**Considerations**:
- Flexibility: Users can choose automatic scheduling or manual binding
- Consistent with K8s scheduling mode (similar to Pod scheduling to Node)

#### 4.1.4 Migration Support

| Migration Type | Trigger Condition |
|----------------|-------------------|
| Active Migration | User modifies `runtimeRef.name` |
| Automatic Migration | Runtime failure, resource shortage, maintenance eviction |

**Considerations**:
- Support operations (automatic migration during maintenance)
- Support user active adjustment (business requirement changes)

#### 4.1.5 PVC Migration

During migration, PVC follows AIAgent, unbinds from old Pod and mounts to new Pod.

**Technical Constraints**:
- Requires network storage support (such as NFS, Ceph, Longhorn)
- Cloud storage (EBS, PD) requires detach/attach, has delay

**Considerations**:
- Data consistency: PVC follows Agent, ensures data not lost
- Storage backend selection: Users need to choose appropriate storage based on migration requirements

#### 4.1.6 CRD Structure Example

```yaml
apiVersion: ai.k8s.io/v1
kind: AIAgent
metadata:
  name: agent-001
  namespace: tenant-a
spec:
  runtimeRef:
    type: adk              # Specify type, automatic scheduling
    # or name: runtime-001  # Specify instance, fixed binding

  harnessOverride:
    mcp:
      - name: filesystem-mcp
        config:
          readOnly: true
      - deny: [github-mcp]
    memory:
      - name: redis-memory
        config:
          ttl: 3600

  volumePolicy: retain     # PVC lifecycle policy: retain | delete

  description: "Data Analysis Agent"

status:
  phase: Running
  runtimeRef:
    name: runtime-001      # Currently bound Runtime
  conditions:
    - type: Ready
      status: "True"
```

---

## 5. Harness Design

### 5.1 Object Definition

Harness is an independent CRD for AI Agent scaffolding capabilities, defining configurations for various external capabilities.

#### 5.1.1 Design Considerations

**Question**: Why define capabilities as independent CRD instead of embedded configuration?

**Decision**: Independent CRD, facilitates reuse and unified management.

**Considerations**:
- Multiple AgentRuntimes/AIAgents can reference the same Harness
- Capability configuration managed independently, modification doesn't require changing Agent CRD
- Handler needs standardized capability definitions to adapt different frameworks

#### 5.1.2 Scope Decision

Adopt Namespace level, no cluster-level sharing support.

**Considerations**:
- Multi-tenant isolation: Different Namespace configurations are independent
- Permission control: Namespace-level RBAC
- Follow K8s conventions (like Role vs ClusterRole)

#### 5.1.3 Single Type Constraint

One Harness CRD can only configure a single capability type, identified by the `type` field.

**Considerations**:
- Simple management: Each Harness has single responsibility
- Easy reference: AgentRuntime references by type grouping
- Follow K8s single responsibility principle

#### 5.1.4 Standard Type List

Currently supported capability types (extensible later):

| Type | Description |
|------|-------------|
| mcp | Model Context Protocol, tool/capability integration |
| skills | Skill modules |
| cli-tools | Command line tools |
| knowledge | Knowledge base/RAG |
| memory | Memory storage |
| state | Runtime state |
| guardrail | Safety guardrail |
| security | Security policy |
| policy | Policy control |
| gateway | API gateway |
| sandbox | Execution isolation environment |

#### 5.1.5 Spec Structure

Adopt design where each type has independent spec field:

```yaml
spec:
  type: mcp
  mcp:                      # Corresponding type's spec field
    provider: filesystem
    tools:
      - read
      - write
```

**Considerations**:
- Clear structure, type corresponds to configuration
- Easy validation and type checking
- Different types have different schema constraints

#### 5.1.6 Binding Mode

AgentRuntime references Harness using type grouping:

```yaml
harness:
  mcp:
    - name: filesystem-mcp
    - name: github-mcp
  memory:
    - name: redis-memory
```

AIAgent customization adopts reference + configuration override mode:

```yaml
harnessOverride:
  mcp:
    - name: filesystem-mcp
        config:
          readOnly: true
    - deny: [github-mcp]
```

**Considerations**:
- Type grouping facilitates managing same-category capabilities
- Override mode supports inheritance + override, matches user intuition
- deny supports disabling public capabilities, flexible control

#### 5.1.7 Configuration Priority

When conflict occurs, AIAgent customization configuration takes precedence (premise: capability is available and implementable).

**Considerations**:
- Agent business needs priority
- Avoid Runtime configuration limiting Agent flexibility

#### 5.1.8 Capability Validation

Controller validates capability availability when creating AIAgent, rejects creation if unavailable.

**Considerations**:
- Detect problems early, avoid runtime failures
- Reduce Handler runtime validation burden

#### 5.1.9 No Append Constraint

AIAgent cannot append Harness not provided by AgentRuntime, can only override or deny.

**Considerations**:
- Security control: Runtime administrator can limit available capability scope
- Avoid Agent arbitrarily extending capabilities, breaking security boundary

#### 5.1.10 CRD Structure Examples

```yaml
# MCP type
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: filesystem-mcp
  namespace: tenant-a
spec:
  type: mcp
  mcp:
    provider: filesystem
    tools:
      - read
      - write
    config:
      rootPath: /data

---
# Memory type
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: redis-memory
  namespace: tenant-a
spec:
  type: memory
  memory:
    backend: redis
    config:
      host: redis-server
      port: 6379
      ttl: 7200

---
# Sandbox type (external mode)
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: external-sandbox
  namespace: tenant-a
spec:
  type: sandbox
  sandbox:
    external:
      sandboxClaimRef: my-sandbox-claim

---
# Sandbox type (embedded mode)
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: embedded-sandbox
  namespace: tenant-a
spec:
  type: sandbox
  sandbox:
    embedded:
      sandboxTemplateRef: secure-template
```

---

## 6. Harness Configuration Delivery Mechanism

### 6.1 Shared Volume Mount Solution

Adopt shared Volume mounting ConfigMap to deliver Harness configuration to Handler.

```
Pod
├── Handler container
│   └── /etc/harness/ (ConfigMap mount)
└── Framework container
│   └── /etc/harness/ (Same Volume)
```

#### 6.1.1 Design Considerations

**Question**: How does Handler obtain Harness configuration?

**Decision**: Shared Volume mount ConfigMap, YAML format.

**Considerations**:

| Solution | Advantages | Disadvantages |
|----------|------------|---------------|
| Handler accesses K8s API | Real-time change perception | Requires RBAC permissions, increases complexity |
| Dynamic API push | Real-time update | Handler needs to expose API, increases complexity |
| Shared Volume mount | Simple, no permissions needed | ConfigMap update has delay (~1 minute) |

Reasons for choosing shared Volume mount:
- Handler doesn't need K8s access permissions, reduces security risk
- Configuration changes don't require Pod restart
- Simple and reliable, follows Sidecar conventions (like Fluent Bit)

#### 6.1.2 ConfigMap Size Constraint

ConfigMap has 1MB size limit.

**Considerations**:
- Harness configuration mainly connection parameters, access methods, small volume
- Single Runtime configuration estimated tens of KB to hundreds of KB
- 1MB sufficient, no need to break limit

#### 6.1.3 File Watch Mechanism

Handler can monitor configuration file changes through polling or fsnotify:

```go
// fsnotify monitoring
watcher, _ := fsnotify.NewWatcher()
watcher.Add("/etc/harness/")
for event := range watcher.Events {
    if event.Op == fsnotify.Write {
        reloadConfig()
    }
}
```

---

## 7. Sandbox Integration Design

### 7.1 Integration with agent-sandbox Project

Utilize the existing Sandbox CRD resource system from agent-sandbox project:

| CRD | Purpose |
|-----|---------|
| SandboxTemplate | Sandbox template, defines Pod spec, network policy, security policy |
| SandboxWarmPool | Warm pool, maintains pre-warmed Sandbox instances |
| SandboxClaim | Sandbox claim, obtains Sandbox instance |
| Sandbox | Sandbox instance (Pod) |

### 7.2 Sandbox Modes

Reference existing Sandbox/SandboxClaim through Harness, support two mutually exclusive modes:

| Mode | Description | Applicable Scenarios |
|------|-------------|---------------------|
| External Mode | Sandbox as independent Pod, AgentRuntime calls via API | Multiple Agents share Sandbox resource pool |
| Embedded Mode | AgentRuntime Pod itself is Sandbox | Agent needs strongly isolated execution environment |

#### 7.2.1 Design Considerations

**Question**: Why support two modes?

**Considerations**:
- External Mode: Sandbox can be resource pool, dynamic scheduling, multiple Agents share
- Embedded Mode: Agent tightly coupled with Sandbox, execution environment more controllable
- Mutually exclusive design: One Harness can only choose one mode, avoid configuration confusion

### 7.3 Sandbox Resource Pool Dynamic Association

SandboxWarmPool maintains pre-warmed instances, SandboxClaim can choose to obtain from pool or create new.

```
SandboxWarmPool ──► Pre-warmed Sandbox instances
        │
        ▼
SandboxClaim ──► Obtain from pool or create new
        │
        ▼
Harness reference ──► AIAgent/AgentRuntime use
```

**Considerations**:
- Warm pool reduces startup latency
- Dynamic association supports on-demand scheduling

---

## 8. Design Objectives Achievement Analysis

### 8.1 Framework Independence

**Achievement Method**:
- Unified AgentRuntime Controller, doesn't perceive framework details
- Handler provided by framework community, responsible for framework adaptation
- Standardized Harness definition, Handler uniformly converts

**Effect**: New framework integration only requires providing Handler image, no platform code modification needed.

### 8.2 Externalized Capabilities

**Achievement Method**:
- Harness independent CRD, reusable
- AgentRuntime references public Harness
- AIAgent customization override, no append

**Effect**: Capability configuration managed independently, modification doesn't require changing Agent CRD, multiple Agents can reuse same Harness.

### 8.3 Flexible Scheduling

**Achievement Method**:
- AIAgent and AgentRuntime decoupling
- Hybrid scheduling mode (type automatic scheduling, instance fixed binding)
- PVC follows migration

**Effect**: Agent can dynamically migrate to different Runtime, supports operations maintenance and load balancing.

### 8.4 Multi-tenancy Support

**Achievement Method**:
- Namespace-level Harness
- PVC independent by AIAgent
- Sandbox isolation policy

**Effect**: Different Namespace configurations independent, data isolated, security boundary clear.

### 8.5 Security Isolation

**Achievement Method**:
- Sandbox two modes (external/embedded)
- Integration with agent-sandbox project security policy
- Harness no append constraint

**Effect**: Runtime administrator can limit capability scope, Agent execution environment can be isolated.

---

## 9. Summary

This design achieves the core resource definition for AI Agent in Kubernetes through three-layer object abstraction (AIAgent, AgentRuntime, Harness). Core innovations include:

1. **Handler Pattern**: Platform layer Controller uniformly abstracts, framework layer Handler specifically adapts, decouples development responsibilities
2. **Agent and Runtime Separation**: Supports dynamic scheduling and migration, analogous to Pod and Node
3. **Harness Standardization**: External capabilities managed independently, inheritance + override mode customization
4. **Sandbox Integration**: Reuses agent-sandbox project, supports multiple execution environment forms

Through this design, AI Agent becomes a first-class citizen in Kubernetes, a core abstraction similar to Pod, capable of adapting to any Agent framework, supporting complex business scenarios, while maintaining security isolation and multi-tenancy capabilities.