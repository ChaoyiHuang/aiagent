# AI Agent abstraction in Kubernetes

## 1. 概述

### 1.1 场景和挑战

#### 1.1.1 典型场景

WeChat是中国的一家社交软件，日活用户超过1.2 billion。WeChat最近开放了一个AI Agent插件，类似OpenClaw或其他AI Agent可以在WeChat中被访问。这打开了一扇门，任何一个WeChat用户可以通过扫码就可以和一个AI Agent加为好友并聊天。AI Agent通过WeChat提供的插件，以客户端模式接入到WeChat的AI Agent Gateway。

Alice是一个人的AI Agent创业公司，她开发了一个生活助手AI Agent。她发现如果自己独立开发手机APP，需要负担成本昂贵的推广成本，且需要处理复杂的Web服务治理、安全和运维。因此通过WeChat接入是最低成本抵达billion级用户的方式。

现在她的生活助手AI Agent已经开发完，她需要考虑上线以后的很多问题：从公有云租用虚拟机去host这些AI Agent也是很大一笔成本，一个AI Agent独占一个虚机或者独占Pod/Sandbox不是很合算的方案。如果AI Agent短时间增长到百万、千万级别，资源开销很大。因此很自然的，单个进程运行很多AI Agent是比较合理的方案选择，AI Agent和执行环境Sandbox分离、分别最大化复用资源成为她的选择。

在业务运行中，她很快发现，大量用户尝试几天后就不再活跃，不知道哪一天会恢复活动，但又不能删除；同时即使是活跃用户，AI Agent和Sandbox的活跃时间长短也千差万别。为了节省运营成本，她需要用最少的公有云租用成本，通过动态consolidate AI Agent，维持进程/Pod/Sandbox的数量到最少，同时可以通过动态扩容（scaling out）满足可能爆发的业务增长，和动态缩容（scaling in）解决业务垂直消退的诉求。除了增长视角外，自动扩缩容对于管理日常高峰和低谷波动也至关重要。在任何情况下，仅仅维持当前业务真实需要的最少资源。

作为一个人的AI Agent创业公司，她需要AI Agent粒度的平台工程来帮助她。

#### 1.1.2 AI Agent的资源利用效率问题

AI Agent具有一些新的资源使用特征：

| 特征 | 说明 |
|------|------|
| 空闲时间长 | Agent大部分时间处于空闲状态，等待任务触发 |
| 任务突发性 | 任务到来时资源使用突增，完成后快速回落 |
| 任务时长差异 | 短任务（秒级~分钟级）和长任务（小时级）并存 |
| 资源需求波动 | 不同任务对CPU、内存、网络的需求差异大 |

当AI Agent执行任务时，可能执行工具，生成代码并运行，因为安全性原因，AI Agent和执行环境（sandbox）存在多样化的考虑：AI Agent和执行环境合并，AI Agent和执行环境分离，或混合模式。

当Kubernetes集群运行规模数量（以及不同类型的）AI Agent时，如何有效地提升集群的资源利用效率，是一个共性的问题。而需要有效地利用资源，能够在AI Agent粒度去识别和处理负载，就非常重要。

#### 1.1.3 AI Agent技术快速迭代，平台工程跟不上AI Agent框架发展

从早期的Langchain，到Manus，到coding agent再到OpenClaw、Hermes，每一次迭代，技术框架都在演进。CNCF/Kubernetes的平台工程，可观察性，治理，安全，策略，流量等等，还是传统在Pod、微服务、服务网格、Serverless等基础上构建的平台工程。要解决1.1.1的问题，需要解决对AI Agent粒度的感知问题。

### 1.2 设计目的

当前Kubernetes生态中缺少AI Agent这个核心对象的抽象。本设计旨在定义一个类似Pod的核心资源，能够对**任何**已经存在的Agent框架（如LangChain、ADK、Sematic Kernel、OpenClaw、Hermes等）和**任何**将来未知的Agent框架进行统一抽象，同时外置Agent的各种脚手架能力（如Model、MCP、Skills、Knowledge/RAG、Memory、State、Guardrail、Security、Policy、Gateway、Sandbox等），这些外置能力可以通过AI Agent ID/Name串接起来。

