# AI Agent abstraction in Kubernetes

## 1. Overview

### 1.1 Scenarios and Challenges

#### 1.1.1 Typical Scenario

WeChat is a social software in China with over 1.2 billion daily active users. WeChat recently opened a weixin-claw plugin for AI Agent like Openclaw or any other AI Agents to be accessible in WeChat. Any WeChat user can scan a QR code to add an AI Agent as a friend and chat with it. AI Agents connect to WeChat's AI Agent Gateway through WeChat's provided plugin in client mode.

Alice runs a one-person AI Agent startup and developed a life assistant AI Agent. She found that if she independently develops a mobile app, she would incur expensive promotion costs and need to handle complex web service security governance and operations. Therefore, connecting through WeChat is the lowest-cost way to reach billion-level users.

Now her life assistant AI Agent is developed, and she needs to consider many post-launch issues: renting virtual machines from public cloud to host these AI Agents is also a significant cost. One AI Agent exclusively occupying a VM or exclusively occupying a Pod/Sandbox is not an economical solution. If AI Agents grow to millions or tens of millions in a short time, the unutilized resource overhead is huge. Therefore, running many AI Agents in a single process is a reasonable solution choice. Separating AI Agent and Sandbox, each maximizing resource reuse, becomes her choice.

During business operation, she quickly discovered that many users try for one or two days and then become inactive, not knowing when they will resume activity, yet cannot be deleted; meanwhile, even for active users, the active duration of AI Agent and Sandbox varies greatly. To save operational costs, she needs to use minimal public cloud rental costs, dynamically consolidate AI Agents to maintain the minimum number of processes/Pods/Sandboxes, while dynamically scaling up to meet suddenly growth bursts, and dynamically scaling down to address business vertical decline demands. In any situation, only maintain the minimum resources truly needed for current business.

As a one-person AI Agent startup, she needs AI Agent granularity platform engineering to help her.

#### 1.1.2 AI Agent Resource Utilization Efficiency Problem

AI Agents have some new resource usage characteristics:

| Characteristic | Description |
|----------------|-------------|
| Long idle time | Agent mostly idle, waiting for task trigger |
| Task burstiness | Resource usage spikes when task arrives, drops quickly after completion |
| Task duration variance | Short tasks (seconds to minutes) and long tasks (hours) coexist |
| Resource demand fluctuation | Different tasks have varying CPU, memory, network demands |

When AI Agent executes tasks, it may execute tools, generate code and run it. Due to security reasons, AI Agent and execution environment(sandbox) have diverse considerations: AI Agent merged with execution environment, AI Agent separated from execution environment.

When Kubernetes cluster runs large-scale number (and different types) of AI Agents, how to effectively improve cluster resource utilization efficiency is a common problem. To effectively utilize resources, being able to identify and handle load at AI Agent granularity is very important.

#### 1.1.3 AI Agent Technology Rapid Iteration, Platform Engineering Cannot Keep Up with AI Agent Framework Development

From early Langchain, to Manus, to coding agent, then to OpenClaw, Hermes, each iteration brings technology framework evolution. CNCF/Kubernetes platform engineering, observability, governance, security, policy, traffic, etc., is still traditional platform engineering built on Pod, microservices, service mesh, Serverless foundations. To solve the requirements in 1.0.1, need to solve AI Agent granularity perception problem.

### 1.2 Design Purpose

Currently, the Kubernetes ecosystem lacks a core abstraction for AI Agents. This design aims to define a core resource similar to Pod that can uniformly abstract any existing Agent framework (such as LangChain, Sematic Kernel, OpenClaw, Hermes, etc.) and future unknown Agent frameworks, while externalizing various scaffolding capabilities (such as Model, MCP, Skills, Knowledge/RAG, Memory, State, Guardrail, Security, Policy, Gateway, Sandbox, etc.), which can be connected through the AI Agent ID/Name.

To enhance resource utilization，AI agent abstraction should be able to support feature implementation like AI Agent bin pack conslidation, AI Agent migration, pod/node scale up/scale down, pod resize, sandbox reuse/hibernate/resize etc.

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

AgentRuntime is the merged object of Agent Handler and Agent Framework, corresponding to a Pod instance. AgentRuntime and AIAgent CRD lifecycles Uniformly managed by AgentRuntime Controller, which is provided by platform.

### 3.1 Object Definition and Design Considerations

- **Agent Handler**: Provided by the framework community, responsible for specific framework startup, configuration conversion, and AI Agent lifecycle. 
- **Agent Framework**: Agent framework like LangChain, Sematic Kernel, OpenClaw, Hermes which run AI agent  

