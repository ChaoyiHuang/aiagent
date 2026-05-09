# OpenClaw Container执行机制深度分析

## 问题一：OpenClaw的Container执行机制是什么？

### 机制概述

OpenClaw使用**Docker容器隔离**作为Sandbox执行机制：

```
┌─────────────────────────────────────────────────────────────┐
│                  OpenClaw Sandbox Architecture               │
└─────────────────────────────────────────────────────────────┘

Gateway Host (OpenClaw运行的主机)
  ├── OpenClaw Gateway Process
  │   ├── Agent Execution Engine
  │   ├── Tool Handlers (read/write/exec)
  │   ├── Plugin System
  │   └── Sandbox Manager
  │
  ├── Docker Daemon
  │   └── Sandbox Container (隔离容器)
  │       ├── Container Workdir (/workspace)
  │       ├── Mounted Host Paths
  │       │   ├── ~/workspace → /workspace (rw)
  │       │   ├── ~/.openclaw/skills → /skills (ro)
  │       │   └── Custom binds (可选)
  │       ├── Security Constraints
  │       │   ├── --read-only (可选)
  │       │   ├── --cap-drop ALL
  │       │   ├── --security-opt no-new-privileges
  │       │   ├── --pids-limit
  │       │   ├── --memory limit
  │       │   └── --network isolation
  │       └── Sleep infinity (保持运行)
  │
  └── Host Filesystem
      ├── ~/.openclaw/
      │   ├── sessions/ (会话记录)
      │   ├── memory/ (记忆存储)
      │   ├── credentials/ (认证信息)
      │   └── skills/ (Skills定义)
      └── ~/workspace/ (工作目录)
```

### Container创建流程（`sandbox/docker.ts`）

```typescript
// 1. 解析Sandbox配置
const cfg = resolveSandboxConfigForAgent(config, agentId);

// 2. 创建Docker容器参数
const args = buildSandboxCreateArgs({
  name: containerName,
  cfg: dockerConfig,
  scopeKey: sessionKey,
  labels: { "openclaw.sandbox": "1", "openclaw.sessionKey": sessionKey }
});

// 3. 执行docker create
await execDocker([
  "create",
  "--name", containerName,
  "--label", "openclaw.sandbox=1",
  "--label", `openclaw.sessionKey=${scopeKey}`,
  "--read-only",  // 可选：root filesystem只读
  "--cap-drop", "ALL",  // 丢弃所有capabilities
  "--security-opt", "no-new-privileges",
  "--pids-limit", "100",  // 进程数限制
  "--memory", "512m",  // 内存限制
  "--network", "none",  // 网络隔离（可选）
  "-v", `${workspaceDir}:${workdir}`,  // 挂载工作目录
  "--workdir", workdir,
  cfg.image,  // 默认debian:bookworm-slim
  "sleep", "infinity"  // 保持容器运行
]);

// 4. 启动容器
await execDocker(["start", containerName]);

// 5. 执行setup命令（可选）
if (cfg.setupCommand) {
  await execDocker(["exec", "-i", containerName, "/bin/sh", "-lc", cfg.setupCommand]);
}
```

### Container执行方式（`fs-bridge.ts`）

所有文件操作都通过`docker exec`在容器内执行：

```typescript
private async runCommand(script: string, options: RunCommandOptions): Promise<ExecDockerRawResult> {
  const dockerArgs = [
    "exec",
    "-i",
    this.sandbox.containerName,  // 容器名称
    "sh",
    "-c",
    script,  // Shell脚本
    "moltbot-sandbox-fs"
  ];
  return execDockerRaw(dockerArgs, {
    input: options.stdin,  // 可以传入数据
    allowFailure: options.allowFailure,
    signal: options.signal
  });
}

// 文件读取示例
async readFile(params: { filePath: string }): Promise<Buffer> {
  const target = this.resolveResolvedPath(params);
  // 直接在容器内读取文件
  return this.readPinnedFile(target);
}

// 文件写入示例
async writeFile(params: { filePath: string, data: Buffer }): Promise<void> {
  const target = this.resolveResolvedPath(params);
  // 生成临时文件，然后通过docker exec移动
  const tempPath = await this.writeFileToTempPath({
    targetContainerPath: target.containerPath,
    data: params.data
  });
  // 执行shell命令：mv tempPath targetPath
  await this.runCheckedCommand(buildWriteCommitPlan(target, tempPath));
}
```

---

## 问题二：read/write/exec是否在Container里执行？

### 答案：取决于Sandbox Mode配置