为了提升资源效率并降低成本，常用技术包括AI Agent装箱整合（bin packing）、迁移、Pod/Node扩缩容（out/in）、Pod调整大小、以及Sandbox复用/休眠/调整大小等。因此，AI Agent的抽象必须能够在单个AI Agent的细粒度层面上实现这些技术。

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
│    - Model、Memory、Sandbox等         │
└─────────────────────────────────────┘
```

---

## 3. AgentRuntime设计

AgentRuntime是Agent Handler和Agent Framework的合并对象，对应一个Pod实例。AgentRuntime Controller管理AgentRuntime和AIAgent CRD的生命周期，并且与Agent框架无关（framework agnostic）。

### 3.1 对象定义与设计考虑

- **Agent Handler**：由Agent框架社区提供，负责具体框架的启动、配置转换和AI Agent生命周期管理。
- **Agent Framework**：如LangChain、ADK、Sematic Kernel、OpenClaw、Hermes等运行AI Agent的框架。

**优势**：
- Controller只需一个，平台层职责清晰
- Agent Handler可由框架生态提供，解耦开发职责
- 新框架接入只需提供Agent Handler镜像，无需修改平台代码

**TBD**：
- Agent Handler不应参与AI Agent的运营流量。相反，它应该只配置外部网关来管理实际的流量路由。

### 3.2 Agent Handler与Agent Framework的进程映射模式

**关键设计考虑**：
- AgentRuntime中的Agent Handler与Agent Framework进程可以有多种映射关系，由Agent Handler自行决定。
- 为了最优的资源利用，Agent Framework和Agent Handler都作为轻量级容器部署。Agent Handler由容器运行时管理，而Agent Framework和单个AI Agent由Handler实例化或通过事件驱动机制创建。

**模式A：单进程多Agent模式**

```
Pod
├── Agent Handler进程
└── Agent Framework进程
        ├── AIAgent-1
        ├── AIAgent-2
        └── AIAgent-3
```

- 一个Agent Handler和一个Agent Framework进程
- 一个Agent Framework进程内运行多个AI Agent
- Agent Framework进程内部实现Agent的路由和隔离

**考虑因素**：
- 适用于框架原生支持多Agent实例（相同类型或不同类型）的场景
- 资源效率高，减少进程开销
- Agent Framework进程负责内部Agent状态隔离

**模式B：多进程单Agent模式**

```
Pod
├── Agent Handler进程
│   └── 启动多个Agent Framework进程
├── Agent Framework进程-1 ──► AIAgent-1
├── Agent Framework进程-2 ──► AIAgent-2
└── Agent Framework进程-3 ──► AIAgent-3
```

- 一个Agent Handler拉起多个Agent Framework进程
- 每个Agent Framework进程对应一个AI Agent
- 进程级隔离，每个Agent独立运行

**考虑因素**：
- 单Agent故障不影响其他Agent
- 资源开销较大，但隔离性更强
- Agent Framework进程可能只支持一个对外可见的Agent

**决策依据**：具体采用哪种模式由Agent Handler根据框架特性、业务需求和资源情况自行决定。平台层不强制约束，只提供基础设施支持（如共享PID命名空间便于Handler管理多进程）。

### 3.3 Agent Framework运行模式

Agent Framework支持多种运行模式，由Agent Handler根据框架特性和业务需求决定。

#### 3.3.1 生命周期模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| Long Running | 长期运行的服务，持续提供服务能力 | 需要持续响应请求、保持状态的Agent |
| Event-triggered | 事件触发按需运行，完成任务后终止 | 执行特定任务后无需持续运行的Agent |

**考虑因素**：
- Long Running模式适合需要持续提供服务的场景，如聊天服务、监控告警Agent
- Event-triggered模式适合一次性任务，如数据处理、报告生成等
- 平台层通过replicas和lifecycle策略支持不同生命周期模式

#### 3.3.2 通信模式

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| Server模式 | 监听端口，对外提供服务 | Agent作为服务端，接收外部请求 |
| Client模式 | 主动连接外部服务，类似聊天客户端 | Agent作为客户端，连接平台服务（如OpenClaw、WeChat的weixin-claw） |

**Server模式示例**：

```
外部请求 ──► AgentRuntime Pod
                    │
                    ▼
            Agent Framework
            (监听端口 8080)
                    │
                    ▼
            AIAgent处理请求
