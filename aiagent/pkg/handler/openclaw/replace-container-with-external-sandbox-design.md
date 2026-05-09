# 完全替代OpenClaw Container为External Sandbox的设计方案

## 目标

在不修改OpenClaw源代码的前提下，将所有原本在Docker Container内执行的操作转移到External Sandbox服务。

## 核心方案：Hook拦截 + 配置禁用Sandbox

### 方案概述

```
┌─────────────────────────────────────────────────────────────────┐
│          替代Container执行的核心思路                                │
└─────────────────────────────────────────────────────────────────┘

原流程（Docker Container）：
Tool Call → SandboxFsBridge → docker exec → Container → Result

新流程（External Sandbox）：
Tool Call → before_tool_call hook → HTTP → Harness Bridge → External Sandbox → Result
          ↓ block local execution
          ↓ return result in blockReason
```

### 三步实现

#### Step 1: 配置禁用Docker Sandbox

```json
// OpenClaw配置：/etc/agent-config/runtime/openclaw-config.json
{
  "agents": {
    "defaults": {
      "sandbox": {
        "mode": "off"  // 关键：不创建Docker Container
      }
    }
  }
}
```

**效果**：
- OpenClaw不会创建Docker Container
- SandboxContext不会被创建（`resolveSandboxContext`返回null）
- SandboxFsBridge不存在
- 所有工具默认在Gateway本地执行

#### Step 2: Plugin拦截所有工具调用

```typescript
// harness-bridge plugin: index.ts
api.on("before_tool_call", async (event, ctx) => {
  const toolName = event.toolName;
  const params = event.params || {};

  // 拦截所有需要隔离的工具
  const interceptTools = [
    "read",           // 文件读取
    "write",          // 文件写入
    "edit",           // 文件编辑
    "apply_patch",    // 文件patch
    "exec",           // Shell执行
    "process",        // 后台进程
    "browser",        // 浏览器操作（可选）
    "canvas",         // Canvas操作（可选）
    "image"           // 图像处理（可选）
  ];

  if (!interceptTools.includes(toolName)) {
    // 不拦截的工具：memory_search, sessions_*, web_search等
    // 这些工具不需要隔离，在Gateway本地执行
    return;
  }

  // 所有拦截的工具：路由到External Sandbox
  const bridgeUrl = process.env.HARNESS_BRIDGE_URL || "http://localhost:8080";
  const toolEndpoint = bridgeUrl + "/tools/" + encodeURIComponent(toolName);

  try {
    const response = await fetch(toolEndpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Agent-Id': ctx.agentId || 'unknown',
        'X-Session-Key': ctx.sessionKey || '',
        'X-Session-Id': ctx.sessionId || '',
        'X-Run-Id': ctx.runId || '',
        'X-Tool-Call-Id': event.toolCallId || ''
      },
      body: JSON.stringify({
        toolName,
        params,
        toolCallId: event.toolCallId,
        context: {
          agentId: ctx.agentId,
          sessionKey: ctx.sessionKey,
          sessionId: ctx.sessionId,
          workspaceDir: ctx.workspaceDir  // 传递workspace目录
        }
      })
    });

    if (!response.ok) {
      // 失败：block并返回错误
      const errorText = await response.text();
      return {
        block: true,
        blockReason: `External Sandbox execution failed: ${response.status} ${errorText}`
      };
    }

    const result = await response.json();

    // 成功：block本地执行，返回结果
    const resultJson = JSON.stringify({
      tool: toolName,
      output: result.output,
      remote: true,
      sandboxId: result.resourceId || '',
      duration: result.duration || 0,
      exitCode: result.exitCode || 0,
      status: result.status || 'completed'
    });

    return {
      block: true,
      blockReason: `REMOTE_EXECUTION_SUCCESS:${resultJson}`
    };
  } catch (error) {
    // 网络错误：block并返回错误
    return {
      block: true,
      blockReason: `External Sandbox connection error: ${String(error)}`
    };
  }
});
```

#### Step 3: Harness Bridge实现工具路由

