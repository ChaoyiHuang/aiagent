// Package openclaw provides plugin generation for harness-bridge integration.
// This file generates OpenClaw plugin files dynamically at runtime,
// enabling external sandbox tool execution without modifying OpenClaw source code.
//
// Plugin Injection Mechanism:
// OpenClaw discovers plugins from multiple sources (src/plugins/discovery.ts):
// - plugins.load.paths config (origin: "config")
// - workspace/.openclaw/extensions (origin: "workspace")
// - ~/.openclaw/extensions (origin: "global")
// - bundled extensions/ (origin: "bundled")
//
// Agent Handler injects plugin by:
// 1. Generating plugin files to /etc/aiagent/plugins/harness-bridge/
// 2. Setting plugins.load.paths in OpenClaw config to include that directory
// 3. OpenClaw automatically discovers and loads the plugin
package openclaw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// HarnessBridgePluginConfig contains configuration for the harness-bridge plugin.
// Design: Plugin directly calls External Sandbox (no Bridge forwarding)
// Consistent with OpenClaw Docker Sandbox design philosophy:
// - File I/O and Shell execution tools → External Sandbox (isolation)
// - Memory/Sessions/Web/Cron tools → Gateway local (management)
type HarnessBridgePluginConfig struct {
	// BridgeURL is the External Sandbox API URL (e.g., "http://sandbox.example.com:9000")
	// Plugin calls this URL directly without forwarding through Bridge
	BridgeURL string

	// PluginDir is the target directory for plugin files (e.g., "/etc/aiagent/plugins/harness-bridge")
	PluginDir string

	// Skills is the list of skills to expose through the plugin
	// Each skill becomes an optional skill_xxx tool for convenience
	Skills []string

	// SandboxApiKey is the API key for External Sandbox authentication (optional)
	// Can also be provided via environment variable EXTERNAL_SANDBOX_API_KEY
	SandboxApiKey string

	// DefaultWorkspace is the default workspace ID for session isolation
	// Can also be provided via environment variable EXTERNAL_SANDBOX_WORKSPACE
	DefaultWorkspace string
}

