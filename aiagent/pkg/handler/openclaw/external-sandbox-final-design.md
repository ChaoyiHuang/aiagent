# OpenClaw External Sandbox Integration - Final Design

## Design Philosophy

**与原OpenClaw Docker Sandbox设计理念一致：只隔离需要隔离的部分，Gateway管理功能保持在本地。**

---

## Tool执行位置分类

### External Sandbox执行（需要隔离）

| Tool | Category | 原因 |
|------|----------|------|
| `read` | 文件操作 | 数据隔离、workspace隔离 |
| `write` | 文件操作 | 数据隔离、防止篡改 |
| `edit` | 文件操作 | 数据隔离、防止篡改 |
| `apply_patch` | 文件操作 | 数据隔离、防止篡改 |
| `exec` | Shell执行 | 代码隔离、安全限制 |
| `process` | 后台进程 | 代码隔离、资源限制 |

### Gateway本地执行（不需要隔离）

| Tool | Category | 原因 |
|------|----------|------|
| `memory_search` | Memory | Gateway本地数据库 |
| `memory_get` | Memory | Gateway本地文件 |
| `sessions_list` | Session | Gateway管理功能 |
| `sessions_spawn` | Session | Gateway管理功能 |
| `sessions_send` | Session | Gateway管理功能 |
| `subagents` | Session | Gateway管理功能 |
| `web_search` | Web | 安全、无隔离需求 |
| `web_fetch` | Web | 安全、无隔离需求 |
| `cron` | Cron | Gateway核心功能 |
| `gateway` | Gateway | Gateway管理 |
| `nodes` | Nodes | Gateway管理 |
| `image` | Media | 可本地处理 |
| `tts` | Media | 可本地处理 |

---

## 架构设计（简化版）

### 核心原则

```
┌─────────────────────────────────────────────────────────────────┐
│          简化架构：Plugin直接调用External Sandbox                 │
└─────────────────────────────────────────────────────────────────┘

关键原则：
1. Plugin直接调用External Sandbox API（不转发）
2. External Sandbox只处理文件操作和Shell执行
3. Gateway本地处理Memory、Session、Web、Cron
4. 与原OpenClaw Docker Sandbox设计一致
```

### 架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                OpenClaw + External Sandbox架构                    │
└─────────────────────────────────────────────────────────────────┘

Gateway Host
  ├── OpenClaw Gateway Process
  │   ├── Agent Execution Engine
  │   │   ├── LLM API调用
  │   │   ├── Session管理
  │   │   └── Tool调度
  │   │
  │   ├── Tool Handlers (本地)
  │   │   ├── memory_search → Memory Manager → Gateway本地数据库
  │   │   ├── sessions_* → Session Manager → Gateway本地
  │   │   ├── web_search/web_fetch → Web Client → Gateway本地
  │   │   ├── cron → Cron Scheduler → Gateway本地
  │   │   └── gateway → Gateway Manager → Gateway本地
  │   │
  │   ├── Plugin: harness-bridge
  │   │   ├── before_tool_call hook
  │   │   │   ├── Intercept: read/write/edit/exec/process
  │   │   │   ├── Direct HTTP POST → External Sandbox
  │   │   │   ├── Return result in blockReason
  │   │   │   └── Pass-through: memory/sessions/web/cron → Gateway本地
  │   │   │
  │   │   └ harness_bridge tool
  │   │   │   ├── Direct HTTP POST → External Sandbox /skills/{name}
  │   │   │   └── Return result
  │   │   │
  │   │   └── Initialization
  │   │       ├── Read config from environment
  │   │       ├── EXTERNAL_SANDBOX_URL
  │   │       ├── EXTERNAL_SANDBOX_API_KEY
  │   │       └── EXTERNAL_SANDBOX_WORKSPACE
  │   │
  │   ├── NO Docker Container (mode=off)
  │   └── NO SandboxFsBridge
  │
  └── Host Filesystem
      ├── ~/.openclaw/
      │   ├── sessions/ (Gateway本地)
      │   ├── memory/ (Gateway本地)
      │   ├── credentials/ (Gateway本地)
      │   └── plugins/ (Gateway本地)
      └── NO workspace mount (数据在External Sandbox)