```go
// Harness Bridge HTTP Server
func (s *SkillServer) handleToolExecution(ctx context.Context, toolName string, req ToolRequest) (*ToolResult, error) {
    // 根据toolName路由到不同的Harness
    switch toolName {
    case "read", "write", "edit", "apply_patch":
        // 文件操作：路由到SandboxHarness
        sandbox := s.harnessManager.GetSandboxHarness()
        if sandbox == nil {
            return nil, fmt.Errorf("no sandbox harness configured")
        }
        
        // 调用External Sandbox Service的文件操作API
        result, err := sandbox.ExecuteFileOperation(ctx, &FileOperationRequest{
            ToolName:    toolName,
            Params:      req.Params,
            WorkspaceID: req.Context.WorkspaceDir,  // workspace标识
            SessionKey:  req.Context.SessionKey,
        })
        return result, err
        
    case "exec", "process":
        // Shell执行：路由到SandboxHarness
        sandbox := s.harnessManager.GetSandboxHarness()
        if sandbox == nil {
            return nil, fmt.Errorf("no sandbox harness configured")
        }
        
        // 调用External Sandbox Service的Shell执行API
        result, err := sandbox.ExecuteShellCommand(ctx, &ShellCommandRequest{
            Command:     req.Params["command"].(string),
            Workdir:     req.Params["workdir"].(string),
            Env:         req.Params["env"].(map[string]string),
            Timeout:     req.Params["timeout"].(int),
            SessionKey:  req.Context.SessionKey,
        })
        return result, err
        
    case "browser", "canvas":
        // 可选：路由到专门的BrowserHarness
        browserHarness := s.harnessManager.GetBrowserHarness()
        if browserHarness != nil {
            return browserHarness.Execute(ctx, toolName, req.Params)
        }
        // 如果没有BrowserHarness，返回错误
        return nil, fmt.Errorf("browser operations not supported")
        
    default:
        return nil, fmt.Errorf("unknown tool: %s", toolName)
    }
}
```

---

## 完整架构对比

### 原架构（Docker Container）

```
┌─────────────────────────────────────────────────────────────────┐
│                OpenClaw + Docker Sandbox架构                      │
└─────────────────────────────────────────────────────────────────┘

Gateway Host
  ├── OpenClaw Gateway Process
  │   ├── Agent Execution Engine
  │   ├── Tool Handlers
  │   ├── Sandbox Manager
  │   └── SandboxFsBridge
  │       ↓ calls docker exec
  │       ↓
  │   Docker Daemon
  │       ├── create sandbox container
  │       ├── exec commands in container
  │       └── manage container lifecycle
  │       ↓
  │   Sandbox Container (debian:bookworm-slim)
  │       ├── mounted workspace
  │       ├── shell execution
  │       ├── file operations
  │       └── isolation constraints
  │
  └── Host Filesystem
      ├── ~/.openclaw/ (Gateway数据)
      └── ~/workspace/ (挂载到Container)
```

### 新架构（External Sandbox）

```
┌─────────────────────────────────────────────────────────────────┐
│              OpenClaw + External Sandbox架构                      │
└─────────────────────────────────────────────────────────────────┘

Gateway Host (OpenClaw Gateway)
  ├── OpenClaw Gateway Process
  │   ├── Agent Execution Engine
  │   ├── Tool Handlers
  │   ├── Plugin: harness-bridge
  │   │   ├── before_tool_call hook (拦截所有工具)
  │   │   └ routes to Harness Bridge
  │   └ NO Sandbox Manager (mode=off)
  │   └ NO SandboxFsBridge
  │   └ NO Docker Container
  │
  ├── Harness Bridge HTTP Server (Go)
  │   ├── /skills/{name} endpoint
  │   ├── /tools/{name} endpoint
  │   ├── HarnessManager
  │   │   ├── SkillsHarness
  │   │   ├── SandboxHarness (External)
  │   │   ├── ModelHarness
  │   │   ├── MCPHarness
  │   │   └ MemoryHarness
  │   │   └── KnowledgeHarness
  │   └
  │   └ NO Docker Daemon involved
  │
  └── Host Filesystem
      ├── ~/.openclaw/ (Gateway数据：Cron/Memory/Session)
      └ NO ~/workspace/ mount (数据在External Sandbox)

External Sandbox Service (Remote Server)
  ├── Sandbox API Server
  │   ├── /file/read
  │   ├── /file/write
  │   ├── /file/edit
  │   ├── /shell/exec
  │   └── /process/manage
  │
  ├── Workspace Storage (持久化或临时)
  │   ├── per-session workspace
  │   ├── file system isolation
  │   └── data persistence
  │
  ├── Execution Engine
  │   ├── Shell execution (安全隔离)
  │   ├── File operations (权限控制)
  │   ├── Process management
  │   └── Resource limits (CPU/Memory)
  │
  └── Security Layer
      ├── Input validation
      ├── Permission checks
      ├── Audit logging
      └── Network isolation (可选)
```

