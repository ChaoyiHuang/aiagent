# Handler架构重构 - 进度总结

## 已完成的重构

### 1. 去掉Registry
- **删除** `pkg/handler/registry.go`
- **修改** `pkg/handler/handler.go` 简化Handler接口
  - 去掉 `SpawnFrameworkProcess`、`KillProcess`、`ListProcesses` 等进程管理方法
  - 新增 `Connect(ctx, endpoint)` - 连接到Framework Container
  - 新增 `Disconnect(ctx)` - 断开连接
  - 新增 `IsConnected()` - 检查连接状态
  - 新增 `SupportsIsolatedMode()` - 检查是否支持跨容器模式

### 2. Handler直接创建（无Registry）
- **修改** `cmd/handler/main.go`
  - 根据 `--framework` flag 直接创建对应的Handler
  - 不再使用 registry.Get("adk-go")
  - 添加 `--isolated` flag 支持 IsolatedMode
  - 添加 `--endpoint` flag 指定 Framework Container 地址
  - 添加 `--socket` flag 指定 Unix socket 路径

```go
// 新创建Handler方式（无Registry）
switch framework {
case "adk-go", "adk":
    return adk.NewADKHandler(cfg)
case "openclaw":
    return openclaw.NewOpenClawHandler(cfg)
default:
    return nil
}
```

### 3. Framework Container gRPC服务
- **新增** `pkg/framework/proto/agent.proto` - Protobuf定义
- **新增** `pkg/framework/proto/agent.pb.go` - Protobuf生成代码（简化版）
- **新增** `pkg/framework/service/agent_service.go` - gRPC服务实现
- **新增** `pkg/framework/client/grpc_client.go` - gRPC客户端
- **新增** `cmd/framework/main.go` - Framework Container入口

### 4. ADK Handler重构
- **修改** `pkg/handler/adk/adapter.go`
  - 支持 SharedMode（Handler和Framework同容器）
  - 支持 IsolatedMode（Handler和Framework分离）
  - gRPC Client连接 Framework Container
  - `loadAgentRemote()` 通过gRPC创建远程agent
  - `RemoteAgentWrapper` 包装远程agent

### 5. OpenClaw Handler重构
- **修改** `pkg/handler/openclaw/adapter.go`
  - 实现 `Connect()` - HTTP通信连接
  - 实现 `Disconnect()` - 断开HTTP通信
  - 添加 `SetRemoteEndpoint()` 到 Bridge

### 6. 运行模式对比

| 特性 | SharedMode | IsolatedMode |
|------|------------|--------------|
| Handler位置 | 同容器 | 独立容器 |
| Framework位置 | 同容器 | 独立容器 |
| 通信方式 | 直接Go调用 | gRPC/HTTP |
| Agent实例 | 内存map | 远程引用 |
| 部署复杂度 | 低 | 高 |
| 隔离性 | 低 | 高 |
| 适用场景 | ADK原生 | OpenClaw/多Framework |

## 部署架构图

### SharedMode（ADK-Go默认）
```
AgentRuntime Pod
┌─────────────────────────────────────┐
│ Handler Container                    │
│ ┌─────────────────────────────────┐ │
│ │ Handler Process (Go)            │ │
│ │ ├── ADKHandler                   │ │
│ │ │   ├── agents map (本地)        │ │
│ │ │   │   ├── agent-1 (wrapper)   │ │
│ │ │   │   └── agent-2 (wrapper)   │ │
│ │ │   └── Run() → 直接调用         │ │
│ │ └─────────────────────────────┘ │
│                                     │
│ /etc/agent-config/ ← ConfigMap      │
│ /etc/harness/ ← Harness ConfigMap   │
└─────────────────────────────────────┘
```