External Sandbox Service (Remote)
  ├── API Server
  │   ├── POST /tools/{name} (文件/Shell操作)
  │   ├── POST /skills/{name} (Skills执行)
  │   └── Workspace Management
  │
  ├── Workspace Storage
  │   ├── workspace-{sessionKey-1}/
  │   ├── workspace-{sessionKey-2}/
  │   └── workspace-default/
  │
  └── Execution Engine
      ├── File Operations (read/write/edit)
      ├── Shell Execution (exec/process)
      ├── Security Layer (权限控制)
      └── Resource Limits (CPU/Memory)
```

---

## Plugin实现

### 拦截策略

```typescript
// Plugin拦截工具列表（与OpenClaw Docker Sandbox一致）
const interceptedTools = [
  "read",           // File read → External Sandbox
  "write",          // File write → External Sandbox
  "edit",           // File edit → External Sandbox
  "apply_patch",    // File patch → External Sandbox
  "exec",           // Shell execution → External Sandbox
  "process"         // Background process → External Sandbox
];

// 不拦截的工具（Gateway本地执行）：
// - memory_search, memory_get: Memory backend in Gateway
// - sessions_list, sessions_spawn, sessions_send, subagents: Session management
// - web_search, web_fetch: Web operations (safe)
// - cron, gateway, nodes: Gateway management
// - image, tts: Media processing (optional)
```

### before_tool_call Hook逻辑

```typescript
api.on("before_tool_call", async (event, ctx) => {
  // 1. 判断是否拦截
  if (!interceptedTools.includes(event.toolName)) {
    // 不拦截 → Gateway本地执行
    return;
  }

  // 2. exec特殊处理：host参数可覆盖
  if (event.toolName === "exec" && event.params.host === "gateway/node") {
    return; // 不拦截
  }

  // 3. 直接HTTP POST到External Sandbox
  const response = await fetch(sandboxUrl + "/tools/" + event.toolName, {
    method: 'POST',
    headers: {
      'Authorization': 'Bearer ' + apiKey,
      'X-Workspace-ID': workspaceID
    },
    body: JSON.stringify({toolName, params, context})
  });

  // 4. Block本地执行，返回结果
  return {
    block: true,
    blockReason: "REMOTE_EXECUTION_SUCCESS:" + JSON.stringify(result)
  };
});
```

---

## External Sandbox API

### 工具执行接口

```yaml
# External Sandbox API Specification

POST /tools/{toolName}
  Headers:
    Authorization: Bearer {apiKey}
    X-Workspace-ID: {workspaceID}
  Body:
    {
      "toolName": "read",
      "params": {"path": "/workspace/file.txt"},
      "context": {"sessionKey": "xxx"}
    }
  Response:
    {
      "output": "file content",
      "status": "completed",
      "duration": 120,
      "exitCode": 0
    }

# read工具示例
POST /tools/read
  Body: {"params": {"path": "/workspace/config.yaml"}}
  Response: {"output": "yaml content", "status": "completed"}

# write工具示例
POST /tools/write
  Body: {"params": {"path": "/workspace/new.txt", "content": "data"}}
  Response: {"output": "written", "status": "completed"}

# exec工具示例
POST /tools/exec
  Body: {"params": {"command": "python script.py", "workdir": "/workspace"}}
  Response: {"output": "script output", "exitCode": 0}
```

### Skills执行接口

```yaml
POST /skills/{skillName}
  Headers:
    Authorization: Bearer {apiKey}
    X-Workspace-ID: {workspaceID}
  Body: {"params": {"query": "weather"}}
  Response: {"output": {"temp": 25}, "duration": 1500}
```

### Workspace管理接口

```yaml
POST /workspace/create
  Body: {"sessionKey": "session-xxx"}
  Response: {"workspaceID": "workspace-xxx", "path": "/workspace/session-xxx"}

POST /workspace/cleanup
  Body: {"workspaceID": "workspace-xxx"}
  Response: {"success": true}