---

## 关键问题分析

### Q1: OpenClaw源代码需要修改吗？

**答案：不需要！**

| 方面 | 是否修改 | 实现方式 |
|------|---------|---------|
| **Sandbox Manager** | ❌ 不修改 | 配置`mode=off`禁用 |
| **SandboxFsBridge** | ❌ 不修改 | 不创建（因为mode=off） |
| **Tool Handlers** | ❌ 不修改 | 保持原样 |
| **Plugin System** | ❌ 不修改 | 使用已有的hook机制 |
| **Gateway Config** | ❌ 不修改 | 只是配置项调整 |

### Q2: Cron/Memory/Session如何处理？

**答案：保持在Gateway本地，不转移到External Sandbox**

| 功能 | 处理方式 | 原因 |
|------|---------|------|
| **Cron Scheduler** | Gateway本地 | 生命周期持久，需要Gateway API |
| **Memory Backend** | Gateway本地 | 数据库在Host，需要全局访问 |
| **Session Storage** | Gateway本地 | 会话记录持久化 |
| **Credentials** | Gateway本地 | 安全敏感，不应远程 |
| **Plugin Loading** | Gateway本地 | 插件在Host执行 |

**为什么不移到External Sandbox？**
1. **数据持久化要求**：Cron/Memory需要长期保存，External Sandbox可能重启
2. **Gateway API依赖**：Cron需要触发Agent，Memory需要搜索本地数据
3. **安全考虑**：Credentials不应远程传输
4. **架构合理性**：Gateway负责管理，External Sandbox负责执行隔离

### Q3: 数据持久化如何处理？

**答案：External Sandbox提供独立的Workspace存储**

```
┌─────────────────────────────────────────────────────────────────┐
│              Workspace数据存储方案                                 │
└─────────────────────────────────────────────────────────────────┘

方案A：External Sandbox持久化存储
  External Sandbox Service
    ├── Workspace Storage (持久化)
    │   ├── workspace-{sessionKey}/
    │   │   ├── file1.txt
    │   │   ├── file2.py
    │   │   └── directory/
    │   ├── workspace-{sessionKey-2}/
    │   └── workspace-shared/
    │
    └── Workspace Manager
        ├── create workspace (session start)
        ├── cleanup workspace (session end or timeout)
        ├── sync workspace (可选：备份到Gateway)
        └── access control (per-session isolation)

方案B：Gateway + External Sandbox双存储
  Gateway Host
    ├── workspace-{sessionKey}/ (本地缓存)
    └   (可选：备份重要文件)
  
  External Sandbox Service
    ├── workspace-{sessionKey}/ (执行环境)
    └── sync with Gateway (可选)

方案C：临时Workspace（每次创建）
  External Sandbox Service
    ├── workspace-temp-{executionId}/
       (每次执行创建临时workspace)
    └── cleanup after execution
    └── no persistence (适合纯执行任务)
```

**推荐方案A**（持久化存储）：
- Workspace按Session隔离
- External Sandbox管理生命周期
- 可配置cleanup策略（idle timeout、max age）
- 支持跨Session共享workspace（shared scope）

### Q4: 与原Docker Sandbox的差异

| 方面 | Docker Container | External Sandbox | 影响 |
|------|------------------|------------------|------|
| **创建速度** | 毫秒级（本地） | 秒级（HTTP往返） | ⚠️ 略慢 |
| **资源隔离** | Docker原生隔离 | External Sandbox实现 | ⚠️ 需要自己实现 |
| **网络隔离** | `--network none` | External Sandbox控制 | ✅ 可实现 |
| **数据持久化** | 挂载Host目录 | External Sandbox存储 | ⚠️ 需要管理 |
| **扩展性** | 单机限制 | 分布式扩展 | ✅ 更灵活 |
| **安全性** | Docker安全机制 | External Sandbox安全机制 | ⚠️ 需要自己实现 |
| **调试难度** | Docker logs | External Sandbox logs | ⚠️ 需要远程调试 |
| **成本** | 本地资源 | 远程资源 + HTTP成本 | ⚠️ 网络开销 |

---

## External Sandbox服务设计

### 服务接口定义