OpenClaw有三种Sandbox模式（`sandbox/types.ts:56`）：

```typescript
type SandboxMode = "off" | "non-main" | "all";
```

#### Mode: "off"（默认）

**所有工具在Gateway本地执行**：

```
Tool Call → Gateway本地execute() → 本地文件系统 → Result
```

| Tool | 执行位置 | 说明 |
|------|---------|------|
| `read` | Gateway Host | 读取本地文件 |
| `write` | Gateway Host | 写入本地文件 |
| `edit` | Gateway Host | 编辑本地文件 |
| `exec` | Gateway Host (默认) | 本地Shell执行 |

#### Mode: "non-main"（部分隔离）

**Main Agent本地执行，Sub-agents在Container执行**：

```
Main Agent Tool Call → Gateway本地 → Result
Sub-agent Tool Call → Container Sandbox → Result
```

**典型场景**：
- 主Agent（coding agent）：本地执行，直接访问文件系统
- 子Agent（安全敏感任务）：Container执行，隔离环境

#### Mode: "all"（完全隔离）

**所有Agent的工具都在Container执行**：

```
Tool Call → SandboxFsBridge → docker exec → Container → Result
```

| Tool | 执行位置 | 实现方式 |
|------|---------|---------|
| `read` | Container | `docker exec ... cat ${filePath}` |
| `write` | Container | `docker exec ... mv ${tempPath} ${targetPath}` |
| `edit` | Container | `docker exec ... sed/apply_patch` |
| `exec` | Container (host="sandbox") | `docker exec ... sh -c "${command}"` |
| `exec` | Gateway (host="gateway") | 本地Shell执行（不受sandbox限制） |
| `exec` | Node (host="node") | 远程Node执行（已有机制） |

### exec工具的Host选择机制（`bash-tools.exec.ts:307-349`）

```typescript
// 默认host配置
const configuredHost = defaults?.host ?? "sandbox";  // sandbox mode默认

// 参数可覆盖
const requestedHost = normalizeExecHost(params.host);  // 用户指定host参数
let host: ExecHost = requestedHost ?? configuredHost;

// Sandbox可用性检查
const sandbox = host === "sandbox" ? defaults?.sandbox : undefined;
if (host === "sandbox" && !sandbox) {
  throw new Error("exec host=sandbox configured, but sandbox runtime unavailable");
}

// Elevated command强制使用gateway
if (elevatedRequested) {
  host = "gateway";  // 需要权限提升，在gateway执行
}

// 实际执行
if (host === "node") {
  return executeNodeHostCommand({...});  // 远程Node执行
}
if (host === "gateway") {
  return processGatewayAllowlist({...});  // Gateway本地执行
}
if (host === "sandbox") {
  return runExecProcess({sandbox, ...});  // Container执行
}
```

### Tool执行位置对比表

| Tool | Mode="off" | Mode="non-main" | Mode="all" |
|------|-----------|-----------------|------------|
| `read` | Gateway | Main: Gateway<br>Sub: Container | Container |
| `write` | Gateway | Main: Gateway<br>Sub: Container | Container |
| `edit` | Gateway | Main: Gateway<br>Sub: Container | Container |
| `exec` (host=sandbox) | Gateway | Main: Gateway<br>Sub: Container | Container |
| `exec` (host=gateway) | Gateway | Gateway | Gateway |
| `exec` (host=node) | Node | Node | Node |

---

## 问题三：OpenClaw自身的cron/memory等功能是否可以在Container里执行？

### 答案：不可以！这些功能必须运行在Gateway Host

### 原因分析

#### 1. **Gateway是OpenClaw的核心运行环境**

OpenClaw采用**Gateway架构**：

```
┌─────────────────────────────────────────────────────────────┐
│            Gateway = OpenClaw Runtime Core                   │
└─────────────────────────────────────────────────────────────┘

Gateway Process (必须在Host上运行)
  ├── Agent Execution Engine
  │   ├── LLM API调用
  │   ├── Session管理
  │   ├── Tool调度
  │   └── Plugin加载
  ├── Cron Scheduler
  │   ├── 定时任务触发
  │   ├── Job管理
  │   └── Webhook调用
  ├── Memory Backend
  │   ├── QMD/SQLite数据库
  │   ├── Memory搜索
  │   ├── 数据持久化
  ├── HTTP/WebSocket Server
  │   ├── Gateway API
  │   ├── Plugin HTTP routes
  │   └── Agent状态推送
  ├── Channel Connectors
  │   ├── Telegram/Discord/Slack等
  │   ├── 消息收发
  │   └── Session绑定
  └── Sandbox Manager
      ├── Container创建/销毁
      ├── Container生命周期管理
      └── 工具路由决策
```