```

**Client模式示例**：

```
AgentRuntime Pod
│
└── Agent Framework
    └── AIAgent (OpenClaw / weixin-claw) ──► 连接外部平台服务
                                              │
                                              ▼
                                        WhatsApp / Discord / WeChat...
```

**考虑因素**：
- Server模式适合Agent需要暴露API供外部调用的场景
- Client模式适合Agent需要连接现有平台服务的场景，如微信机器人、钉钉助手等
- Agent Handler根据框架特性选择合适的通信模式
- 平台层提供网络配置支持，但不强制约束通信方式

### 3.4 资源效率考虑

AgentRuntime设计考虑了AI Agent的资源利用效率问题。

#### 3.4.1 资源共享策略

AgentRuntime支持两种多AI Agent模式，均可实现资源利用效率提升：

**模式一：单Agent Framework多AI Agent**

```
AgentRuntime Pod
│
├── Agent Handler (轻量级，资源占用少)
│
└── Agent Framework (单进程)
    ├── AIAgent-1 (空闲)
    ├── AIAgent-2 (执行任务)
    └── AIAgent-3 (空闲)
```

**资源效率优势**：
- 一个Agent Framework进程承载多个AI Agent
- 进程级别资源共享：内存、网络连接、运行时环境
- 空闲Agent仅占用Framework内部状态，无额外进程开销
- Framework内部实现Agent调度和资源分配

**适用场景**：
- Agent框架原生支持单进程多Agent
- 资源效率优先，进程内命名空间级别隔离

**模式二：多Agent Framework进程多AI Agent**

```
AgentRuntime Pod
│
├── Agent Handler (轻量级管理进程)
│   ├── 启动 Agent Framework进程-1
│   ├── 启动 Agent Framework进程-2
│   └── 启动 Agent Framework进程-3
│
├── Agent Framework进程-1 ──► AIAgent-1
├── Agent Framework进程-2 ──► AIAgent-2
└── Agent Framework进程-3 ──► AIAgent-3
```

**资源效率优势**：
- 共享Pod基础设施：共享网络命名空间、共享PID命名空间
- 共享Pod资源配额：多个Agent共享同一个Pod的CPU/内存配额
- 空闲Agent Framework进程资源占用低，可快速激活
- 避免传统模式每个Agent独立Pod的调度开销

**适用场景**：
- 需要进程级别隔离的Agent
- 单Agent故障不影响其他Agent

**对比分析**：

| 维度 | 单Framework多Agent | 多Framework多Agent |
|------|-------------------|-------------------|
| 进程数量 | 1个Framework进程 | N个Framework进程 |
| 资源共享粒度 | 进程内共享 | Pod级别共享 |
| 资源效率 | 最高 | 高 |
| Agent故障影响 | 可能影响同进程Agent | 仅影响单个Agent |
| Agent Handler管理 | 监控单个进程 | 管理多个进程 |

**考虑因素**：
- 两种模式都显著提升资源利用效率，避免传统1 Agent = 1 Pod的浪费
- Agent Handler根据框架特性、业务需求选择合适模式
- 空闲Agent不占用额外Pod，任务到来时动态调度资源
- 平台层提供共享PID命名空间支持，便于Handler管理多进程

#### 3.4.2 设计要点

AgentRuntime通过以下设计点提升资源效率：

1. **轻量级Agent Handler**：Handler作为管理进程，资源占用最小化设计
2. **Framework进程共享**：多个Agent共用一个Framework进程，减少进程开销
3. **动态资源调度**：Framework内部根据任务需求动态分配资源给各Agent
4. **Pod级别资源配额**：按Pod而非Agent设置资源配额，灵活调整

**Agent Handler资源效率设计**：

| 设计点 | 说明 |
|--------|------|
| 最小化镜像体积 | Handler镜像精简，减少启动时间和存储占用 |
| 轻量级监控 | 使用共享PID命名空间，避免复杂监控机制 |
| 配置文件监听 | 使用fsnotify而非轮询，减少CPU占用 |
| 事件驱动处理 | Handler仅在事件触发时处理，空闲时无开销 |

### 3.5 Pod容器配置

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
- **TBD**：需要设计一个机制让Agent Handler管理Agent Framework和AI Agents

### 3.6 Sandbox集成设计

AgentRuntime支持与agent-sandbox项目集成，通过Harness引用Sandbox资源。

#### 3.6.1 与agent-sandbox项目集成

利用agent-sandbox项目现有的Sandbox CRD资源体系：

| CRD | 作用 |
|-----|------|
| SandboxTemplate | 沙箱模板，定义Pod规格、网络策略、安全策略 |
| SandboxWarmPool | 预热池，维护预热的Sandbox实例 |
| SandboxClaim | 沙箱申领，获取Sandbox实例 |
| Sandbox | 沙箱实例（Pod） |

#### 3.6.2 Sandbox模式

通过Harness引用已存在的Sandbox/SandboxClaim，支持两种互斥模式：

| 模式 | 说明 | 适用场景 |
|------|------|----------|
| 外部模式 | Sandbox作为独立Pod，AgentRuntime通过API调用 | 多Agent共享Sandbox资源池 |
| 嵌入式模式 | AgentRuntime Pod本身就是Sandbox | Agent需要强隔离执行环境 |

**设计考虑**：
- 外部模式：Sandbox可作为资源池，动态调度，多个Agent共享
- 嵌入式模式：Agent与Sandbox紧耦合，执行环境更可控
- 互斥设计：一个Harness只能选择一种，避免配置混乱

#### 3.6.3 Sandbox资源池动态关联

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
- **TBD**：如何在复用同一个Sandbox时实现AI Agent之间的无缝切换

#### 3.6.4 容器合并策略（嵌入式Sandbox）

当使用嵌入式Sandbox模式时，SandboxTemplate定义Pod基础规格，AgentRuntime的Agent Handler和Agent Framework容器采用**追加模式**合并。

**考虑因素**：
- SandboxTemplate可能已包含代码执行容器、监控sidecar等
- AgentRuntime的Agent Handler和Agent Framework是业务核心容器
- 追加模式保持SandboxTemplate完整性，同时叠加Agent容器

### 3.7 Agent标识管理

一些Agent Framework在运行时会生成内部UUID用于日志和协议交互等。Agent Handler应该建立CRD定义的Agent ID/Name与这些框架特定标识符之间的映射。这种关联确保Agent的所有维度信息可以在整个平台工程生态系统中统一。

### 3.8 CRD结构示例

```yaml
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

  harness:
    model:
      - name: model-deepseek-default   # 模型服务配置
    mcp:
      - name: mcp-registry-default    # MCP Registry配置
    memory:
      - name: redis-memory
    sandbox:
      - name: gvisor-sandbox
    knowledge:
      - name: custom-rag

  agentConfig:                     # 公共配置（所有Agent共享）
    - name: protocol
      configMapRef:
        name: protocol-config

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

