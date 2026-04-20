# Kubernetes的AI Agent核心资源抽象

## 1. 概述

### 1.1 目的

当前Kubernetes生态中缺少AI Agent这个核心对象的抽象。本设计旨在定义一个类似Pod的核心资源，能够对任何Agent框架（如LangChain、ADK、OpenClaw、CrewAI、Hermes等）进行统一抽象，同时外置Agent的各种脚手架能力（如CLI Tools、MCP、Skills、Knowledge/RAG、Memory、State、Guardrail、Security、Policy、Gateway、Sandbox等），这些外置能力可以通过AI Agent ID串接起来。

### 1.2 核心目标

- **框架无关性**：支持任何Agent框架，无需平台层为每个框架开发独立Controller
- **能力外置化**：脚手架能力独立管理，可复用、可定制
- **灵活调度**：AI Agent可以动态迁移到不同运行时环境
- **多租户支持**：Namespace级别资源隔离
- **安全隔离**：支持Sandbox执行环境的多种形态

---

## 2. 核心对象定义

### 2.1 架构分层

本设计将AI Agent抽象为三个核心对象：

```
┌─────────────────────────────────────┐
│         AIAgent (业务对象)            │
│    - 独立CRD，可被调度                 │
│    - 绑定Harness定制化配置             │
└─────────────────────────────────────┘
              │
              │ 调度/映射
              ▼
┌─────────────────────────────────────┐
│      AgentRuntime (运行时载体)         │
│    - Agent Handler + Agent Framework │
│    - 绑定公共Harness配置               │
│    - 1:1对应Pod                       │
└─────────────────────────────────────┘
              │
              │ 引用
              ▼
┌─────────────────────────────────────┐
│         Harness (脚手架能力)           │
│    - Namespace级独立CRD               │
│    - MCP、Memory、Sandbox等           │
└─────────────────────────────────────┘
```

**类比关系**：

| 对象 | 类似于K8s中的 | 说明 |
|------|---------------|------|
| AgentRuntime | Node | 运行时载体，承载Agent执行 |
| AIAgent | Pod | 可被调度的工作负载 |
| Harness | ConfigMap/Secret | 外置能力配置 |

---

## 3. AgentRuntime设计

### 3.1 对象定义

AgentRuntime是Agent Handler和Agent Framework的合并对象，对应一个Pod实例。

#### 3.1.1 设计考虑

**问题**：如何避免为每种Agent框架开发独立Controller？

**决策**：采用Agent Handler模式。

- **平台层Controller**：统一管理AgentRuntime和AIAgent CRD生命周期，不感知框架细节
- **Agent Handler**：由框架社区提供，负责具体框架的启动、配置转换、Agent管理

**优势**：
- Controller只需一个，平台层职责清晰
- Agent Handler可由框架生态提供，解耦开发职责
- 新框架接入只需提供Agent Handler镜像，无需修改平台代码

#### 3.1.2 Agent Handler与Agent Framework的进程映射模式

**关键设计考虑**：AgentRuntime中的Agent Handler与Agent Framework进程可以有多种映射关系，由Agent Handler自行决定。

**模式A：单进程多Agent模式**

```
Pod
├── Agent Handler容器
└── Agent Framework容器
    └── 一个Agent Framework进程
        ├── AIAgent-1
        ├── AIAgent-2
        └── AIAgent-3
```

- 一个Agent Handler和一个Agent Framework进程
- 一个Agent Framework进程内运行多个AI Agent
- Agent Framework进程内部实现Agent的路由和隔离

**考虑因素**：
- 适用于框架原生支持多Agent场景（如CrewAI、ADK多Agent）
- 资源效率高，减少进程开销
- Agent Framework进程负责内部Agent状态隔离

**模式B：多进程单Agent模式**

```
Pod
├── Agent Handler容器
│   └── 启动多个Agent Framework进程
├── Agent Framework进程-1 ──► AIAgent-1
├── Agent Framework进程-2 ──► AIAgent-2
└── Agent Framework进程-3 ──► AIAgent-3
```