**Gateway进程必须在Host运行**，因为：
- 需要访问Host的文件系统（`~/.openclaw/`）
- 需要管理Docker容器（调用docker命令）
- 需要运行HTTP服务器（Gateway API）
- 需要连接外部服务（LLM API、Channels）

#### 2. **Cron/Memory/Session等功能的依赖分析**

| 功能 | 依赖资源 | 必须运行位置 | 能否在Container |
|------|---------|-------------|----------------|
| **Cron Scheduler** | `~/.openclaw/cron.json`<br>Gateway HTTP API<br>定时触发机制 | Gateway Host | ❌ NO |
| **Memory Backend** | `~/.openclaw/memory/`<br>QMD数据库<br>SQLite/Redis | Gateway Host | ❌ NO |
| **Session Storage** | `~/.openclaw/sessions/*.jsonl`<br>会话历史 | Gateway Host | ❌ NO |
| **Credentials Store** | `~/.openclaw/credentials/`<br>OAuth/API keys | Gateway Host | ❌ NO |
| **Plugin System** | `~/.openclaw/plugins/`<br>Plugin加载执行 | Gateway Host | ❌ NO |
| **Gateway HTTP Server** | HTTP端口监听<br>WebSocket推送 | Gateway Host | ❌ NO |
| **Channel Connectors** | Telegram/Discord API<br>消息收发 | Gateway Host | ❌ NO |

#### 3. **Cron Tool的实现（`tools/cron-tool.ts`）**

```typescript
// Cron工具调用Gateway API
async function callGateway(action: string, gatewayOpts: GatewayCallOptions, params: unknown) {
  const gatewayUrl = gatewayOpts.gatewayUrl || `http://localhost:${process.env.GATEWAY_PORT || 18789}`;
  const response = await fetch(`${gatewayUrl}/api/${action}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${gatewayOpts.gatewayToken}`
    },
    body: JSON.stringify(params)
  });
  return response.json();
}

// Cron action示例
execute: async (_toolCallId, params) => {
  const action = readStringParam(params, "action");
  
  switch (action) {
    case "add":
      // 调用Gateway API添加定时任务
      return jsonResult(await callGateway("cron.add", gatewayOpts, { job }));
    case "list":
      // 调用Gateway API列出定时任务
      return jsonResult(await callGateway("cron.list", gatewayOpts, {}));
    case "run":
      // 调用Gateway API手动触发任务
      return jsonResult(await callGateway("cron.run", gatewayOpts, { id, mode }));
  }
}
```

**关键点**：
- Cron Tool只是一个**Gateway API的客户端**
- 实际的Cron Scheduler运行在Gateway进程中
- 定时任务触发时，Gateway调用Agent执行

#### 4. **Memory Tool的实现（`tools/memory-tool.ts`）**

```typescript
execute: async (_toolCallId, params) => {
  const query = readStringParam(params, "query", { required: true });
  
  // 获取Memory Search Manager（运行在Gateway）
  const { manager, error } = await getMemorySearchManager({
    cfg,  // Gateway config
    agentId
  });
  
  if (!manager) {
    return jsonResult(buildMemorySearchUnavailableResult(error));
  }
  
  // 执行搜索（在Gateway本地的QMD/SQLite数据库）
  const rawResults = await manager.search(query, {
    maxResults,
    minScore,
    sessionKey
  });
  
  return jsonResult({
    results: decorated,
    provider: manager.status().provider,
    model: manager.status().model
  });
}
```

**关键点**：
- Memory Tool调用Gateway的Memory Manager
- Memory Manager访问`~/.openclaw/memory/`目录
- 数据库（QMD/SQLite）运行在Gateway进程中

---

### Sandbox Container的职责范围

**Container只用于Agent的Tool执行隔离**：

```
┌─────────────────────────────────────────────────────────────┐
│           Sandbox Container职责 vs Gateway职责               │
└─────────────────────────────────────────────────────────────┘

Container职责（Agent Tool隔离）
  ✅ 文件操作：read/write/edit/apply_patch
  ✅ Shell执行：exec/process
  ✅ Browser操作：browser/canvas (可选)
  ✅ Skills执行：通过SKILL.md + Container环境
  ❌ Cron/Memory/Session管理
  ❌ Plugin加载执行
  ❌ Gateway HTTP服务
  ❌ Channel连接