AIAgent是独立的业务对象，可以被调度到不同的AgentRuntime上执行。

### 4.1 对象定义与设计考虑

将AIAgent与AgentRuntime解耦以支持动态调度和迁移，这一设计对于资源效率至关重要。

**考虑因素**：
- 一个AgentRuntime可以承载多个AIAgent（N:1关系）
- AIAgent可能需要迁移到不同Runtime（故障、资源调整和整合、维护）
- PVC数据需要跟随AIAgent迁移

### 4.2 Agent ID设计

| 字段 | 来源 | 用途 |
|------|------|------|
| `metadata.name` | 用户指定 | Namespace内唯一，人类可读 |
| `metadata.uid` | K8s自动生成 | 绝对唯一标识 |

**考虑因素**：
- 遵循K8s惯例，用户熟悉
- name便于人类识别和kubectl操作
- uid用于绝对唯一标识，Agent Handler可选择使用哪个

### 4.3 调度模式

采用混合调度模式：

| 用户指定 | Controller行为 |
|----------|----------------|
| `runtimeRef.type: adk` | 自动调度到匹配类型的Runtime |
| `runtimeRef.name: runtime-001` | 直接绑定到指定实例 |
| 不指定 | 默认调度策略 |

**考虑因素**：
- 灵活性：用户可选择自动调度或手动绑定
- 与K8s调度模式一致（类似Pod调度到Node）