### IsolatedMode（跨容器）
```
AgentRuntime Pod
┌────────────────────┐  ┌────────────────────┐
│ Handler Container  │  │ Framework Container │
│                    │  │                     │
│ Handler Process    │  │ Framework Process   │
│ ├── ADKHandler     │  │ ├── gRPC Server     │
│ │   ├── gRPC Client│  │ │   └── AgentService │
│ │   └── localAgents│  │ │       ├── agent-1 │
│ │       (ID refs)  │  │ │       └── agent-2 │
│ │                  │  │ │                    │
│ │ Connect() ───────┼──┼─→ unix:///var/run/  │
│ │   gRPC calls ───┼──┼─→ CreateAgent()      │
│ │                  │  │ │   StartAgent()    │
│ │                  │  │ │   RunAgent()      │
└────────────────────┘  └────────────────────┘
         ↑                        ↑
    /var/run/agent.sock ← EmptyDir Volume
    /etc/agent-config/ ← ConfigMap (共享)
```

## 剩余编译错误

### 当前编译状态
```
# aiagent/pkg/framework/service
pkg/framework/service/agent_service.go:379:76: undefined: agent.NewState

# aiagent/pkg/handler/adk
pkg/handler/adk/adapter.go:511:26: undefined: proto
pkg/handler/adk/adapter.go:516:13: undefined: v1.AgentPhaseInitializing
pkg/handler/adk/adapter.go:522:13: undefined: v1.AgentPhaseError

# aiagent/pkg/handler/adk
pkg/handler/adk/loader.go:116:12: wrapper.AddSubAgentToWrapper undefined
```

### 需要修复的问题
1. `agent.NewState()` 未定义 - 需要检查 `pkg/agent/state.go`
2. `v1.AgentPhaseInitializing`、`v1.AgentPhaseError` 未定义 - 需要检查 `api/v1/aigent_types.go`
3. `proto` 包未导入到 adk adapter.go
4. `AddSubAgentToWrapper` 方法未定义

## 下一步工作

### 立即需要完成
1. 修复编译错误（约10-20行代码修改）
2. 运行单元测试验证重构
3. 创建 Dockerfile.framework 用于 Framework Container

### 短期完善
1. 完整的protobuf生成（使用protoc而不是手动）
2. OpenClaw HTTP客户端实现（IsolatedMode）
3. E2E测试验证跨容器通信
4. Kind部署脚本更新（支持双容器模式）

### 文件清单

**新增文件**：
- `pkg/framework/proto/agent.proto`
- `pkg/framework/proto/agent.pb.go`
- `pkg/framework/service/agent_service.go`
- `pkg/framework/client/grpc_client.go`
- `cmd/framework/main.go`

**修改文件**：
- `pkg/handler/handler.go` - Handler接口重构
- `pkg/handler/adk/adapter.go` - 支持IsolatedMode
- `pkg/handler/openclaw/adapter.go` - 支持Connect/Disconnect
- `pkg/handler/openclaw/bridge.go` - 添加SetRemoteEndpoint
- `cmd/handler/main.go` - 直接创建Handler，无Registry
- `pkg/runtime/registry.go` - 简化HandlerRequirements

**删除文件**：
- `pkg/handler/registry.go` - 不再需要Registry

## 架构原则

### Handler职责
1. **只支持一种Framework**：ADK Handler只支持ADK-Go，OpenClaw Handler只支持OpenClaw
2. **两种运行模式**：SharedMode（本地）或 IsolatedMode（远程）
3. **统一接口**：LoadAgent、StartAgent、StopAgent 对用户透明
4. **配置转换**：ConvertConfig、ConvertHarness 转换为框架特定格式

### Framework Container职责
1. **运行Agent实例**：实际的agent.Run()执行
2. **状态管理**：Session、State管理
3. **gRPC服务**：暴露统一的AgentService接口
4. **生命周期管理**：Create、Start、Stop、Delete

### 跨容器通信协议
- **ADK-Go**: gRPC over Unix socket（同Pod）或 TCP（远程）
- **OpenClaw**: HTTP/WebSocket（stdin/stdout无法跨容器）

## 总结

核心重构已完成，去掉了Registry，每个Handler只支持自己的Framework。跨容器通信机制的基础架构已建立（gRPC服务端、客户端），剩余工作主要是修复少量编译错误和完成测试验证。