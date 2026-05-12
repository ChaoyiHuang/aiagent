# Handler架构重构 - 进度总结

## 已完成的重构 (2026-05-12 验证)

### E2E测试通过状态

| 测试场景 | 验证内容 | 结果 |
|---------|---------|------|
| **ADK Shared模式** | 2个AIAgent → 1个Framework进程 | ✓ 通过 |
| **ADK Isolated模式** | 3个AIAgent → 3个Framework进程 | ✓ 通过 |
| **OpenClaw Gateway模式** | 2个AIAgent → 2个Gateway进程 | ✓ 通过 |

---

## 最终实现架构

### ImageVolume模式 (已验证)

采用Kubernetes 1.35+的ImageVolume特性，Handler容器直接访问Framework镜像内容：

```
Pod (AgentRuntime)
├── Handler Container (Process Manager)
│   ├── VolumeMounts:
│   │   ├── /framework-rootfs -> ImageVolume (Framework image)
│   │   ├── /etc/harness/<name> -> Harness ConfigMaps
│   │   ├── /shared/workdir -> EmptyDir (agent workspace)
│   │   └── /etc/agent-config -> hostPath (Config Daemon)
│   │
│   └── Handler启动Framework进程:
│       ADK: exec.Command("/framework-rootfs/adk-framework")
│       OpenClaw: exec.Command("/framework-rootfs/usr/local/bin/openclaw")
│
└── Framework Container (DUMMY)
│   └── ENTRYPOINT: ["sleep", "infinity"]
│   └── Provides image content for ImageVolume
│
└── ShareProcessNamespace: true
```

### Config Daemon (Solution M, 已实现)

Config Daemon作为DaemonSet运行，监控AIAgent CRD并同步配置到hostPath：

```
Config Daemon (DaemonSet)
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

**优势**:
- Handler不需要K8s API访问权限
- 无需RBAC配置
- 与ShareProcessNamespace模式兼容
- 支持动态Agent添加

### adk-go库集成 (已实现)

adk-framework直接集成adk-go库进行真实的Agent执行：

```go
import (
    adkagent "google.golang.org/adk/agent"
    "google.golang.org/adk/agent/llmagent"
    "google.golang.org/adk/runner"
    "google.golang.org/adk/session"
)

// 创建Agent
agent, err := llmagent.New(llmagent.Config{
    Name:        config.Name,
    Model:       customModel,  // 实现model.LLM接口
    Description: config.Description,
    Instruction: instruction,
})

// 通过Runner执行
r, err := runner.New(runner.Config{
    Agent:           rootAgent,
    SessionService:  session.InMemoryService(),
    AutoCreateSession: true,
})

for event, err := range r.Run(ctx, userID, sessionID, msg, runConfig) {
    // 处理事件流
}
```

**JSON-RPC方法**:
- `agent.run`: 执行Agent并返回事件流
- `agent.status`: 查询Agent状态
- `agent.list`: 列出所有Agent
- `framework.status`: Framework健康信息

---

## 进程映射模式对比

### Shared模式 (单进程多Agent)

```
AgentRuntime Pod
┌─────────────────────────────────────┐
│ Handler Container                    │
│ ┌─────────────────────────────────┐ │
│ │ Handler Process (Go)            │ │
│ │ ├── 监控单个Framework进程        │ │
│ │ └── JSON-RPC通信                │ │
│ └─────────────────────────────────┘ │
│                                     │
│ Framework Container (DUMMY)         │
│ ┌─────────────────────────────────┐ │
│ │ Framework Process               │ │
│ │ ├── AIAgent-1 (内存中)          │ │
│ │ ├── AIAgent-2 (内存中)          │ │
│ │ └── adk-go Runner执行           │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

**特点**:
- 资源效率最高
- 进程内共享状态
- Framework内部实现Agent路由

### Isolated模式 (多进程单Agent)