### 4.4 迁移支持

| 迁移类型 | 触发条件 |
|----------|----------|
| 主动迁移 | 用户修改`runtimeRef.name` |
| 自动迁移 | Runtime故障、资源不足或整合、维护驱逐 |

**考虑因素**：
- 支持运维操作（例如维护时自动迁移）
- 支持用户主动调整（例如业务需求变化）

### 4.5 PVC迁移

迁移时PVC跟随AIAgent，从旧Pod解绑并挂载到新Pod。

**技术约束**：
- 需要网络存储支持（如NFS、Ceph、Longhorn）
- 云存储（EBS、PD）需detach/attach，有延迟

**考虑因素**：
- 数据一致性：PVC跟随Agent，保证数据不丢失
- 存储后端选择：用户需根据迁移需求选择合适存储

### 4.6 agentConfig设计

agentConfig是Agent/Handler/Framework启动和运行所需的业务配置传递机制，与Harness（平台工程能力）职责分离。

#### 4.6.1 设计理念

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

#### 4.6.2 配置声明方式

**决策**：AgentRuntime声明公共配置（针对所有同类Agent），AIAgent追加Agent专属配置。

**考虑因素**：
- 有些配置对所有同类Agent是一样的（如协议配置）
- 有些配置是Agent特有的（如Prompt内容、受控技能集）
- 公共配置在Runtime级别管理，减少重复配置

#### 4.6.3 文件来源

**决策**：引用外部ConfigMap/Secret。

**考虑因素**：
- 用户预先创建ConfigMap/Secret存放配置文件
- AIAgent和AgentRuntime引用这些外部资源
- 配置内容与CRD分离，便于独立管理和更新
- 遵循K8s惯例（ConfigMap/Secret是配置的标准载体）

#### 4.6.4 挂载路径规范

**决策**：统一挂载路径，按来源分子目录。