```

---

## 与原OpenClaw Docker Sandbox对比

### 设计理念一致性

| 方面 | OpenClaw Docker Sandbox | External Sandbox | 一致性 |
|------|------------------------|------------------|--------|
| **文件操作隔离** | Container内 | External Sandbox | ✅ 一致 |
| **Shell执行隔离** | Container内 (host=sandbox) | External Sandbox | ✅ 一致 |
| **Memory本地** | Gateway本地 | Gateway本地 | ✅ 一致 |
| **Session本地** | Gateway本地 | Gateway本地 | ✅ 一致 |
| **Web本地** | Gateway本地 | Gateway本地 | ✅ 一致 |
| **Cron本地** | Gateway本地 | Gateway本地 | ✅ 一致 |

### 配置对比

```json
// OpenClaw原配置（Docker Sandbox）
{
  "agents.defaults.sandbox.mode": "all"
}
// 效果：创建Docker Container，文件/Shell在Container执行

// 新配置（External Sandbox）
{
  "agents.defaults.sandbox.mode": "off",
  "plugins.enabled": true,
  "plugins.load.paths": ["/etc/aiagent/plugins/harness-bridge"]
}
// 效果：不创建Docker Container，Plugin拦截文件/Shell → External Sandbox
```

---

## 执行流程对比

### read工具流程

**原OpenClaw Docker流程**：
```
LLM → read tool → SandboxFsBridge → docker exec → Container → cat file → Result
```

**新流程**：
```
LLM → read tool → before_tool_call hook拦截 → HTTP POST → External Sandbox → read file → Result in blockReason
```

### memory_search工具流程（不拦截）

**原OpenClaw流程**：
```
LLM → memory_search tool → Memory Manager → Gateway数据库 → Result
```

**新流程（相同）**：
```
LLM → memory_search tool → before_tool_call hook不拦截 → Memory Manager → Gateway数据库 → Result
```

---

## 配置说明

### OpenClaw配置

```json
{
  "agents": {
    "defaults": {
      "sandbox": {
        "mode": "off"  // 关键：不创建Docker Container
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
```

### Plugin环境变量

```bash
# External Sandbox配置（Plugin直接读取）
EXTERNAL_SANDBOX_URL=http://sandbox.example.com:9000
EXTERNAL_SANDBOX_API_KEY=xxx
EXTERNAL_SANDBOX_WORKSPACE=workspace-default
```

### External Sandbox服务配置

```yaml
external_sandbox:
  url: http://sandbox.example.com:9000
  auth:
    api_key: xxx
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
    resource_limits:
      memory: 512MB
      cpu: 1
      timeout: 300s
```

---

## 总结

### 核心设计原则

```
设计理念：与原OpenClaw Docker Sandbox一致
  - 文件操作 + Shell执行 → External Sandbox（隔离）
  - Memory + Session + Web + Cron → Gateway本地（管理）
  - Plugin直接调用External Sandbox（不转发）
```

### 工具分类

| Category | 执行位置 | 工具列表 |
|----------|---------|---------|
| **文件操作** | External Sandbox | read, write, edit, apply_patch |
| **Shell执行** | External Sandbox | exec, process |
| **Memory** | Gateway本地 | memory_search, memory_get |
| **Session** | Gateway本地 | sessions_*, subagents |
| **Web** | Gateway本地 | web_search, web_fetch |
| **Gateway管理** | Gateway本地 | cron, gateway, nodes |
| **Media** | Gateway本地（可选） | image, tts |

### 关键变化

| 方面 | 变化 | 原因 |
|------|------|------|
| **Docker Container** | 不创建 | mode=off |
| **SandboxFsBridge** | 不使用 | 替换为Plugin hook |
| **Plugin转发** | 不转发 | 直接调用External Sandbox |
| **Gateway管理功能** | 不变 | Memory/Session/Cron保持本地 |

---

## References

- OpenClaw Sandbox设计：`/home/joehuang_sweden/aiagent2/openclaw/src/agents/sandbox/`
- Plugin生成器：`/home/joehuang_sweden/aiagent2/aiagent/pkg/handler/openclaw/plugin_generator.go`
- OpenClaw Container执行分析：`openclaw-container-execution-analysis.md`