- 一个Agent Handler拉起多个Agent Framework进程
- 每个Agent Framework进程对应一个AI Agent
- 进程级隔离，每个Agent独立运行

**考虑因素**：
- 适用于需要强隔离的场景
- 单Agent故障不影响其他Agent
- 资源开销较大，但隔离性更强

**决策依据**：具体采用哪种模式由Agent Handler根据框架特性、业务需求和资源情况自行决定。平台层不强制约束，只提供基础设施支持（如共享PID命名空间便于Handler管理多进程）。

#### 3.1.3 Pod容器配置

**共享命名空间决策**：

| 维度 | 决策 | 考虑因素 |
|------|------|----------|
| 进程命名空间 | 共享 | Agent Handler需监控Agent Framework进程，共享PID命名空间可降低隔离开销，同时支持多进程管理 |
| 网络命名空间 | 共享 | Agent Handler与Agent Framework通信无需跨网络栈，开销最低 |
| 文件系统 | 隔离 | Agent Handler和Agent Framework镜像独立发布，需要独立文件空间 |

**考虑因素**：
- Agent Handler和Agent Framework是紧耦合进程关系，无强安全隔离需求
- 轻量化容器隔离，减少开销
- 镜像独立发布和升级的需求
- 共享PID命名空间支持Agent Handler拉起和管理多个Agent Framework进程

#### 3.1.4 容器合并策略（嵌入式Sandbox）

当使用嵌入式Sandbox模式时，SandboxTemplate定义Pod基础规格，AgentRuntime的Agent Handler和Agent Framework容器采用**追加模式**合并。

**考虑因素**：
- SandboxTemplate可能已包含代码执行容器、监控sidecar等
- AgentRuntime的Agent Handler和Agent Framework是业务核心容器
- 追加模式保持SandboxTemplate完整性，同时叠加Agent容器

#### 3.1.5 CRD结构示例

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
    mcp:
      - name: mcp-registry-default    # MCP Registry配置
    memory:
      - name: redis-memory
    sandbox:
      - name: gvisor-sandbox
    knowledge:
      - name: custom-rag

  sandboxTemplateRef: secure-template  # 嵌入式Sandbox时使用

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

## 4. AIAgent设计

### 4.1 对象定义

AIAgent是独立的业务对象，可以被调度到不同的AgentRuntime上执行。

#### 4.1.1 设计考虑

**问题**：为什么要将AIAgent与AgentRuntime分离？

**决策**：解耦设计，支持动态调度和迁移。

**考虑因素**：
- 一个AgentRuntime可以承载多个AIAgent（N:1关系）
- AIAgent可能需要迁移到不同Runtime（故障、资源调整、维护）
- PVC数据需要跟随AIAgent迁移

#### 4.1.2 Agent ID设计

| 字段 | 来源 | 用途 |
|------|------|------|
| `metadata.name` | 用户指定 | Namespace内唯一，人类可读 |
| `metadata.uid` | K8s自动生成 | 绝对唯一标识 |

**考虑因素**：
- 遵循K8s惯例，用户熟悉
- name便于人类识别和kubectl操作
- uid用于绝对唯一标识，Agent Handler可选择使用哪个

#### 4.1.3 调度模式

采用混合调度模式：

| 用户指定 | Controller行为 |
|----------|----------------|
| `runtimeRef.type: adk` | 自动调度到匹配类型的Runtime |
| `runtimeRef.name: runtime-001` | 直接绑定到指定实例 |
| 不指定 | 默认调度策略 |

**考虑因素**：
- 灵活性：用户可选择自动调度或手动绑定
- 与K8s调度模式一致（类似Pod调度到Node）

#### 4.1.4 迁移支持

| 迁移类型 | 触发条件 |
|----------|----------|
| 主动迁移 | 用户修改`runtimeRef.name` |
| 自动迁移 | Runtime故障、资源不足、维护驱逐 |