Gateway职责（OpenClaw核心）
  ✅ Agent执行引擎（LLM调用、Session管理）
  ✅ Tool调度（路由决策）
  ✅ Cron Scheduler（定时任务）
  ✅ Memory Backend（数据持久化）
  ✅ Plugin System（加载执行）
  ✅ HTTP/WebSocket Server
  ✅ Channel Connectors
  ✅ Sandbox Manager（Container管理）
  ❌ 文件操作（委托给Container或本地）
  ❌ Shell执行（委托给Container、Gateway或Node）
```

---

### 为什么不能在Container运行Cron/Memory？

#### 技术原因

1. **Container生命周期不持久**
   - Container可能被销毁重建（config变更、prune）
   - Cron Job需要持久化存储，Container重启会丢失

2. **Gateway API不可访问**
   - Container网络通常隔离（`--network none`）
   - 无法访问Gateway的HTTP端点

3. **文件系统不共享**
   - Container只有挂载的workspace目录
   - `~/.openclaw/`目录在Host，Container无法访问

4. **权限隔离**
   - Container安全限制（`--cap-drop ALL`, `no-new-privileges`）
   - 无法管理Docker（不能调用docker命令）

#### 架构原因

1. **职责分离**
   - Gateway：管理、调度、持久化
   - Container：执行、隔离、临时性

2. **数据一致性**
   - Memory/Cron/Session需要全局一致
   - Container之间隔离，无法共享数据

3. **可靠性要求**
   - Gateway必须持续运行（服务可用性）
   - Container可以随时销毁（隔离性）

---

## 总结：OpenClaw执行机制全景图

### 执行位置矩阵

| 功能/组件 | Gateway Host | Sandbox Container | Remote Node |
|----------|-------------|------------------|-------------|
| **Agent Execution** | ✅ Main Agent | ✅ Sub-agent (mode=all) | ❌ |
| **LLM API Calls** | ✅ 必须在这里 | ❌ 网络隔离 | ❌ |
| **read/write/edit** | ✅ (mode=off) | ✅ (mode=all) | ❌ |
| **exec (host=sandbox)** | ❌ | ✅ Container内 | ❌ |
| **exec (host=gateway)** | ✅ Gateway本地 | ❌ | ❌ |
| **exec (host=node)** | ❌ | ❌ | ✅ 远程Node |
| **Cron Scheduler** | ✅ 必须在这里 | ❌ 生命周期不持久 | ❌ |
| **Memory Backend** | ✅ 必须在这里 | ❌ 数据库在Host | ❌ |
| **Session Storage** | ✅ 必须在这里 | ❌ 会话在Host | ❌ |
| **Plugin System** | ✅ 加载执行 | ❌ Plugin在Host | ❌ |
| **Gateway HTTP** | ✅ 服务监听 | ❌ 端口绑定 | ❌ |
| **Channel Connectors** | ✅ API连接 | ❌ 网络限制 | ❌ |
| **Sandbox Manager** | ✅ 管理Docker | ❌ 不能自管理 | ❌ |

### 我们的Harness Bridge Integration的影响

| Harness组件 | 应该运行位置 | 原因 |
|------------|-------------|------|
| **Harness Bridge HTTP Server** | Gateway Host | 需要访问HarnessManager，管理Sandbox |
| **SkillsHarness** | Gateway Host | Skills定义和路由决策 |
| **SandboxHarness** | Gateway Host + External Sandbox | 管理External Sandbox服务 |
| **ModelHarness** | Gateway Host | LLM API配置和路由 |
| **MCPHarness** | Gateway Host | MCP Server管理 |
| **MemoryHarness** | Gateway Host | Memory Backend管理 |
| **Skill Execution (External Sandbox)** | External Sandbox Service | 远程执行，隔离环境 |

**关键结论**：
- Harness Bridge **必须在Gateway Host运行**
- Skills和Built-in Tools可以通过Bridge路由到External Sandbox
- Cron/Memory/Session等功能仍然在Gateway本地运行
- External Sandbox只负责**工具执行**，不负责**核心管理**

---

## References

- Sandbox Docker: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/sandbox/docker.ts`
- Sandbox Context: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/sandbox/context.ts`
- Sandbox FsBridge: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/sandbox/fs-bridge.ts`
- Exec Tool Host Selection: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/bash-tools.exec.ts`
- Cron Tool: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/tools/cron-tool.ts`
- Memory Tool: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/tools/memory-tool.ts`