# OpenClaw Tool Remote Execution - Complete Design

## Overview

This document describes the complete implementation of remote tool execution for OpenClaw, covering both Skills and built-in Tools.

## Key Questions Answered

### Q1: How to distinguish Skills execution from built-in Tools execution?

**Answer**: OpenClaw does NOT distinguish between Skills and Tools at execution level. Both are invoked through the same mechanism - tool name matching.

#### Skills vs Built-in Tools

| Aspect | Skills | Built-in Tools |
|--------|--------|----------------|
| **Definition** | TEXT PROMPTS (SKILL.md files) | JavaScript code with execute() method |
| **Registration** | Via plugin's `registerTool()` API | Built into OpenClaw core |
| **LLM Call** | LLM sees tool schema (name, description, parameters) | LLM sees tool schema |
| **Execution** | Plugin's execute() method | Core's execute() method |
| **Examples** | `skill_weather`, `skill_calculator` | `read`, `write`, `edit`, `exec`, `process` |

#### Execution Flow

```
LLM Tool Call Decision
  ↓
tool name matching
  ↓
if (toolName in ["read", "write", "edit", "exec", ...]) → Built-in execute()
if (toolName == "skill_xxx") → Plugin execute()
if (toolName == "harness_bridge") → Plugin execute() → HTTP → Bridge → Remote
```

**Key Insight**: Skills are NOT separate from Tools. Skills are just tools registered by plugins.

### Q2: Can built-in Tools be intercepted for remote execution?

**Answer**: YES! Using `before_tool_call` plugin hook.

## Implementation Design

### Mechanism 1: Harness Bridge Tool (For Skills)

```
┌─────────────────────────────────────────────────────────────────┐
│                    Skill Remote Execution Flow                    │
└─────────────────────────────────────────────────────────────────┘

OpenClaw LLM
  ↓ calls "harness_bridge" tool with skill="weather", params={...}
harness_bridge tool.execute() (in harness-bridge plugin)
  ↓ HTTP POST to Bridge URL
Harness Bridge HTTP Endpoint (/skills/weather)
  ↓
SkillsHarness.ExecuteSkill()
  ↓
RemoteSkillExecutor
  ↓
External Sandbox Service
  ↓ execution result
HTTP Response {output: {...}, resourceId: "sandbox-xxx", duration: 1200}
  ↓
harness_bridge tool.execute() returns
  ↓ {content: [{type: "text", text: "..."}], details: {remote: true, ...}}
OpenClaw receives result
  ↓
LLM processes result and continues conversation
```

**Implementation** (in `plugin_generator.go`):

```typescript
api.registerTool((ctx) => {
  return {
    name: "harness_bridge",
    parameters: {
      type: "object",
      properties: {
        skill: { type: "string" },
        params: { type: "object" }
      },
      required: ["skill"]
    },
    execute: async (toolCallId, args) => {
      const response = await fetch(bridgeUrl + "/skills/" + args.skill, {
        method: 'POST',
        body: JSON.stringify(args.params)
      });
      const result = await response.json();
      return {
        content: [{ type: "text", text: JSON.stringify(result.output) }],
        details: { remote: true, sandboxId: result.resourceId }
      };
    }
  };
});
```

### Mechanism 2: before_tool_call Hook (For Built-in Tools)

```
┌─────────────────────────────────────────────────────────────────┐
│               Built-in Tool Remote Execution Flow                 │
└─────────────────────────────────────────────────────────────────┘

OpenClaw LLM
  ↓ calls "read" tool with path="/etc/config.yaml"
before_tool_call hook fires (in harness-bridge plugin)
  ↓ intercepts toolName == "read"
  ↓ HTTP POST to Bridge URL
Harness Bridge HTTP Endpoint (/tools/read)
  ↓ {toolName: "read", params: {path: "/etc/config.yaml"}}
ToolsHarness.ExecuteTool()
  ↓
RemoteToolExecutor
  ↓
External Sandbox Service (reads file in sandbox)
  ↓ file content
HTTP Response {output: "file content...", resourceId: "sandbox-xxx"}
  ↓
before_tool_call hook returns
  ↓ {block: true, blockReason: "REMOTE_EXECUTION_SUCCESS:{...}"}
Built-in tool execution BLOCKED
  ↓ error thrown with blockReason
OpenClaw receives error
  ↓ LLM sees error message with embedded result
  ↓ extracts result from "REMOTE_EXECUTION_SUCCESS:<json>"
LLM processes result and continues conversation
```

#### OpenClaw Hook Limitations

OpenClaw's `before_tool_call` hook can only:
1. **Modify params**: `{params: {...}}` → tool executes with modified params
2. **Block execution**: `{block: true, blockReason: "..."}` → tool throws error

**Cannot directly return tool result!**

#### "Block with Result in Reason" Pattern

Since we cannot directly return result, we use this pattern:

```typescript
api.on("before_tool_call", async (event, ctx) => {
  // Execute tool remotely
  const response = await fetch(bridgeUrl + "/tools/" + event.toolName, {
    method: 'POST',
    body: JSON.stringify({
      toolName: event.toolName,
      params: event.params
    })
  });
  const result = await response.json();

  // Embed result in blockReason
  const resultJson = JSON.stringify({
    tool: event.toolName,
    output: result.output,
    remote: true,
    sandboxId: result.resourceId
  });

  return {
    block: true,
    blockReason: "REMOTE_EXECUTION_SUCCESS:" + resultJson
  };
});
```

The LLM will receive an error message like:
```
Error: REMOTE_EXECUTION_SUCCESS:{"tool":"read","output":"file content","remote":true}
```

LLM can parse this JSON to extract the result.

### Q3: Will remote execution results seamlessly integrate with OpenClaw business flow?