**考虑因素**：
- 支持运维操作（维护时自动迁移）
- 支持用户主动调整（业务需求变化）

#### 4.1.5 PVC迁移

迁移时PVC跟随AIAgent，从旧Pod解绑并挂载到新Pod。

**技术约束**：
- 需要网络存储支持（如NFS、Ceph、Longhorn）
- 云存储（EBS、PD）需detach/attach，有延迟

**考虑因素**：
- 数据一致性：PVC跟随Agent，保证数据不丢失
- 存储后端选择：用户需根据迁移需求选择合适存储

#### 4.1.6 CRD结构示例

```yaml
apiVersion: ai.k8s.io/v1
kind: AIAgent
metadata:
  name: agent-001
  namespace: tenant-a
spec:
  runtimeRef:
    type: adk              # 指定类型，自动调度
    # 或 name: runtime-001  # 指定实例，固定绑定

  harnessOverride:
    mcp:
      - name: mcp-registry-default
        allowedServers:         # 覆盖允许的MCP Server
          - github
          - browser
        deniedServers:          # 添加禁止的MCP Server
          - filesystem
    memory:
      - name: redis-memory
        config:
          ttl: 3600

  volumePolicy: retain     # PVC生命周期策略：retain | delete

  description: "数据分析Agent"

status:
  phase: Running
  runtimeRef:
    name: runtime-001      # 当前绑定的Runtime
  conditions:
    - type: Ready
      status: "True"
```

---

## 5. Harness与agentConfig的概念区分

在深入设计具体对象之前，需要明确两个核心概念的区分：Harness和agentConfig。

### 5.0.1 概念定义

| 维度 | Harness | agentConfig |
|------|---------|-------------|
| **定位** | 平台工程能力 | Agent/Handler/Framework配置信息 |
| **示例** | 可观察性、安全、流量治理、Sandbox隔离、MCP接入等 | Prompt、协议配置（A2A）、技能定义、Registry连接等 |
| **处理方式** | 根据Agent ID进行细粒度的平台级处理 | Handler/Framework启动和运行需要的配置内容 |
| **责任方** | 平台层负责管理和提供 | Handler决定格式和用途 |
| **关注点** | 能力外置化、标准化 | 业务配置、框架特定需求 |

### 5.0.2 设计考虑

**问题**：为什么需要区分Harness和agentConfig？

**决策**：两者职责不同，需要独立管理。

**考虑因素**：

- **Harness是平台工程能力**：这些能力与业务逻辑无关，是平台层提供的通用能力，如安全隔离、流量治理、可观察性等。平台层可以根据Agent ID进行细粒度的控制，例如允许/禁止某个Agent使用某个Sandbox。

- **agentConfig是业务配置**：这些配置是Agent/Handler/Framework启动和运行所需的业务信息，如Prompt内容、通信协议配置、技能定义等。平台层不关心这些配置的具体内容和格式，只负责传递机制。

- **解耦设计**：将平台工程能力与业务配置分离，使得：
  - 平台层专注于提供和管理标准化的工程能力
  - Handler专注于处理框架特定的业务配置
  - 用户可以独立管理两类内容，互不干扰

---

## 6. Harness设计

### 6.1 对象定义

Harness是AI Agent脚手架能力的独立CRD，定义各种外置能力的配置。

#### 6.1.1 设计考虑

**问题**：为什么将能力定义为独立CRD而非内嵌配置？

**决策**：独立CRD，便于复用和统一管理。

**考虑因素**：
- 多个AgentRuntime/AIAgent可引用同一个Harness
- 能力配置独立管理，修改无需改动Agent CRD
- Agent Handler需要标准化能力定义，适配不同框架

#### 5.1.2 Scope决策

采用Namespace级别，不支持集群级共享。

**考虑因素**：
- 多租户隔离：不同Namespace的配置独立
- 权限控制：Namespace级RBAC
- 遵循K8s惯例（如Role vs ClusterRole）

#### 5.1.3 单一类型约束