```yaml
# External Sandbox API Specification

# 1. 文件操作
POST /file/read
  Request: {path: "/workspace/file.txt", workspaceID: "session-xxx"}
  Response: {content: "file content", mimeType: "text/plain", size: 1024}

POST /file/write
  Request: {path: "/workspace/new.txt", content: "data", workspaceID: "session-xxx"}
  Response: {success: true, size: 1024}

POST /file/edit
  Request: {path: "/workspace/file.txt", edits: [{oldText: "old", newText: "new"}]}
  Response: {success: true, modified: 3}

POST /file/apply_patch
  Request: {path: "/workspace/file.txt", patch: "--- a/file\n+++ b/file\n..."}
  Response: {success: true}

# 2. Shell执行
POST /shell/exec
  Request: {
    command: "python script.py",
    workdir: "/workspace",
    env: {"PATH": "/usr/bin"},
    timeout: 60,
    workspaceID: "session-xxx"
  }
  Response: {
    output: "execution output",
    exitCode: 0,
    duration: 1200,
    status: "completed"
  }

POST /shell/process
  Request: {
    action: "start",
    command: "long-running-task",
    background: true,
    workspaceID: "session-xxx"
  }
  Response: {
    sessionId: "process-xxx",
    pid: 12345,
    status: "running"
  }

# 3. Workspace管理
POST /workspace/create
  Request: {sessionKey: "session-xxx", scope: "session"}
  Response: {workspaceID: "session-xxx", path: "/workspace/session-xxx"}

POST /workspace/cleanup
  Request: {workspaceID: "session-xxx"}
  Response: {success: true}

GET /workspace/status
  Request: {workspaceID: "session-xxx"}
  Response: {
    size: 10240,
    files: 15,
    lastAccessed: "2024-01-01T00:00:00Z",
    status: "active"
  }
```

### SandboxHarness实现

```go
// SandboxHarness: 路径到External Sandbox
type SandboxHarness struct {
    mode      SandboxMode  // "external"
    endpoint  string       // External Sandbox API URL
    client    *http.Client
    workspaceManager *WorkspaceManager
}

func (h *SandboxHarness) ExecuteFileOperation(ctx context.Context, req *FileOperationRequest) (*ToolResult, error) {
    endpoint := h.endpoint + "/file/" + req.ToolName
    
    httpReq := map[string]interface{}{
        "path":        req.Params["path"],
        "workspaceID": req.WorkspaceID,
    }
    
    // 添加tool特定参数
    switch req.ToolName {
    case "write":
        httpReq["content"] = req.Params["content"]
    case "edit":
        httpReq["edits"] = req.Params["edits"]
    case "apply_patch":
        httpReq["patch"] = req.Params["patch"]
    }
    
    // HTTP POST
    resp, err := h.client.PostJSON(ctx, endpoint, httpReq)
    if err != nil {
        return nil, fmt.Errorf("External Sandbox request failed: %w", err)
    }
    
    // 解析响应
    result := &ToolResult{
        Output:     resp["content"],
        Status:     "completed",
        Duration:   resp["duration"].(int64),
        ResourceID: req.WorkspaceID,  // workspace作为resource标识
    }
    
    return result, nil
}

func (h *SandboxHarness) ExecuteShellCommand(ctx context.Context, req *ShellCommandRequest) (*ToolResult, error) {
    endpoint := h.endpoint + "/shell/exec"
    
    httpReq := map[string]interface{}{
        "command":     req.Command,
        "workdir":     req.Workdir,
        "env":         req.Env,
        "timeout":     req.Timeout,
        "workspaceID": req.WorkspaceID,
    }
    
    resp, err := h.client.PostJSON(ctx, endpoint, httpReq)
    if err != nil {
        return nil, fmt.Errorf("External Sandbox shell execution failed: %w", err)
    }
    
    result := &ToolResult{
        Output:     resp["output"],
        ExitCode:   resp["exitCode"].(int),
        Status:     resp["status"].(string),
        Duration:   resp["duration"].(int64),
        ResourceID: req.WorkspaceID,
    }
    
    return result, nil
}
```

---

## 实施步骤

### Phase 1: 配置OpenClaw

```bash
# 1. 配置禁用Docker Sandbox
cat > /etc/agent-config/runtime/openclaw-config.json << 'EOF'
{
  "agents": {
    "defaults": {
      "sandbox": {
        "mode": "off"
      }
    }
  },
  "plugins": {
    "enabled": true,
    "load": {
      "paths": ["/etc/aiagent/plugins/harness-bridge"]
    }
  }
}
EOF

# 2. 启动OpenClaw Gateway（不需要Docker）
openclaw gateway run --config /etc/agent-config/runtime/openclaw-config.json
```