// GenerateHarnessBridgePlugin generates the harness-bridge plugin files.
// This plugin allows OpenClaw to call skills through Harness Bridge HTTP endpoint,
// enabling external sandbox tool execution without modifying OpenClaw source code.
//
// Generated files:
// - index.ts: Plugin implementation with harness_bridge tool
// - package.json: Plugin package metadata
// - openclaw.plugin.json: OpenClaw plugin manifest
func GenerateHarnessBridgePlugin(ctx context.Context, cfg *HarnessBridgePluginConfig) error {
	if cfg == nil {
		return fmt.Errorf("plugin config is required")
	}

	if cfg.PluginDir == "" {
		cfg.PluginDir = "/etc/aiagent/plugins/harness-bridge"
	}

	// Create plugin directory
	if err := os.MkdirAll(cfg.PluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// Generate index.ts (main plugin file)
	if err := generatePluginIndexTS(cfg); err != nil {
		return fmt.Errorf("failed to generate index.ts: %w", err)
	}

	// Generate package.json
	if err := generatePluginPackageJSON(cfg); err != nil {
		return fmt.Errorf("failed to generate package.json: %w", err)
	}

	// Generate openclaw.plugin.json
	if err := generatePluginManifest(cfg); err != nil {
		return fmt.Errorf("failed to generate openclaw.plugin.json: %w", err)
	}

	return nil
}

// generatePluginIndexTS generates the main plugin TypeScript file.
func generatePluginIndexTS(cfg *HarnessBridgePluginConfig) error {
	// Generate skill list for tool description
	skillList := ""
	for i, skill := range cfg.Skills {
		if i > 0 {
			skillList += ", "
		}
		skillList += skill
	}

	// Note: We use heredoc pattern for the TypeScript template to avoid escaping issues
	content := fmt.Sprintf(`import type { OpenClawPluginApi, PluginHookBeforeToolCallEvent, PluginHookToolContext } from "openclaw/plugin-sdk";

// Harness Bridge Plugin for External Sandbox Integration
//
// DESIGN PHILOSOPHY: Same as OpenClaw Docker Sandbox
// - Tools needing isolation (file I/O, shell execution) → External Sandbox
// - Tools managing Gateway (memory, sessions, cron) → Gateway local
// - No source code modification to OpenClaw required
//
// Tool Classification (consistent with OpenClaw SandboxFsBridge):
//   External Sandbox (intercepted):
//     - read, write, edit, apply_patch: File operations (data isolation)
//     - exec, process: Shell execution (code isolation)
//   Gateway Local (not intercepted):
//     - memory_search, memory_get: Memory backend (Gateway database)
//     - sessions_*, subagents: Session management (Gateway control)
//     - web_search, web_fetch: Web operations (safe)
//     - cron, gateway: Gateway management (Gateway core)
//     - image, tts: Media processing (optional local)
//
// Architecture: Plugin directly calls External Sandbox (no Bridge forwarding)
// Flow: Tool call → before_tool_call hook → External Sandbox HTTP API → Result in blockReason

const harnessBridgePlugin = {
  id: "harness-bridge",
  name: "Harness Bridge",
  description: "Routes tool/skill calls to External Sandbox. Design consistent with OpenClaw Docker Sandbox. Available skills: %s",

  register(api: OpenClawPluginApi) {
    // ===== Configuration =====
    // Sandbox URL from environment or default
    const sandboxUrl = process.env.EXTERNAL_SANDBOX_URL || "%s";
    const sandboxApiKey = process.env.EXTERNAL_SANDBOX_API_KEY || "";
    const workspaceDefault = process.env.EXTERNAL_SANDBOX_WORKSPACE || "workspace-default";

    // ===== Tool Interception Policy =====
    // Intercept tools that need isolation (same as OpenClaw Docker Sandbox mode=all)
    const interceptedTools = [
      "read",           // File read → External Sandbox
      "write",          // File write → External Sandbox
      "edit",           // File edit → External Sandbox
      "apply_patch",    // File patch → External Sandbox
      "exec",           // Shell execution → External Sandbox
      "process"         // Background process → External Sandbox
    ];

    // Tools NOT intercepted (execute in Gateway local):
    // - memory_search, memory_get: Memory backend in Gateway
    // - sessions_list, sessions_spawn, sessions_send, subagents: Session management
    // - web_search, web_fetch: Web operations (safe)
    // - cron, gateway, nodes: Gateway management
    // - image, tts, browser, canvas: Media/UI (optional)

    // ===== 1. before_tool_call Hook: Intercept and Route to External Sandbox =====
    api.on("before_tool_call", async (event: PluginHookBeforeToolCallEvent, ctx: PluginHookToolContext) => {
      const toolName = event.toolName;
      const params = event.params || {};

      // Check if this tool should be intercepted
      if (!interceptedTools.includes(toolName)) {
        // Not intercepted → Gateway local execution (memory, sessions, web, cron, etc.)
        return;
      }

      // Special case: exec tool with explicit host parameter
      // If host="gateway" or "node", respect that setting (don't intercept)
      if (toolName === "exec") {
        const host = params.host;
        if (host === "gateway" || host === "node") {
          // Explicit host override → Gateway or Node execution
          return;
        }
      }

      // ===== Intercept → External Sandbox =====
      const toolEndpoint = sandboxUrl + "/tools/" + encodeURIComponent(toolName);

      // Resolve workspace ID from session
      const workspaceID = resolveWorkspaceID(ctx.sessionKey || "", workspaceDefault);

      try {
        const response = await fetch(toolEndpoint, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer ' + sandboxApiKey,
            'X-Workspace-ID': workspaceID,
            'X-Agent-Id': ctx.agentId || 'unknown',
            'X-Session-Key': ctx.sessionKey || '',
            'X-Session-Id': ctx.sessionId || '',
            'X-Run-Id': ctx.runId || ''
          },
          body: JSON.stringify({
            toolName: toolName,
            params: params,
            toolCallId: event.toolCallId,
            context: {
              agentId: ctx.agentId,
              sessionKey: ctx.sessionKey,
              sessionId: ctx.sessionId,
              workspaceDir: ctx.workspaceDir || workspaceID
            }
          })
        });

        if (!response.ok) {
          // External Sandbox execution failed
          const errorText = await response.text();
          return {
            block: true,
            blockReason: 'External Sandbox error (' + response.status + '): ' + errorText +
                         '. Tool ' + toolName + ' blocked for safety (fail-closed).'
          };
        }

        const result = await response.json();

        // SUCCESS: Block local execution and embed result in blockReason
        // Format: REMOTE_EXECUTION_SUCCESS:<json>
        const resultJson = JSON.stringify({
          tool: toolName,
          output: result.output,
          remote: true,
          sandboxId: result.sandboxId || sandboxUrl,
          workspaceId: workspaceID,
          duration: result.duration || 0,
          exitCode: result.exitCode || 0,
          status: result.status || 'completed'
        });

        return {
          block: true,
          blockReason: 'REMOTE_EXECUTION_SUCCESS:' + resultJson
        };

      } catch (error) {
        // Network error → Block for safety (fail-closed)
        return {
          block: true,
          blockReason: 'External Sandbox connection error: ' + String(error) +
                       '. Tool ' + toolName + ' blocked for safety (fail-closed).'
        };
      }
    });

    // ===== 2. harness_bridge Tool: Skills Execution =====
    // Skills are executed in External Sandbox (same isolation level as tools)
    api.registerTool((ctx) => {
      return {
        name: "harness_bridge",
        label: "Harness Bridge",
        description: "Execute a skill through External Sandbox. Use when skill execution needs isolation.",
        parameters: {
          type: "object",
          properties: {
            skill: {
              type: "string",
              description: "Skill name to execute (e.g., weather, calculator)"
            },
            params: {
              type: "object",
              description: "Skill parameters as JSON object"
            }
          },
          required: ["skill"]
        },
        execute: async (toolCallId, args) => {
          const skillName = args.skill;
          const skillParams = args.params || {};

          const skillEndpoint = sandboxUrl + "/skills/" + encodeURIComponent(skillName);
          const workspaceID = resolveWorkspaceID(ctx.sessionKey || "", workspaceDefault);

          try {
            const response = await fetch(skillEndpoint, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + sandboxApiKey,
                'X-Workspace-ID': workspaceID,
                'X-Agent-Id': ctx.agentId || 'unknown',
                'X-Session-Key': ctx.sessionKey || ''
              },
              body: JSON.stringify(skillParams)
            });

            if (!response.ok) {
              const errorText = await response.text();
              return {
                content: [{
                  type: "text",
                  text: 'External Sandbox error: ' + response.status + ' ' + errorText
                }],
                isError: true
              };
            }

            const result = await response.json();

            // Format result for OpenClaw
            const outputText = typeof result.output === 'object'
              ? JSON.stringify(result.output, null, 2)
              : String(result.output);

            return {
              content: [{
                type: "text",
                text: outputText
              }],
              details: {
                skill: skillName,
                remote: true,
                sandboxId: result.sandboxId || sandboxUrl,
                workspaceId: workspaceID,
                duration: result.duration || 0
              }
            };

          } catch (error) {
            return {
              content: [{
                type: "text",
                text: 'External Sandbox connection error: ' + String(error)
              }],
              isError: true
            };
          }
        }
      };
    }, { names: ["harness_bridge"] });

    // ===== 3. Individual Skill Tools (Optional) =====
    // Register convenience skill tools for direct LLM invocation
    const skills = %s;
    for (const skillName of skills) {
      api.registerTool((ctx) => {
        return {
          name: "skill_" + skillName.replace(/-/g, "_"),
          label: skillName + " Skill",
          description: "Execute " + skillName + " skill through External Sandbox",
          parameters: {
            type: "object",
            properties: {
              params: {
                type: "object",
                description: skillName + " skill parameters"
              }
            }
          },
          execute: async (toolCallId, args) => {
            // Delegate to harness_bridge with skill name preset
            const harnessBridgeTool = api.getTool("harness_bridge");
            if (harnessBridgeTool && harnessBridgeTool.execute) {
              return await harnessBridgeTool.execute(toolCallId, {
                skill: skillName,
                params: args.params || {}
              });
            }
            return {
              content: [{ type: "text", text: "harness_bridge tool not available" }],
              isError: true
            };
          }
        };
      }, { names: ["skill_" + skillName.replace(/-/g, "_")], optional: true });
    }
  }
};

// Helper: Resolve workspace ID from session key
function resolveWorkspaceID(sessionKey: string, defaultWorkspace: string): string {
  if (!sessionKey || sessionKey.trim() === "") {
    return defaultWorkspace;
  }
  // Use session key as workspace ID (same isolation as OpenClaw Sandbox scope=session)
  // Format: workspace-{sessionKey}
  return "workspace-" + sessionKey.split(":").pop() || defaultWorkspace;
}

export default harnessBridgePlugin;
`, skillList, cfg.BridgeURL, formatSkillArrayForTS(cfg.Skills))

	return writeFile(filepath.Join(cfg.PluginDir, "index.ts"), content)
}

// generatePluginPackageJSON generates the package.json file.
func generatePluginPackageJSON(cfg *HarnessBridgePluginConfig) error {
	content := `{
  "name": "harness-bridge",
  "version": "1.0.0",
  "description": "Harness Bridge plugin for External Sandbox tool execution. Design consistent with OpenClaw Docker Sandbox: intercepts file/shell tools (read/write/edit/exec/process) and routes to External Sandbox; memory/sessions/web/cron remain Gateway local.",
  "main": "index.ts",
  "openclaw": {
    "extension": true,
    "plugin": true,
    "hooks": ["before_tool_call"],
    "interceptedTools": ["read", "write", "edit", "apply_patch", "exec", "process"]
  }
}`
	return writeFile(filepath.Join(cfg.PluginDir, "package.json"), content)
}

// generatePluginManifest generates the openclaw.plugin.json manifest.
func generatePluginManifest(cfg *HarnessBridgePluginConfig) error {
	content := fmt.Sprintf(`{
  "id": "harness-bridge",
  "name": "Harness Bridge",
  "description": "Routes file/shell tools to External Sandbox. Design consistent with OpenClaw Docker Sandbox: file I/O and shell execution → External Sandbox; memory/sessions/web/cron → Gateway local.",
  "extension": "index.ts",
  "tools": ["harness_bridge"],
  "hooks": ["before_tool_call"],
  "interceptedTools": ["read", "write", "edit", "apply_patch", "exec", "process"],
  "configSchema": {
    "type": "object",
    "properties": {
      "sandboxUrl": {
        "type": "string",
        "description": "External Sandbox API URL (plugin calls directly, no forwarding)",
        "default": "%s"
      },
      "sandboxApiKey": {
        "type": "string",
        "description": "API key for External Sandbox authentication (or use EXTERNAL_SANDBOX_API_KEY env var)"
      },
      "defaultWorkspace": {
        "type": "string",
        "description": "Default workspace ID for session isolation (or use EXTERNAL_SANDBOX_WORKSPACE env var)",
        "default": "workspace-default"
      }
    }
  }
}`, cfg.BridgeURL)

	return writeFile(filepath.Join(cfg.PluginDir, "openclaw.plugin.json"), content)
}

// formatSkillArrayForTS formats skill names as TypeScript array literal.
func formatSkillArrayForTS(skills []string) string {
	if len(skills) == 0 {
		return "[]"
	}
	result := "["
	for i, skill := range skills {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("\"%s\"", skill)
	}
	result += "]"
	return result
}

// writeFile writes content to a file.
func writeFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}