一个Harness CRD只能配置单一能力类型，通过`type`字段标识。

**考虑因素**：
- 简化管理：每个Harness职责单一
- 便于引用：AgentRuntime按类型分组引用
- 遵循K8s单一职责原则

#### 5.1.4 标准类型列表

当前支持的能力类型（后续可扩展）：

| 类型 | 说明 |
|------|------|
| mcp | Model Context Protocol，工具/能力接入 |
| skills | 技能模块 |
| cli-tools | 命令行工具 |
| knowledge | 知识库/RAG |
| memory | 记忆存储 |
| state | 运行状态 |
| guardrail | 安全护栏 |
| security | 安全策略 |
| policy | 策略控制 |
| gateway | API网关 |
| sandbox | 执行隔离环境 |

#### 5.1.5 Spec结构

采用每种类型独立spec字段的设计。

**MCP类型特殊设计**：由于MCP Server数量庞大且种类繁多，无法在Harness中枚举所有具体的MCP Server。因此MCP Harness采用**Registry模式**，只配置MCP Registry的连接信息以及允许发现和禁止发现的MCP Server策略。

```yaml
spec:
  type: mcp
  mcp:                      # MCP Registry配置
    registry:
      endpoint: https://mcp-registry.example.com
      authSecretRef: mcp-registry-token
    allowedServers:         # 允许发现的MCP Server列表（白名单）
      - github
      - browser
      - filesystem
    deniedServers:          # 禁止发现的MCP Server列表（黑名单）
      - dangerous-tool
    discoveryPolicy: allowlist  # 发现策略：allowlist | denylist | all
```

**考虑因素**：
- MCP Server无法枚举，Harness只配置Registry而非具体Server
- Handler标准化处理Registry连接和Server发现机制
- 具体MCP Server由Agent业务动态决定，通过Registry获取
- 白名单/黑名单策略控制可用Server范围

**其他类型示例**：

```yaml
spec:
  type: memory
  memory:
    backend: redis
    config:
      host: redis-server
      port: 6379
```

**考虑因素**：
- 结构清晰，类型与配置对应
- 便于校验和类型检查
- 不同类型有不同的schema约束

#### 5.1.6 绑定模式

AgentRuntime引用Harness采用按类型分组：

```yaml
harness:
  mcp:
    - name: mcp-registry-default    # MCP Registry配置
  memory:
    - name: redis-memory
  sandbox:
    - name: gvisor-sandbox
```

AIAgent定制化采用引用+配置覆盖模式：

```yaml
harnessOverride:
  mcp:
    - name: mcp-registry-default
      allowedServers:         # 覆盖允许的MCP Server
        - github
        - browser
      deniedServers:          # 添加禁止的MCP Server
        - filesystem
    # 或直接禁止整个Registry
    - deny: [mcp-registry-external]
```

**考虑因素**：
- MCP Harness引用Registry配置，而非具体Server
- Agent可以通过覆盖allowedServers/deniedServers定制可用Server
- deny支持禁用整个Registry，灵活控制

#### 5.1.7 配置优先级

冲突时以AIAgent定制化配置为准（前提：能力可获得可实施）。

**考虑因素**：
- Agent业务需求优先
- 避免Runtime配置限制Agent灵活性

#### 5.1.8 能力校验

Controller在创建AIAgent时校验能力是否可获得，不可用则拒绝创建。

**考虑因素**：
- 提前发现问题，避免运行时失败
- 减少Agent Handler运行时校验负担

#### 5.1.9 不追加约束

AIAgent不能追加AgentRuntime未提供的Harness，只能覆盖或禁止。

**考虑因素**：
- 安全控制：Runtime管理员可限定可用能力范围
- 避免Agent随意扩展能力，突破安全边界

#### 5.1.10 CRD结构示例