**Advantages**:
- Only one Controller needed, platform layer responsibilities are clear
- Agent Handlers can be provided by framework ecosystem, decoupling development responsibilities
- New framework integration only requires providing an Agent Handler image, no platform code modification needed

### 3.2 Agent Handler and Agent Framework Process Mapping Modes

**Key Design Consideration**: The Agent Handler and Agent Framework processes in AgentRuntime can have multiple mapping relationships, decided by the Agent Handler itself.

**Mode A: Single Process Multiple Agents Mode**

```
Pod
├── Agent Handler container
└── Agent Framework container
    └── One Agent Framework process
        ├── AIAgent-1
        ├── AIAgent-2
        └── AIAgent-3
```

- One Agent Handler and one Agent Framework process
- One Agent Framework process runs multiple AI Agents internally
- Agent Framework process implements internal Agent routing and isolation

**Considerations**:
- Suitable for frameworks that natively support multi-Agent scenarios (such as CrewAI, ADK multi-Agent)
- High resource efficiency, reduces process overhead
- Agent Framework process responsible for internal Agent state isolation

**Mode B: Multiple Processes Single Agent Mode**

```
Pod
├── Agent Handler container
│   └── Starts multiple Agent Framework processes
├── Agent Framework process-1 ──► AIAgent-1
├── Agent Framework process-2 ──► AIAgent-2
└── Agent Framework process-3 ──► AIAgent-3
```

- One Agent Handler starts multiple Agent Framework processes
- Each Agent Framework process corresponds to one AI Agent
- Process-level isolation, each Agent runs independently

**Considerations**:
- Suitable for scenarios requiring strong isolation
- Single Agent failure doesn't affect other Agents
- Higher resource overhead, but stronger isolation

**Decision Basis**: Which mode to adopt is decided by the Agent Handler based on framework characteristics, business requirements, and resource conditions. The platform layer doesn't enforce constraints, only provides infrastructure support (such as shared PID namespace for Handler to manage multiple processes).

### 3.3 Agent Framework Running Modes

Agent Framework supports multiple running modes, decided by Agent Handler based on framework characteristics and business requirements.

#### 3.3.1 Lifecycle Modes

| Mode | Description | Applicable Scenarios |
|------|-------------|---------------------|
| Long Running | Long-running service, continuously providing service capabilities | Agents that need continuous response to requests and state maintenance |
| Event-triggered | Event-triggered on-demand execution, terminates after task completion | Agents that execute specific tasks without continuous running needs |

**Considerations**:
- Long Running mode suits scenarios requiring continuous service, such as chat services, monitoring alert Agents
- Event-triggered mode suits one-time tasks, such as data processing, report generation
- Platform layer supports different lifecycle modes through replicas and lifecycle policies

#### 3.3.2 Communication Modes