```
AgentRuntime Pod
┌─────────────────────────────────────┐
│ Handler Container                    │
│ ┌─────────────────────────────────┐ │
│ │ Handler Process (Go)            │ │
│ │ ├── 监控多个Framework进程        │ │
│ │ ├── 进程生命周期管理             │ │
│ │ └── JSON-RPC通信 (多连接)       │ │
│ └─────────────────────────────────┘ │
│                                     │
│ Framework Container (DUMMY)         │
│ ┌─────────────────────────────────┐ │
│ │ Framework进程-1 ──► AIAgent-1   │ │
│ │ Framework进程-2 ──► AIAgent-2   │ │
│ │ Framework进程-3 ──► AIAgent-3   │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

**特点**:
- 进程级隔离
- 单Agent故障不影响其他
- 更强的资源隔离

---

## 核心实现文件

### Controller层

| 文件 | 功能 |
|------|------|
| `pkg/controller/agentruntime_controller.go` | Pod创建, ImageVolume配置, Harness解析 |
| `pkg/controller/aigent_controller.go` | AIAgent调度, AgentIndex更新 |

### Handler层

| 文件 | 功能 |
|------|------|
| `pkg/handler/handler.go` | Handler接口定义 |
| `pkg/handler/adk/handler.go` | ADK Handler实现 |
| `pkg/handler/adk/converter.go` | AIAgentSpec → ADK YAML转换 |
| `pkg/handler/openclaw/handler.go` | OpenClaw Handler实现 |

### Framework层

| 文件 | 功能 |
|------|------|
| `cmd/adk-framework/main.go` | adk-go集成, JSON-RPC服务 |
| `cmd/adk-handler/main.go` | Handler进程管理 |
| `cmd/config-daemon/main.go` | Config Daemon实现 |

### Docker镜像

| Dockerfile | 说明 |
|------------|------|
| `Dockerfile.adk-framework` | Framework镜像 (golang:1.25-alpine) |
| `Dockerfile.adk-handler` | Handler镜像 |
| `Dockerfile.config-daemon` | Config Daemon镜像 |
| `Dockerfile.manager` | Controller Manager镜像 |
| `Dockerfile.openclaw-handler` | OpenClaw Handler镜像 |

---

## 设计决策回顾

### 1. 去掉Registry (已完成)
- Handler根据`--framework` flag直接创建
- 不再使用registry.Get()
- 简化Handler接口

### 2. ImageVolume模式 (已验证)
- 使用K8s 1.35+ ImageVolume特性
- Handler访问Framework完整文件系统
- 无需二进制复制或init容器
- 独立镜像发布

### 3. Config Daemon (已实现)
- Solution M: hostPath + DaemonSet
- Handler无需K8s API权限
- 支持动态Agent添加
- 与ShareProcessNamespace兼容

### 4. adk-go集成 (已实现)
- 本地replace directive导入adk-go
- llmagent.New()创建真实Agent
- 自定义模型支持OpenAI兼容API
- JSON-RPC通信协议

---

## 架构原则

### Handler职责
1. **单一Framework支持**: ADK Handler只支持ADK-Go
2. **进程管理**: 启动/监控/停止Framework进程
3. **配置转换**: AIAgentSpec → Framework特定格式
4. **生命周期**: Load/Start/Stop Agent

### Framework职责
1. **Agent执行**: 实际的agent.Run()执行
2. **状态管理**: Session, State管理
3. **通信服务**: JSON-RPC/HTTP接口

### Controller职责
1. **Pod创建**: ImageVolume + ShareProcessNamespace配置
2. **Harness解析**: 创建ConfigMap挂载
3. **调度管理**: AIAgent → AgentRuntime调度

---

## 总结

核心重构已完成并通过E2E测试验证。ImageVolume模式和Config Daemon解决方案均已实现，adk-go库集成使Agent可以执行真实的LLM对话。所有3种进程映射模式（ADK Shared、ADK Isolated、OpenClaw Gateway）均通过测试。

**验证状态**: ✓ 所有E2E测试通过