```
Pod挂载结构：
/etc/agent-config/
├── runtime/                        # Runtime公共配置
│   ├── protocol/
│   │   └── protocol.yaml
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

#### 4.6.5 更新机制

**决策**：Handler主动监听文件变更。

**考虑因素**：
- Handler使用fsnotify或轮询监听`/etc/agent-config/`目录
- 文件变更时，Handler重新加载配置并更新Agent
- Handler自行决定更新策略（立即生效、等待窗口等）
- 平台层不介入更新逻辑，降低复杂度

#### 4.6.6 文件命名

**决策**：Handler定义配置文件命名规范，避免同名冲突。

**考虑因素**：
- Handler在文档中说明需要哪些配置文件及其命名
- 用户按Handler要求准备不同名称的文件
- 平台层不处理文件冲突，只负责挂载

#### 4.6.7 覆盖行为

**决策**：合并挂载，Handler决定合并逻辑。

**考虑因素**：
- Runtime公共配置和AIAgent配置都挂载到Pod
- Handler知道哪些是公共配置（`runtime/`目录），哪些是专属配置（`agent/`目录）
- Handler自行决定如何合并或覆盖，具有最大灵活性

#### 4.6.8 Runtime动态更新

**决策**：支持动态更新，Handler决定更新方式。

**考虑因素**：
- AgentRuntime的agentConfig修改后，所有相关Agent的Pod都会收到更新
- Handler监听变更并决定如何更新所有Agent
- 平台层负责ConfigMap同步，Handler负责业务逻辑更新

#### 4.6.9 引用范围

**决策**：只能引用同Namespace的ConfigMap/Secret。

**考虑因素**：
- 符合多租户隔离原则
- 跨Namespace引用需要额外RBAC权限，增加安全风险
- 实际用例需要时再考虑扩展

#### 4.6.10 agentConfig设计汇总

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

### 4.7 CRD结构示例

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

  agentConfig:                      # Agent专属配置（追加）
    - name: prompt
      configMapRef:
        name: agent-prompt
    - name: skills
      configMapRef:
        name: agent-skill-set

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

## 5. Harness设计（WIP and TBD）

Harness是AI Agent脚手架能力的独立CRD，定义各种外置能力的配置。

实际上，许多网关和Registry已经存在。当Agent Framework无法直接与它们通信时，Agent Handler介入并通过一致的接口与它们交互。然后，它将这些Harness能力转换为Agent Framework可以理解的格式。例如，它可以从Skill Hub下载技能，供仅处理本地配置的Agent Framework使用，有效地将Skill Hub转变为通用供应中心。Agent Handler本质上充当标准化资源与不同Agent框架特定需求之间的中介。

### 5.1 Harness与agentConfig的概念区分

在深入设计Harness之前，需要明确两个核心概念的区分。

#### 5.1.1 概念定义

| 维度 | Harness | agentConfig |
|------|---------|-------------|
| **定位** | 平台工程能力 | Agent/Handler/Framework配置信息 |
| **示例** | GAIE Gateway、MCP Registry、AgentGateway、Sandbox等 | Prompt、协议配置（A2A）等 |
| **处理方式** | 根据Agent ID进行细粒度的平台级处理 | Handler/Framework启动和运行需要的配置内容 |
| **责任方** | 平台层负责管理和提供 | Handler决定格式和用途 |
| **关注点** | 能力外置化、标准化 | 业务配置、框架特定需求 |

#### 5.1.2 设计考虑

- **Harness是平台工程能力**：这些能力与业务逻辑无关，是平台层提供的通用能力，如Agent Gateway、MCP Registry、Skill Hub等。平台层可以根据Agent ID或Name进行细粒度的控制，例如允许/禁止某个Agent使用某个Sandbox。

- **agentConfig是业务配置**：这些配置是Agent/Handler/Framework启动和运行所需的业务信息，如Prompt内容、通信协议配置、技能定义等。平台层不关心这些配置的具体内容和格式，只负责传递机制。

- **解耦设计**：将平台工程能力与业务配置分离，使得：
  - 平台层专注于提供和管理标准化的工程能力
  - Handler专注于处理框架特定的业务配置
  - 用户可以独立管理两类内容，互不干扰

### 5.2 对象定义

#### 5.2.1 设计考虑

**决策**：
- 独立CRD，便于复用和统一管理。
- 采用Namespace级别，不支持集群级共享。

**考虑因素**：
- 多个AgentRuntime/AIAgent可引用同一个Harness
- 能力配置独立管理，修改无需改动Agent CRD
- Agent Handler需要标准化能力定义，适配不同框架
- 多租户隔离：不同Namespace的配置独立
- 权限控制：Namespace级RBAC
- 遵循K8s惯例（如Role vs ClusterRole）

#### 5.2.2 标准能力支持（后续可扩展）

| 类型 | 说明 |
|------|------|
| model | 模型服务，LLM模型接入配置 |
| mcp | Model Context Protocol，工具/能力接入 |
| skills | 技能模块 |
| knowledge | 知识库/RAG |
| memory | 记忆存储 |
| state | 运行状态 |
| guardrail | 安全护栏 |
| security | 安全策略 |
| policy | 策略控制 |
| sandbox | 执行隔离环境 |

### 5.3 Spec结构

采用每种类型独立spec字段的设计。

以Model类型为例。模型作为平台提供的核心服务能力，通过Harness配置Agent可以访问的模型服务。

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
    defaultModel: deepseek-chat
```