| Mode | Description | Applicable Scenarios |
|------|-------------|---------------------|
| Server Mode | Listening on port, providing services externally | Agent as server, receiving external requests |
| Client Mode | Actively connects to external services, similar to chat client | Agent as client, connecting to platform services (such as OpenClaw, WeChat's weixin-claw) |

**Server Mode Example**:

```
External Request ──► AgentRuntime Pod
                        │
                        ▼
                Agent Framework
                (Listening on port 8080)
                        │
                        ▼
                AIAgent processes request
```

**Client Mode Example**:

```
AgentRuntime Pod
│
└── Agent Framework
    └── AIAgent (OpenClaw / weixin-claw) ──► Connects to external platform service
                                              │
                                              ▼
                                        WhatsApp / Discord / WeChat...
```

**Considerations**:
- Server mode suits scenarios where Agent needs to expose API for external invocation
- Client mode suits scenarios where Agent needs to connect to existing platform services, such as WeChat bot, DingTalk assistant
- Agent Handler selects appropriate communication mode based on framework characteristics
- Platform layer provides network configuration support, but doesn't enforce communication methods

### 3.4 Resource Efficiency Considerations

AgentRuntime design considers the resource utilization efficiency of AI Agents.

#### 3.4.1 Resource Usage Characteristics

Most AI Agents have the following resource usage characteristics:

| Characteristic | Description |
|----------------|-------------|
| Long idle time | Agent mostly idle, waiting for task trigger |
| Task burstiness | Resource usage spikes when task arrives, drops quickly after completion |
| Task duration variance | Short tasks (seconds to minutes) and long tasks (hours) coexist |
| Resource demand fluctuation | Different tasks have varying CPU, memory, network demands |

**Considerations**:
- Traditional one-to-one deployment (one Agent per Pod) causes resource waste
- Idle Agents occupy Pod resources without actual work
- Resource shortage during task bursts, requiring elastic scaling

#### 3.4.2 Resource Sharing Strategies

AgentRuntime supports two multi-AI Agent modes, both achieving resource utilization efficiency improvement:

**Mode 1: Single Agent Framework Multiple AI Agents**

```
AgentRuntime Pod
│
├── Agent Handler (Lightweight, minimal resource usage)
│
└── Agent Framework (Single process)
    ├── AIAgent-1 (Idle)
    ├── AIAgent-2 (Executing task)
    └── AIAgent-3 (Idle)
```

**Resource Efficiency Advantages**:
- One Agent Framework process hosts multiple AI Agents
- Process-level resource sharing: memory, network connections, runtime environment
- Idle Agents only occupy Framework internal state, no extra process overhead
- Framework internally implements Agent scheduling and resource allocation

**Applicable Scenarios**:
- Agent framework natively supports single-process multi-Agent
- Resource efficiency priority, lower isolation requirements

**Mode 2: Multi Agent Framework Processes Multiple AI Agents**

```
AgentRuntime Pod
│
├── Agent Handler (Lightweight management process)
│   ├── Starts Agent Framework process-1
│   ├── Starts Agent Framework process-2
│   └── Starts Agent Framework process-3
│
├── Agent Framework process-1 ──► AIAgent-1
├── Agent Framework process-2 ──► AIAgent-2
└── Agent Framework process-3 ──► AIAgent-3
```

**Resource Efficiency Advantages**:
- Shared Pod infrastructure: shared network namespace, shared PID namespace
- Shared Pod resource quota: multiple Agents share same Pod's CPU/memory quota
- Idle Agent Framework process low resource occupation, quick activation
- Avoids traditional mode scheduling overhead of independent Pod per Agent

**Applicable Scenarios**:
- Need process-level isolation for Agents
- Agents run independently, no collaboration needs
- Single Agent failure doesn't affect other Agents

**Comparison Analysis**:

| Dimension | Single Framework Multi-Agent | Multi Framework Multi-Agent |
|-----------|------------------------------|----------------------------|
| Process count | 1 Framework process | N Framework processes |
| Resource sharing granularity | In-process sharing | Pod-level sharing |
| Isolation strength | Weak (Framework internal) | Strong (process-level) |
| Resource efficiency | Highest | High |
| Agent failure impact | May affect same-process Agents | Only affects single Agent |
| Agent Handler management | Monitor single process | Manage multiple processes |

**Considerations**:
- Both modes significantly improve resource utilization efficiency, avoiding traditional 1 Agent = 1 Pod waste
- Agent Handler selects appropriate mode based on framework characteristics, business requirements
- Idle Agents don't occupy extra Pods, dynamically schedule resources when tasks arrive
- Platform layer provides shared PID namespace support for Handler to manage multiple processes

#### 3.4.3 Design Points

AgentRuntime improves resource efficiency through the following design points:

1. **Lightweight Agent Handler**: Handler as management process, minimized resource occupation design
2. **Framework Process Sharing**: Multiple Agents share one Framework process, reducing process overhead
3. **Dynamic Resource Scheduling**: Framework internally allocates resources to Agents based on task demand
4. **Pod-level Resource Quota**: Resource quota set by Pod rather than Agent, flexible adjustment

**Agent Handler Resource Efficiency Design**:

| Design Point | Description |
|--------------|-------------|
| Minimized image size | Handler image streamlined, reducing startup time and storage occupation |
| Lightweight monitoring | Use shared PID namespace, avoiding complex monitoring mechanisms |
| Config file listening | Use fsnotify instead of polling, reducing CPU occupation |
| Event-driven processing | Handler only processes on event trigger, no overhead when idle |

### 3.5 Pod Container Configuration

**Shared Namespace Decisions**:

| Dimension | Decision | Considerations |
|-----------|----------|----------------|
| Process Namespace | Shared | Agent Handler needs to monitor Agent Framework process; shared PID namespace reduces isolation overhead, also supports multi-process management |
| Network Namespace | Shared | Agent Handler and Agent Framework communication doesn't need cross-network stack, minimal overhead |
| File System | Isolated | Agent Handler and Agent Framework images are released independently, need independent file spaces |

**Considerations**:
- Agent Handler and Agent Framework are tightly coupled process relationships, no strong security isolation requirement
- Lightweight container isolation, reduce overhead
- Requirement for independent image release and upgrade
- Shared PID namespace supports Agent Handler starting and managing multiple Agent Framework processes

### 3.6 Sandbox Integration Design

AgentRuntime supports integration with the agent-sandbox project, referencing Sandbox resources through Harness.

#### 3.6.1 Integration with agent-sandbox Project

Utilize the existing Sandbox CRD resource system from agent-sandbox project:

| CRD | Purpose |
|-----|---------|
| SandboxTemplate | Sandbox template, defines Pod spec, network policy, security policy |
| SandboxWarmPool | Warm pool, maintains pre-warmed Sandbox instances |
| SandboxClaim | Sandbox claim, obtains Sandbox instance |
| Sandbox | Sandbox instance (Pod) |

#### 3.6.2 Sandbox Modes

Reference existing Sandbox/SandboxClaim through Harness, support two mutually exclusive modes:

| Mode | Description | Applicable Scenarios |
|------|-------------|---------------------|
| External Mode | Sandbox as independent Pod, AgentRuntime calls via API | Multiple Agents share Sandbox resource pool |
| Embedded Mode | AgentRuntime Pod itself is Sandbox | Agent needs strongly isolated execution environment |

**Design Considerations**:

**Question**: Why support two modes?

**Considerations**:
- External Mode: Sandbox can be resource pool, dynamic scheduling, multiple Agents share
- Embedded Mode: Agent tightly coupled with Sandbox, execution environment more controllable
- Mutually exclusive design: One Harness can only choose one mode, avoid configuration confusion

#### 3.6.3 Sandbox Resource Pool Dynamic Association

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

#### 3.6.4 Container Merge Strategy (Embedded Sandbox)

When using embedded Sandbox mode, SandboxTemplate defines the Pod base specification, and AgentRuntime's Agent Handler and Agent Framework containers use **append mode** for merging.

**Considerations**:
- SandboxTemplate may already contain code execution containers, monitoring sidecars, etc.
- AgentRuntime's Agent Handler and Agent Framework are business core containers
- Append mode preserves SandboxTemplate integrity while overlaying Agent containers

### 3.7 CRD Structure Example

```yaml
apiVersion: ai.k8s.io/v1
kind: AgentRuntime
metadata:
  name: runtime-001
  namespace: tenant-a
spec:
  agentHandler:
    image: adk-handler:v1.2.0
    resources:
      limits:
        cpu: "500m"
        memory: "512Mi"
  agentFramework:
    image: adk-runtime:v1.2.0
    resources:
      limits:
        cpu: "1000m"
        memory: "1Gi"

  harness:
    model:
      - name: model-deepseek-default   # Model service configuration
    mcp:
      - name: mcp-registry-default    # MCP Registry configuration
    memory:
      - name: redis-memory
    sandbox:
      - name: gvisor-sandbox
    knowledge:
      - name: custom-rag

  agentConfig:                     # Public configuration (shared by all Agents)
    - name: protocol
      configMapRef:
        name: protocol-config
    - name: registry
      secretRef:
        name: registry-secret

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

AIAgent is an independent business object that can be scheduled to different AgentRuntimes for execution.

### 4.1 Object Definition and Design Considerations

**Question**: Why separate AIAgent from AgentRuntime?

**Decision**: Decoupled design, supports dynamic scheduling and migration.

**Considerations**:
- One AgentRuntime can host multiple AIAgents (N:1 relationship)
- AIAgent may need to migrate to different Runtime (failure, resource adjustment, maintenance)
- PVC data needs to follow AIAgent migration

### 4.2 Agent ID Design

| Field | Source | Purpose |
|-------|--------|---------|
| `metadata.name` | User specified | Unique within Namespace, human readable |
| `metadata.uid` | K8s auto-generated | Absolutely unique identifier |

**Considerations**:
- Follow K8s conventions, familiar to users
- name facilitates human recognition and kubectl operations
- uid for absolutely unique identifier, Agent Handler can choose which to use

### 4.3 Scheduling Mode

Adopt hybrid scheduling mode:

| User Specification | Controller Behavior |
|--------------------|---------------------|
| `runtimeRef.type: adk` | Automatically schedule to matching type Runtime |
| `runtimeRef.name: runtime-001` | Directly bind to specified instance |
| Not specified | Default scheduling policy |

**Considerations**:
- Flexibility: Users can choose automatic scheduling or manual binding
- Consistent with K8s scheduling mode (similar to Pod scheduling to Node)

### 4.4 Migration Support

| Migration Type | Trigger Condition |
|----------------|-------------------|
| Active Migration | User modifies `runtimeRef.name` |
| Automatic Migration | Runtime failure, resource shortage, maintenance eviction |

**Considerations**:
- Support operations (automatic migration during maintenance)
- Support user active adjustment (business requirement changes)

### 4.5 PVC Migration

During migration, PVC follows AIAgent, unbinds from old Pod and mounts to new Pod.

**Technical Constraints**:
- Requires network storage support (such as NFS, Ceph, Longhorn)
- Cloud storage (EBS, PD) requires detach/attach, has delay

**Considerations**:
- Data consistency: PVC follows Agent, ensures data not lost
- Storage backend selection: Users need to choose appropriate storage based on migration requirements

### 4.6 agentConfig Design

agentConfig is the business configuration delivery mechanism needed by Agent/Handler/Framework startup and runtime, separate from Harness (platform engineering capabilities).

#### 4.6.1 Design Philosophy

**Core Principle**: Platform layer only defines file delivery mechanism, Handler determines specific file content format.

```
Platform Layer Responsibilities:
├── Define file delivery mechanism (how to deliver)
└── Don't care about file content format

Handler Responsibilities:
├── Define configuration file format needed by its framework
├── Parse configuration file content
└── Use these configurations when starting Agent

User Responsibilities:
├── Prepare correctly formatted configuration files per Handler documentation
└── Submit to platform for delivery to Handler
```

#### 4.6.2 Configuration Declaration Method

**Decision**: AgentRuntime declares public configuration (for all Agents of same type), AIAgent appends Agent-specific configuration.

**Considerations**:
- Some configurations are same for all Agents of same type (e.g., protocol config, Registry connection)
- Some configurations are Agent-specific (e.g., Prompt content, skill definitions)
- Public configuration managed at Runtime level reduces duplicate configuration

#### 4.6.3 File Source

**Decision**: Reference external ConfigMap/Secret.

**Considerations**:
- Users pre-create ConfigMap/Secret to store configuration files
- AIAgent and AgentRuntime reference these external resources
- Configuration content separated from CRD, facilitates independent management and updates
- Follows K8s conventions (ConfigMap/Secret are standard carriers for configuration)

#### 4.6.4 Mount Path Specification

**Decision**: Unified mount path, sub-directories by source.

```
Pod Mount Structure:
/etc/agent-config/
├── runtime/                        # Runtime public configuration
│   ├── protocol/
│   │   └── protocol.yaml
│   └── registry/
│   │   └── registry.json
└── agent/                          # Agent-specific configuration
    ├── prompt/
    │   └── prompt.yaml
    └── skills/
    │   └── skills.yaml
```

**Considerations**:
- Handler knows to read all configuration files from `/etc/agent-config/`
- `runtime/` and `agent/` sub-directories distinguish public and specific configuration
- Handler decides merge logic itself, has maximum flexibility

#### 4.6.5 Update Mechanism

**Decision**: Handler actively monitors file changes.

**Considerations**:
- Handler uses fsnotify or polling to monitor `/etc/agent-config/` directory
- When files change, Handler reloads configuration and updates Agent
- Handler decides update strategy itself (immediate effect, waiting window, etc.)
- Platform layer doesn't intervene in update logic, reduces complexity

#### 4.6.6 File Naming

**Decision**: Handler defines configuration file naming specification, avoids name conflicts.

**Considerations**:
- Handler documents what configuration files are needed and their naming
- Users prepare differently named files per Handler requirements
- Platform layer doesn't handle file conflicts, only responsible for mounting

#### 4.6.7 Override Behavior

**Decision**: Merge mount, Handler decides merge logic.

**Considerations**:
- Runtime public configuration and AIAgent configuration both mounted to Pod
- Handler knows which is public (`runtime/` directory) and which is specific (`agent/` directory)
- Handler decides how to merge or override, has maximum flexibility

#### 4.6.8 Runtime Dynamic Update

**Decision**: Support dynamic update, Handler decides update method.

**Considerations**:
- After AgentRuntime's agentConfig is modified, all related Agents' Pods receive updates
- Handler monitors changes and decides how to update all Agents
- Platform layer responsible for ConfigMap sync, Handler responsible for business logic update

#### 4.6.9 Reference Scope

**Decision**: Only reference ConfigMap/Secret in same Namespace.

**Considerations**:
- Aligns with multi-tenant isolation principle
- Cross-Namespace reference requires extra RBAC permissions, increases security risk
- Consider extension when actual use case needs arise

#### 4.6.10 agentConfig Design Summary

| Dimension | Decision |
|-----------|----------|
| Design Philosophy | Platform only defines delivery mechanism, Handler determines content format |
| Naming | agentConfig |
| File Source | Reference external ConfigMap/Secret |
| Declaration Method | Runtime declares public config, AIAgent appends specific config |
| Mount Path | `/etc/agent-config/runtime/` and `/etc/agent-config/agent/` |
| Update Mechanism | Handler actively monitors file changes |
| File Naming | Handler defines specification, avoid same names |
| Override Behavior | Merge mount, Handler decides merge logic |
| Runtime Dynamic Update | Supported, Handler decides update method |
| Reference Scope | Same Namespace, no cross-Namespace |

### 4.7 CRD Structure Example

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
      - name: mcp-registry-default
        allowedServers:         # Override allowed MCP Servers
          - github
          - browser
        deniedServers:          # Add denied MCP Servers
          - filesystem
    memory:
      - name: redis-memory
        config:
          ttl: 3600

  agentConfig:                      # Agent-specific configuration (append)
    - name: prompt
      configMapRef:
        name: agent-prompt
    - name: skills
      configMapRef:
        name: agent-skills

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

## 5. Harness Design (WIP and TBD)

Harness is an independent CRD for AI Agent scaffolding capabilities, defining configurations for various external capabilities.

### 5.1 Harness vs agentConfig Concept Distinction

Before diving into specific object designs, it's important to clarify the distinction between two core concepts: Harness and agentConfig.

#### 5.1.1 Concept Definition

| Dimension | Harness | agentConfig |
|-----------|---------|-------------|
| **Positioning** | Platform engineering capabilities | Agent/Handler/Framework configuration information |
| **Examples** | Observability, security, traffic governance, Sandbox isolation, MCP integration, etc. | Prompt, protocol config (A2A), skill definitions, Registry connection, etc. |
| **Processing Method** | Platform-level processing based on Agent ID | Configuration content needed by Handler/Framework startup and runtime |
| **Responsibility** | Platform layer manages and provides | Handler determines format and usage |
| **Focus** | Capability externalization, standardization | Business configuration, framework-specific requirements |

#### 5.1.2 Design Considerations

**Question**: Why distinguish Harness and agentConfig?

**Decision**: Both have different responsibilities and need independent management.

**Considerations**:

- **Harness is Platform Engineering Capability**: These capabilities are unrelated to business logic, generic capabilities provided by the platform layer, such as security isolation, traffic governance, observability, etc. The platform layer can perform fine-grained control based on Agent ID, for example allowing/denying a specific Agent to use a specific Sandbox.

- **agentConfig is Business Configuration**: These configurations are business information needed by Agent/Handler/Framework startup and runtime, such as Prompt content, communication protocol config, skill definitions, etc. The platform layer doesn't care about the specific content and format of these configurations, only responsible for the delivery mechanism.

- **Decoupled Design**: Separating platform engineering capabilities from business configuration enables:
  - Platform layer focuses on providing and managing standardized engineering capabilities
  - Handler focuses on processing framework-specific business configuration
  - Users can independently manage both types of content without interference

### 5.2 Object Definition

#### 5.2.1 Design Considerations

**Question**: Why define capabilities as independent CRD instead of embedded configuration?

**Decision**: Independent CRD, facilitates reuse and unified management.

**Considerations**:
- Multiple AgentRuntimes/AIAgents can reference the same Harness
- Capability configuration managed independently, modification doesn't require changing Agent CRD
- Agent Handler needs standardized capability definitions to adapt different frameworks

#### 5.2.2 Scope Decision

Adopt Namespace level, no cluster-level sharing support.

**Considerations**:
- Multi-tenant isolation: Different Namespace configurations are independent
- Permission control: Namespace-level RBAC
- Follow K8s conventions (like Role vs ClusterRole)

#### 5.2.3 Single Type Constraint

One Harness CRD can only configure a single capability type, identified by the `type` field.

**Considerations**:
- Simple management: Each Harness has single responsibility
- Easy reference: AgentRuntime references by type grouping
- Follow K8s single responsibility principle

#### 5.2.4 Standard Type List

Currently supported capability types (extensible later):

| Type | Description |
|------|-------------|
| model | Model service, LLM model integration configuration |
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

### 5.3 Spec Structure

Adopt design where each type has independent spec field.

#### 5.3.1 Model Type Design

Model as platform-provided core service capability, configuring model services that Agents can access through Harness.

```yaml
spec:
  type: model
  model:
    provider: deepseek
    endpoint: https://api.deepseek.com/v1
    authSecretRef: deepseek-api-key
    models:
      - name: deepseek-chat
        allowed: true
        rateLimit: 100
      - name: deepseek-coder
        allowed: true
      - name: qwen-turbo
        allowed: true
      - name: qwen-max
        allowed: false
    defaultModel: deepseek-chat
```

**Considerations**:
- Model service is Agent's core dependency, needs unified configuration and management
- Platform layer provides model integration capability, Handler doesn't need to handle different provider details
- Multi-model configuration supported, Agent can choose appropriate model based on task requirements
- Model access control and rate limiting policies, avoid resource abuse
- Authentication info managed through Secret, aligns with K8s security practices

#### 5.3.2 MCP Type Special Design

Since MCP Servers are numerous and varied, it's impossible to enumerate all specific MCP Servers in Harness. Therefore, MCP Harness adopts **Registry Mode**, only configuring MCP Registry connection information and policies for allowed and denied MCP Servers.

```yaml
spec:
  type: mcp
  mcp:                      # MCP Registry configuration
    registry:
      endpoint: https://mcp-registry.example.com
      authSecretRef: mcp-registry-token
    allowedServers:         # Allowed MCP Server list (whitelist)
      - github
      - browser
      - filesystem
    deniedServers:          # Denied MCP Server list (blacklist)
      - dangerous-tool
    discoveryPolicy: allowlist  # Discovery policy: allowlist | denylist | all
```

**Considerations**:
- MCP Servers cannot be enumerated, Harness only configures Registry not specific Servers
- Agent Handler standardizes Registry connection and Server discovery mechanism
- Specific MCP Servers are dynamically decided by Agent business, obtained through Registry
- Whitelist/blacklist policies control available Server scope

#### 5.3.3 Other Type Examples

```yaml
spec:
  type: memory
  memory:
    backend: redis
    config:
      host: redis-server
      port: 6379
```

**Considerations**:
- Clear structure, type corresponds to configuration
- Easy validation and type checking
- Different types have different schema constraints

### 5.4 Binding Mode

#### 5.4.1 AgentRuntime Reference

AgentRuntime references Harness using type grouping:

```yaml
harness:
  model:
    - name: model-deepseek-default   # Model service configuration
  mcp:
    - name: mcp-registry-default    # MCP Registry configuration
  memory:
    - name: redis-memory
  sandbox:
    - name: gvisor-sandbox
```

#### 5.4.2 AIAgent Customization

AIAgent customization adopts reference + configuration override mode:

```yaml
harnessOverride:
  mcp:
    - name: mcp-registry-default
      allowedServers:         # Override allowed MCP Servers
        - github
        - browser
      deniedServers:          # Add denied MCP Servers
        - filesystem
    # Or deny entire Registry
    - deny: [mcp-registry-external]
```

**Considerations**:
- MCP Harness references Registry configuration, not specific Servers
- Agent can customize available Servers by overriding allowedServers/deniedServers
- deny supports disabling entire Registry, flexible control

### 5.5 Configuration Priority

When conflict occurs, AIAgent customization configuration takes precedence (premise: capability is available and implementable).

**Considerations**:
- Agent business needs priority
- Avoid Runtime configuration limiting Agent flexibility

### 5.6 Capability Validation

Controller validates capability availability when creating AIAgent, rejects creation if unavailable.

**Considerations**:
- Detect problems early, avoid runtime failures
- Reduce Agent Handler runtime validation burden

### 5.7 No Append Constraint

AIAgent cannot append Harness not provided by AgentRuntime, can only override or deny.

**Considerations**:
- Security control: Runtime administrator can limit available capability scope
- Avoid Agent arbitrarily extending capabilities, breaking security boundary

### 5.8 Harness Configuration Delivery Mechanism

#### 5.8.1 Shared Volume Mount Solution

Adopt shared Volume mounting ConfigMap to deliver Harness configuration to Agent Handler.

```
Pod
├── Agent Handler container
│   └── /etc/harness/ (ConfigMap mount)
└── Agent Framework container
│   └── /etc/harness/ (Same Volume)
```

#### 5.8.2 Design Considerations

**Question**: How does Agent Handler obtain Harness configuration?

**Decision**: Shared Volume mount ConfigMap, YAML format.

**Considerations**:

| Solution | Advantages | Disadvantages |
|----------|------------|---------------|
| Agent Handler accesses K8s API | Real-time change perception | Requires RBAC permissions, increases complexity |
| Dynamic API push | Real-time update | Agent Handler needs to expose API, increases complexity |
| Shared Volume mount | Simple, no permissions needed | ConfigMap update has delay (~1 minute) |

Reasons for choosing shared Volume mount:
- Agent Handler doesn't need K8s access permissions, reduces security risk
- Configuration changes don't require Pod restart
- Simple and reliable, follows Sidecar conventions (like Fluent Bit)

#### 5.8.3 ConfigMap Size Constraint

ConfigMap has 1MB size limit.

**Considerations**:
- Harness configuration mainly connection parameters, access methods, small volume
- Single Runtime configuration estimated tens of KB to hundreds of KB
- 1MB sufficient, no need to break limit

#### 5.8.4 File Watch Mechanism

Agent Handler can monitor configuration file changes through polling or fsnotify:

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

### 5.9 CRD Structure Examples

```yaml
# Model type - Model service configuration
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: model-deepseek-default
  namespace: tenant-a
spec:
  type: model
  model:
    provider: deepseek
    endpoint: https://api.deepseek.com/v1
    authSecretRef: deepseek-api-key
    models:
      - name: deepseek-chat
        allowed: true
        rateLimit: 100
      - name: deepseek-coder
        allowed: true
      - name: qwen-turbo
        allowed: true
      - name: qwen-max
        allowed: false
    defaultModel: deepseek-chat

---
# MCP type - Registry configuration
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: mcp-registry-default
  namespace: tenant-a
spec:
  type: mcp
  mcp:
    registry:
      endpoint: https://mcp-registry.example.com
      authSecretRef: mcp-registry-token
    allowedServers:         # Allowed MCP Server whitelist
      - github
      - browser
      - filesystem
      - slack
    deniedServers:          # Denied MCP Server blacklist
      - dangerous-tool
    discoveryPolicy: allowlist  # Discovery policy: allowlist | denylist | all

---
# MCP type - External Registry configuration
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: mcp-registry-external
  namespace: tenant-a
spec:
  type: mcp
  mcp:
    registry:
      endpoint: https://external-mcp.example.org
      authSecretRef: external-registry-token
      # No allowedServers/deniedServers means allow all Servers
    discoveryPolicy: all

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

## 6. Design Objectives Achievement Analysis

### 6.1 Framework Independence

**Achievement Method**:
- Unified AgentRuntime Controller, doesn't perceive framework details
- Agent Handler provided by framework community, responsible for framework adaptation
- Standardized Harness definition, Agent Handler uniformly converts

**Effect**: New framework integration only requires providing Agent Handler image, no platform code modification needed.

### 6.2 Externalized Capabilities

**Achievement Method**:
- Harness independent CRD, reusable
- AgentRuntime references public Harness
- AIAgent customization override, no append

**Effect**: Capability configuration managed independently, modification doesn't require changing Agent CRD, multiple Agents can reuse same Harness.

### 6.3 Flexible Scheduling

**Achievement Method**:
- AIAgent and AgentRuntime decoupling
- Hybrid scheduling mode (type automatic scheduling, instance fixed binding)
- PVC follows migration

**Effect**: Agent can dynamically migrate to different Runtime, supports operations maintenance and load balancing.

### 6.4 Multi-tenancy Support

**Achievement Method**:
- Namespace-level Harness
- PVC independent by AIAgent
- Sandbox isolation policy

**Effect**: Different Namespace configurations independent, data isolated, security boundary clear.

### 6.5 Security Isolation

**Achievement Method**:
- Sandbox two modes (external/embedded)
- Integration with agent-sandbox project security policy
- Harness no append constraint

**Effect**: Runtime administrator can limit capability scope, Agent execution environment can be isolated.

---

## 7. Summary

This design achieves the core resource definition for AI Agent in Kubernetes through multi-layer object abstraction (AIAgent, AgentRuntime, Harness, agentConfig). Core innovations include:

1. **Agent Handler Pattern**: Platform layer Controller uniformly abstracts, framework layer Agent Handler specifically adapts, decouples development responsibilities
2. **Agent and Runtime Separation**: Supports dynamic scheduling and migration, analogous to Pod and Node
3. **Flexible Process Mapping Modes**: Supports single process multiple Agents and multiple processes single Agent modes, decided by Agent Handler
4. **Harness Standardization**: Platform engineering capabilities externalized, inheritance + override mode customization
5. **agentConfig Abstraction**: Business configuration separated from platform capabilities, Handler determines format, platform provides delivery mechanism
6. **Sandbox Integration**: Reuses agent-sandbox project, supports multiple execution environment forms

Through this design, AI Agent becomes a first-class citizen in Kubernetes, a core abstraction similar to Pod, capable of adapting to any Agent framework, supporting complex business scenarios, while maintaining security isolation and multi-tenancy capabilities.

---

**Note: The opinions expressed in this article do not reflect the view of the author's affiliation.**

---

**Footnote**: When referencing the "1.0.1 Typical Scenario" section in other documents or articles, please cite the source and credit the author of this article.