```yaml
# MCP类型 - Registry配置
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
    allowedServers:         # 允许发现的MCP Server白名单
      - github
      - browser
      - filesystem
      - slack
    deniedServers:          # 禁止发现的MCP Server黑名单
      - dangerous-tool
    discoveryPolicy: allowlist  # 发现策略：allowlist(白名单) | denylist(黑名单) | all(全部)

---
# MCP类型 - 外部Registry配置
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
      # 无allowedServers/deniedServers表示允许所有Server
    discoveryPolicy: all

---
# Memory类型
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
# Sandbox类型（外部模式）
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
# Sandbox类型（嵌入式模式）
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

## 6. Harness配置传递机制

### 6.1 共享Volume挂载方案

采用共享Volume挂载ConfigMap的方式传递Harness配置给Agent Handler。

```
Pod
├── Agent Handler容器
│   └── /etc/harness/ (ConfigMap挂载)
└── Agent Framework容器
│   └── /etc/harness/ (同一Volume)
```

#### 6.1.1 设计考虑

**问题**：Agent Handler如何获取Harness配置？

**决策**：共享Volume挂载ConfigMap，YAML格式。

**考虑因素**：

| 方案 | 优点 | 缺点 |
|------|------|------|
| Agent Handler访问K8s API | 实时感知变更 | 需RBAC权限，增加复杂度 |
| 动态API推送 | 实时更新 | Agent Handler需暴露API，增加复杂度 |
| 共享Volume挂载 | 简单，无需权限 | ConfigMap更新有延迟(~1分钟) |

选择共享Volume挂载的原因：
- Agent Handler无需K8s访问权限，降低安全风险
- 配置变更无需重启Pod
- 简单可靠，符合Sidecar惯例（如Fluent Bit）

#### 6.1.2 ConfigMap大小约束

ConfigMap有1MB大小限制。

**考虑因素**：
- Harness配置主要是连接参数、访问方式，体积较小
- 单个Runtime配置预估几十KB到几百KB
- 1MB足够，无需突破限制

#### 6.1.3 文件Watch机制

Agent Handler可通过轮询或fsnotify监听配置文件变更：

```go
// fsnotify监听
watcher, _ := fsnotify.NewWatcher()
watcher.Add("/etc/harness/")
for event := range watcher.Events {
    if event.Op == fsnotify.Write {
        reloadConfig()
    }
}
```

---

## 7. Sandbox集成设计

### 7.1 与agent-sandbox项目集成

利用agent-sandbox项目现有的Sandbox CRD资源体系：

| CRD | 作用 |
|-----|------|
| SandboxTemplate | 沙箱模板，定义Pod规格、网络策略、安全策略 |
| SandboxWarmPool | 预热池，维护预热的Sandbox实例 |
| SandboxClaim | 沙箱申领，获取Sandbox实例 |
| Sandbox | 沙箱实例（Pod） |

### 7.2 Sandbox模式

通过Harness引用已存在的Sandbox/SandboxClaim，支持两种互斥模式：

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| 外部模式 | Sandbox作为独立Pod，AgentRuntime通过API调用 | 多Agent共享Sandbox资源池 |
| 嵌入式模式 | AgentRuntime Pod本身就是Sandbox | Agent需要强隔离执行环境 |

#### 7.2.1 设计考虑

**问题**：为什么支持两种模式？

**考虑因素**：
- 外部模式：Sandbox可作为资源池，动态调度，多个Agent共享
- 嵌入式模式：Agent与Sandbox紧耦合，执行环境更可控
- 互斥设计：一个Harness只能选择一种，避免配置混乱

### 7.3 Sandbox资源池动态关联

SandboxWarmPool维护预热实例，SandboxClaim可选择从池中获取或新建。

```
SandboxWarmPool ──► 预热Sandbox实例
        │
        ▼
SandboxClaim ──► 从池获取或新建
        │
        ▼
Harness引用 ──► AIAgent/AgentRuntime使用
```

**考虑因素**：
- 预热池降低启动延迟
- 动态关联支持按需调度

---

## 9. agentConfig设计

agentConfig是Agent/Handler/Framework启动和运行所需的业务配置传递机制，与Harness（平台工程能力）职责分离。

### 9.1 设计理念

**核心原则**：平台层只定义文件传递机制，具体文件内容由Handler决定格式。

```
平台层职责：
├── 定义文件传递机制（如何传递）
└── 不关心文件内容格式