### Phase 2: 生成harness-bridge Plugin

```go
// 使用plugin_generator.go生成
pluginCfg := &HarnessBridgePluginConfig{
    BridgeURL: "http://localhost:8080",
    PluginDir: "/etc/aiagent/plugins/harness-bridge",
    Skills:    []string{"weather", "calculator"},
    InterceptAllTools: true,  // 新增：拦截所有工具
}

GenerateHarnessBridgePlugin(ctx, pluginCfg)
```

### Phase 3: 启动Harness Bridge

```bash
# Harness Bridge HTTP Server (Gateway Host)
harness-bridge-server \
  --port 8080 \
  --harness-config /etc/aiagent/harness-config.yaml \
  --workspace-storage /var/lib/aiagent/workspaces
```

### Phase 4: 配置External Sandbox Service

```yaml
# External Sandbox配置
external_sandbox:
  endpoint: http://sandbox-service.example.com:9000
  auth:
    type: api_key
    key: ${SANDBOX_API_KEY}
  workspace:
    storage: /var/lib/sandbox/workspaces
    cleanup:
      idle_hours: 24
      max_age_days: 7
  security:
    allowed_paths:
      - /workspace/**
    denied_commands:
      - rm -rf /
      - dd if=/dev/zero
    resource_limits:
      memory: 512MB
      cpu: 1
      timeout: 300s
```

---

## 与原有方案的对比

### 原方案（混合模式）

| 组件 | 执行位置 | 说明 |
|------|---------|------|
| Skills | External Sandbox | 通过harness_bridge tool |
| read/write/edit/exec | Docker Container | SandboxFsBridge |
| Cron/Memory | Gateway Host | OpenClaw核心 |

### 新方案（完全External）

| 组件 | 执行位置 | 说明 |
|------|---------|------|
| Skills | External Sandbox | 通过harness_bridge tool |
| read/write/edit/exec | External Sandbox | 通过before_tool_call hook |
| Cron/Memory | Gateway Host | 保持不变 |

---

## 优势与限制

### 优势

| 方面 | Docker Container | External Sandbox |
|------|------------------|------------------|
| **分布式执行** | ❌ 单机 | ✅ 多节点 |
| **资源弹性** | ❌ 固定Host资源 | ✅ 动态分配 |
| **统一管理** | ❌ 每个Gateway独立 | ✅ 集中管理所有Sandbox |
| **扩展性** | ❌ Docker限制 | ✅ 可扩展更多服务 |
| **安全性** | ⚠️ Docker隔离 | ✅ 更灵活的安全策略 |
| **跨环境** | ❌ 需要Docker安装 | ✅ HTTP即可，无依赖 |

### 限制

| 方面 | 影响 | 解决方案 |
|------|------|---------|
| **网络延迟** | ⚠️ HTTP往返延迟 | 缓存、预连接、CDN |
| **数据同步** | ⚠️ Workspace跨服务 | Workspace Manager同步 |
| **调试难度** | ⚠️ 远程调试 | 统一日志、监控 |
| **实现成本** | ⚠️ 需要实现External Sandbox | 使用现有SaaS服务 |
| **单点故障** | ⚠️ External Sandbox不可用 | 多实例、failover |

---

## 总结

**完全可行！无需修改OpenClaw源代码。**

| 方面 | 实现方式 | 是否修改源代码 |
|------|---------|---------------|
| **禁用Docker** | 配置`mode=off` | ❌ NO |
| **拦截工具** | Plugin `before_tool_call` hook | ❌ NO |
| **路由到远程** | HTTP POST to Harness Bridge | ❌ NO |
| **返回结果** | blockReason嵌入JSON | ❌ NO |
| **数据持久化** | External Sandbox Workspace | ❌ NO |

**关键配置**：
```json
{
  "agents.defaults.sandbox.mode": "off",
  "plugins.enabled": true,
  "plugins.load.paths": ["/etc/aiagent/plugins/harness-bridge"]
}
```

**唯一限制**：Cron/Memory/Session保持在Gateway本地，不转移到External Sandbox（架构合理性）。

**推荐场景**：
- 多Gateway部署（统一External Sandbox）
- Kubernetes环境（External Sandbox作为独立服务）
- 安全合规要求（更严格的隔离）
- 资源弹性需求（动态分配Sandbox资源）