### 5.4 绑定模式

#### 5.4.1 AgentRuntime引用

AgentRuntime引用Harness采用按类型分组：

```yaml
harness:
  model:
    - name: model-deepseek-default
  mcp:
    - name: mcp-registry-default
  sandbox:
    - name: gvisor-sandbox
```

#### 5.4.2 AIAgent定制化

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

AIAgent不能追加AgentRuntime未提供的Harness，只能覆盖或禁止。

**考虑因素**：
- 安全控制：Runtime管理员可限定可用能力范围
- 避免Agent随意扩展能力，突破安全边界

### 5.5 Harness配置传递机制

#### 5.5.1 共享Volume挂载方案

采用共享Volume挂载ConfigMap的方式传递Harness配置给Agent Handler。

```
Pod
├── Agent Handler
│   └── /etc/harness/ (ConfigMap挂载)
└── Agent Framework
│   └── /etc/harness/ (同一Volume)
```

选择共享Volume挂载的原因：
- Agent Handler无需K8s访问权限，降低安全风险
- 配置变更无需重启Pod
- 简单可靠，符合Sidecar惯例

#### 5.5.2 文件Watch机制

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

## 6. 设计目标达成分析

### 6.1 框架无关性

**达成方式**：
- 统一的AgentRuntime Controller，不感知框架细节
- Agent Handler由框架社区提供，负责框架适配
- 标准化Harness定义，Agent Handler统一转换

**效果**：新框架接入只需提供Agent Handler镜像，无需修改平台代码。

### 6.2 能力外置化

**达成方式**：
- Harness独立CRD，可复用
- AgentRuntime引用公共Harness
- AIAgent定制化覆盖，不追加

**效果**：能力配置独立管理，修改无需改动Agent CRD，多个Agent可复用同一Harness。

### 6.3 灵活调度

**达成方式**：
- AIAgent与AgentRuntime解耦
- 混合调度模式（类型自动调度，实例固定绑定）
- PVC跟随迁移

**效果**：Agent可动态迁移到不同Runtime实例，支持装箱整合、自动扩缩容（out/in）、运维维护和负载均衡。

### 6.4 多租户支持

**达成方式**：
- Namespace级Harness
- PVC按AIAgent独立
- Sandbox隔离策略

**效果**：不同Namespace配置独立，数据隔离，安全边界清晰。

### 6.5 安全隔离

**达成方式**：
- Sandbox两种模式（外部/嵌入式）
- 集成agent-sandbox项目安全策略
- Harness不追加约束

**效果**：Runtime管理员可限定能力范围，Agent执行环境可隔离。

---

## 7. 总结

本设计通过多层对象抽象（AIAgent、AgentRuntime、Harness、agentConfig），实现了AI Agent在Kubernetes中的核心资源定义。核心创新点包括：

1. **Agent Handler模式**：平台层Controller统一抽象，框架层Agent Handler具体适配，解耦开发职责
2. **Agent与Runtime分离**：支持动态调度和迁移
3. **灵活的进程映射模式**：支持单进程多Agent和多进程单Agent两种模式，由Agent Handler自行决定
4. **Harness标准化**：平台工程能力外置化管理，继承+覆盖模式定制
5. **agentConfig抽象**：业务配置与平台能力分离，Handler决定格式，平台提供传递机制
6. **Sandbox集成**：复用agent-sandbox项目，支持多种执行环境形态

通过本设计，AI Agent成为Kubernetes中的一等公民，类似Pod的核心抽象，能够适配任何Agent框架，支持复杂业务场景和资源效率目标，同时保持安全隔离和多租户能力。

---

**注：本文所表达的观点不代表作者所属机构的立场。**