Handler职责：
├── 定义自己框架需要的配置文件格式
├── 解析配置文件内容
└── 启动Agent时使用这些配置

用户职责：
├── 根据Handler文档，准备正确格式的配置文件
└── 提交给平台传递给Handler
```

### 9.2 配置声明方式

**决策**：AgentRuntime声明公共配置（针对所有同类Agent），AIAgent追加Agent专属配置。

**考虑因素**：
- 有些配置对所有同类Agent是一样的（如协议配置、Registry连接）
- 有些配置是Agent特有的（如Prompt内容、技能定义）
- 公共配置在Runtime级别管理，减少重复配置

### 9.3 文件来源

**决策**：引用外部ConfigMap/Secret。

**考虑因素**：
- 用户预先创建ConfigMap/Secret存放配置文件
- AIAgent和AgentRuntime引用这些外部资源
- 配置内容与CRD分离，便于独立管理和更新
- 遵循K8s惯例（ConfigMap/Secret是配置的标准载体）

### 9.4 挂载路径规范

**决策**：统一挂载路径，按来源分子目录。

```
Pod挂载结构：
/etc/agent-config/
├── runtime/                        # Runtime公共配置
│   ├── protocol/
│   │   └── protocol.yaml
│   └── registry/
│   │   └── registry.json
└── agent/                          # Agent专属配置
    ├── prompt/
    │   └── prompt.yaml
    └── skills/
    │   └── skills.yaml
```

**考虑因素**：
- Handler知道去`/etc/agent-config/`读取所有配置文件
- `runtime/`和`agent/`子目录区分公共配置和专属配置
- Handler自行决定合并逻辑，具有最大灵活性

### 9.5 更新机制

**决策**：Handler主动监听文件变更。

**考虑因素**：
- Handler使用fsnotify或轮询监听`/etc/agent-config/`目录
- 文件变更时，Handler重新加载配置并更新Agent
- Handler自行决定更新策略（立即生效、等待窗口等）
- 平台层不介入更新逻辑，降低复杂度

### 9.6 文件命名

**决策**：Handler定义配置文件命名规范，避免同名冲突。

**考虑因素**：
- Handler在文档中说明需要哪些配置文件及其命名
- 用户按Handler要求准备不同名称的文件
- 平台层不处理文件冲突，只负责挂载

### 9.7 覆盖行为

**决策**：合并挂载，Handler决定合并逻辑。

**考虑因素**：
- Runtime公共配置和AIAgent配置都挂载到Pod
- Handler知道哪些是公共配置（`runtime/`目录），哪些是专属配置（`agent/`目录）
- Handler自行决定如何合并或覆盖，具有最大灵活性

### 9.8 Runtime动态更新

**决策**：支持动态更新，Handler决定更新方式。

**考虑因素**：
- AgentRuntime的agentConfig修改后，所有相关Agent的Pod都会收到更新
- Handler监听变更并决定如何更新所有Agent
- 平台层负责ConfigMap同步，Handler负责业务逻辑更新

### 9.9 引用范围

**决策**：只能引用同Namespace的ConfigMap/Secret。

**考虑因素**：
- 符合多租户隔离原则
- 跨Namespace引用需要额外RBAC权限，增加安全风险
- 实际用例需要时再考虑扩展

### 9.10 CRD结构示例

```yaml
# AgentRuntime CRD
apiVersion: ai.k8s.io/v1
kind: AgentRuntime
metadata:
  name: runtime-001
  namespace: tenant-a