**Answer**: YES! The result format is compatible.

#### Result Format Compatibility

**Skill Execution Result** (from `harness_bridge` tool):

```typescript
{
  content: [
    { type: "text", text: "weather data..." }
  ],
  details: {
    skill: "weather",
    remote: true,
    sandboxId: "sandbox-123",
    duration: 1200
  }
}
```

**Built-in Tool Result** (from `before_tool_call` hook):

```typescript
// Embedded in blockReason
{
  tool: "read",
  output: "file content...",
  remote: true,
  sandboxId: "sandbox-123",
  duration: 800,
  exitCode: 0,
  status: "completed"
}
```

Both formats contain:
- **output**: The actual result (text/JSON)
- **remote**: Flag indicating remote execution
- **sandboxId**: Sandbox resource identifier
- **duration**: Execution time

#### LLM Processing Flow

```
1. LLM receives result (either from tool return or error message)
2. LLM parses result format
3. LLM extracts output content
4. LLM continues reasoning and conversation
```

**No impact on business logic!** The LLM treats the result the same way.

## Built-in Tools Intercepted

Based on OpenClaw source code (`src/agents/tool-mutation.ts:1-15`):

| Tool | Operation | Intercepted |
|------|-----------|-------------|
| `read` | File read | ✅ YES |
| `write` | File write | ✅ YES |
| `edit` | File edit | ✅ YES |
| `apply_patch` | File patch | ✅ YES |
| `exec` | Shell execution | ✅ YES (unless host="gateway/node") |
| `process` | Background process | ✅ YES |
| `web_search` | Web search | ❌ NO (safe) |
| `web_fetch` | Web fetch | ❌ NO (safe) |
| `memory_search` | Memory search | ❌ NO (safe) |
| `sessions_*` | Session operations | ❌ NO (safe) |

**Interception criteria**:
- File I/O operations (read/write/edit/apply_patch) → Risk of data leakage → Intercept
- Shell execution (exec/process) → Risk of code execution → Intercept
- Web/Memory operations → Low risk → Don't intercept

## Implementation Files

### Generated Plugin Files

Location: `/etc/aiagent/plugins/harness-bridge/`

| File | Content |
|------|---------|
| `index.ts` | Main plugin code with tool and hooks |
| `package.json` | Package metadata |
| `openclaw.plugin.json` | Plugin manifest |

### Plugin Registration

In OpenClaw config (`/etc/agent-config/runtime/openclaw-config.json`):

```json
{
  "plugins": {
    "enabled": true,
    "load": {
      "paths": ["/etc/aiagent/plugins/harness-bridge"]
    }
  }
}
```

OpenClaw will discover and load the plugin from `plugins.load.paths`.

## Execution Modes

### External Sandbox Mode (Intercept Enabled)

```
Tool Call → before_tool_call hook → HTTP → Bridge → Remote Sandbox → Result
          ↓ block built-in tool
          ↓ LLM receives result in error message
```

### Embedded/Local Mode (Intercept Disabled)

Plugin is NOT generated when `SandboxMode != "external"`:

```go
// In adapter.go:EnsurePluginGenerated()
if !h.IsSkillRemoteExecution() {
    return nil  // Skip plugin generation
}
```

Tool calls execute locally:
```
Tool Call → Built-in execute() → Local execution → Result
```

## Security Considerations

### Fail-Closed Policy

When remote execution fails:
```typescript
if (!response.ok) {
  return {
    block: true,
    blockReason: "Remote execution failed. Tool blocked for safety."
  };
}
```

**Built-in tool is blocked** → No local execution → Safe failure

### Fail-Open Alternative (Optional)

To allow fallback to local execution:
```typescript
if (!response.ok) {
  return;  // Don't block, let local execution proceed
}
```

**Not recommended** for security-sensitive tools (exec, write).

## Integration Points

### Harness Bridge HTTP Endpoints

| Endpoint | Purpose | Request Body |
|----------|---------|--------------|
| `/skills/{skillName}` | Execute skill | `{skill, params}` → Skill params |
| `/tools/{toolName}` | Execute built-in tool | `{toolName, params}` → Tool params |

### Request Headers

```
X-Agent-Id: agent-xxx
X-Session-Key: session-xxx
X-Session-Id: uuid-xxx
X-Run-Id: run-xxx
```

### Response Format

```json
{
  "output": "result content (string or object)",
  "resourceId": "sandbox-xxx",
  "duration": 1200,
  "status": "completed",
  "exitCode": 0
}
```

## Summary

| Aspect | Implementation | Impact on OpenClaw |
|--------|---------------|-------------------|
| **Skills Remote Execution** | `harness_bridge` tool | ✅ No modification needed |
| **Built-in Tools Remote** | `before_tool_call` hook | ✅ No modification needed |
| **Result Integration** | Standard format + JSON in error | ✅ Seamless for LLM |
| **Plugin Injection** | `plugins.load.paths` config | ✅ No source modification |
| **Security** | Fail-closed (block on error) | ✅ Safe isolation |

**Total Changes Required**:
- ✅ 0 changes to OpenClaw source code
- ✅ 1 generated plugin (harness-bridge)
- ✅ 1 config entry (plugins.load.paths)
- ✅ 1 HTTP server (Harness Bridge)

## References

- OpenClaw Plugin System: `/home/joehuang_sweden/aiagent2/openclaw/src/plugins/types.ts`
- OpenClaw Hook Mechanism: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/pi-tools.before-tool-call.ts`
- Tool Mutation Detection: `/home/joehuang_sweden/aiagent2/openclaw/src/agents/tool-mutation.ts`
- Generated Plugin: `/home/joehuang_sweden/aiagent2/aiagent/pkg/handler/openclaw/plugin_generator.go`