spec:
  agentHandler:
    image: adk-handler:v1.2.0
  agentFramework:
    image: adk-runtime:v1.2.0
  
  harness:                         # 平台工程能力（外置）
    mcp:
      - name: mcp-registry-default
    sandbox:
      - name: gvisor-sandbox
  
  agentConfig:                     # 公共配置（所有Agent共享）
    - name: protocol
      configMapRef:
        name: protocol-config
    - name: registry
      secretRef:
        name: registry-secret

---
# AIAgent CRD
apiVersion: ai.k8s.io/v1
kind: AIAgent
metadata:
  name: agent-001
  namespace: tenant-a
spec:
  runtimeRef:
    type: adk
  
  harnessOverride:                  # Harness能力定制化（覆盖/禁止）
    mcp:
      - name: mcp-registry-default
        allowedServers:
          - github
  
  agentConfig:                      # Agent专属配置（追加）
    - name: prompt
      configMapRef:
        name: agent-prompt
    - name: skills
      configMapRef:
        name: agent-skills
```

### 9.11 agentConfig设计汇总

| 维度 | 决策 |
|------|------|
| 设计理念 | 平台只定义传递机制，Handler决定内容格式 |
| 命名 | agentConfig |
| 文件来源 | 引用外部ConfigMap/Secret |
| 声明方式 | Runtime声明公共配置，AIAgent追加专属配置 |
| 挂载路径 | `/etc/agent-config/runtime/`和`/etc/agent-config/agent/` |
| 更新机制 | Handler主动监听文件变更 |
| 文件命名 | Handler定义规范，避免同名 |
| 覆盖行为 | 合并挂载，Handler决定合并逻辑 |
| Runtime动态更新 | 支持，Handler决定更新方式 |
| 引用范围 | 同Namespace，不跨Namespace |

---

## 10. 设计目标达成分析

### 8.1 框架无关性

**达成方式**：
- 统一的AgentRuntime Controller，不感知框架细节
- Agent Handler由框架社区提供，负责框架适配
- 标准化Harness定义，Agent Handler统一转换

**效果**：新框架接入只需提供Agent Handler镜像，无需修改平台代码。

### 8.2 能力外置化

**达成方式**：
- Harness独立CRD，可复用
- AgentRuntime引用公共Harness
- AIAgent定制化覆盖，不追加

**效果**：能力配置独立管理，修改无需改动Agent CRD，多个Agent可复用同一Harness。

### 8.3 灵活调度

**达成方式**：
- AIAgent与AgentRuntime解耦
- 混合调度模式（类型自动调度，实例固定绑定）
- PVC跟随迁移

**效果**：Agent可动态迁移到不同Runtime，支持运维维护和负载均衡。

### 8.4 多租户支持

**达成方式**：
- Namespace级Harness
- PVC按AIAgent独立
- Sandbox隔离策略

**效果**：不同Namespace配置独立，数据隔离，安全边界清晰。

### 8.5 安全隔离

**达成方式**：
- Sandbox两种模式（外部/嵌入式）
- 集成agent-sandbox项目安全策略
- Harness不追加约束

**效果**：Runtime管理员可限定能力范围，Agent执行环境可隔离。

---

## 11. 总结

本设计通过多层对象抽象（AIAgent、AgentRuntime、Harness、agentConfig），实现了AI Agent在Kubernetes中的核心资源定义。核心创新点包括：

1. **Agent Handler模式**：平台层Controller统一抽象，框架层Agent Handler具体适配，解耦开发职责
2. **Agent与Runtime分离**：支持动态调度和迁移，类比Pod与Node
3. **灵活的进程映射模式**：支持单进程多Agent和多进程单Agent两种模式，由Agent Handler自行决定
4. **Harness标准化**：平台工程能力外置化管理，继承+覆盖模式定制
5. **agentConfig抽象**：业务配置与平台能力分离，Handler决定格式，平台提供传递机制
6. **Sandbox集成**：复用agent-sandbox项目，支持多种执行环境形态

通过本设计，AI Agent成为Kubernetes中的一等公民，类似Pod的核心抽象，能够适配任何Agent框架，支持复杂业务场景，同时保持安全隔离和多租户能力。
