# Handler Information Flow Analysis

## 实现状态 (2026-05-12)

**已实现方案: Config Daemon (Solution M)**

通过Config Daemon + hostPath方式解决了信息流问题，Handler无需K8s API访问权限即可获取Agent配置。所有E2E测试已通过验证。

---

## 已验证的信息来源

### 1. Harness ConfigMap (已验证 ✓)
- **创建**: AgentRuntime Controller 在 `resolveHarnessReferences()` 中创建
- **命名**: `<harness-name>-harness-config`
- **挂载路径**: `/etc/harness/<harness-name>/`
- **内容**: `model.yaml`, `mcp.yaml`, `memory.yaml`, `sandbox.yaml`, `skills.yaml`
- **状态**: ✅ Handler 可以正确读取，E2E测试验证

### 2. Agent Config via hostPath (已实现 ✓)

采用Config Daemon方案，Agent配置通过hostPath传递：

```
Config Daemon (DaemonSet)
├── Watches AIAgent CRDs via Informer
├── Writes to hostPath: /var/lib/aiagent/configs/<namespace>/<agent-name>/
│   ├── agent-config.json
│   └── agent-meta.yaml
│
Pod (AgentRuntime)
├── Mounts hostPath: /var/lib/aiagent/configs -> /etc/agent-config
└── Handler reads /etc/agent-config/<namespace>/agent-index.yaml
```

**优势**:
- Handler不需要K8s API权限
- 无RBAC配置需求
- 与ShareProcessNamespace和ImageVolume模式兼容
- 支持动态Agent添加

---

## 原问题分析 (已解决)

### 原循环依赖问题
```
AgentRuntime Pod 创建 → 需要知道绑定的 agent 列表
                      → 需要挂载 agent ConfigMap

AIAgent binding → 需要 Runtime phase = "Running"
               → 需要 Pod 已经存在

结果：初始 Pod 创建时没有 agent bindings，无法挂载 agent volumes
```

### Controller 时序
```
1. AgentRuntime Controller: create Pod (没有 agent volumes)
2. Pod Running → Runtime phase = "Running"
3. AIAgent Controller: 看到 Runtime Running → 开始 binding
4. AIAgent Controller: create Agent ConfigMap + update AgentIndex
5. AgentRuntime Controller: 检测到 runtime.Status.Agents 变化
6. AgentRuntime Controller: update Pod (添加 agent volumes)
7. Pod 重启 (!) → Handler 重启 → Agent 加载中断
```

## 推荐解决方案: Handler 通过 K8s API 读取

### 原因
- 避免 Pod 重启
- 更灵活的 agent 管理
- 支持动态添加/删除 agents

### 实现方案

#### 1. Handler 使用 in-cluster config
```go
// cmd/adk-handler/main.go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

func createK8sClient() (*kubernetes.Clientset, error) {
    config, err := rest.InClusterConfig()
    if err != nil {
        return nil, err
    }
    return kubernetes.NewForConfig(config)
}
```

#### 2. Handler 通过 API 读取 Agent ConfigMap
```go
func loadAgentSpecViaAPI(client *kubernetes.Clientset, agentName, namespace string) (*v1.AIAgentSpec, error) {
    cmName := "agent-config-" + agentName
    cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
    if err != nil {
        return nil, err
    }
    
    agentYAML := cm.Data["agent.yaml"]
    var spec v1.AIAgentSpec
    if err := yaml.Unmarshal([]byte(agentYAML), &spec); err != nil {
        return nil, err
    }
    return &spec, nil
}
```

#### 3. RBAC 配置
```yaml
# handler-role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-handler
  namespace: <namespace>
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list"]
  resourceNames: ["agent-config-*"]  # 只允许读取 agent-config-* ConfigMaps

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-handler
subjects:
- kind: ServiceAccount
  name: agent-handler
roleRef:
  kind: Role
  name: agent-handler
```

#### 4. 修改 Handler main.go
```go
func loadAgentSpec(agentName string, agentConfigDir string, k8sClient *kubernetes.Clientset, namespace string) (*v1.AIAgentSpec, error) {
    // 优先从文件读取（如果 volume mount 存在）
    possiblePaths := []string{
        filepath.Join(agentConfigDir, "agent", agentName, "agent.yaml"),
        filepath.Join(agentConfigDir, agentName, "agent.yaml"),
    }
    
    for _, path := range possiblePaths {
        data, err := os.ReadFile(path)
        if err == nil {
            var spec v1.AIAgentSpec
            if err := yaml.Unmarshal(data, &spec); err != nil {
                return nil, err
            }
            return &spec, nil
        }
    }
    
    // Fallback: 通过 K8s API 读取
    if k8sClient != nil {
        return loadAgentSpecViaAPI(k8sClient, agentName, namespace)
    }
    
    // 最后返回 minimal spec
    return &v1.AIAgentSpec{
        Description: agentName,
        RuntimeRef:  v1.RuntimeReference{Type: "adk"},
    }, nil
}
```

## Agent.yaml 内容分析 (需要增强)

Controller 生成的 `agent.yaml` 包含：
```yaml
name: <agent-name>
description: <description>
runtimeRef:
  type: <framework-type>
  name: <runtime-name>
volumePolicy: retain/delete
harnessOverride:
  mcp:
    - name: <mcp-name>
      deny: false
  ...
```

但这个配置缺少关键信息：
- ❌ 没有 `instruction` (agent 提示词)
- ❌ 没有 `tools` 列表
- ❌ 没有具体的 model 选择

需要从 API spec 扩展或从 Harness 合并这些信息。

## 修复优先级

### P0: Handler 通过 K8s API 读取 Agent ConfigMap
- 修改 Handler main.go
- 添加 RBAC 配置
- 测试 API 读取

### P1: 增强 agent.yaml 内容生成
- 在 generateAgentConfigYAML() 中添加更多字段
- 或者让 Handler 直接从 Harness 合成这些信息

### P2 (可选): Agent ConfigMap 动态挂载
- 仅当不需要热添加 agents 时考虑
- 会导致 Pod 重启

## 实现步骤

### Step 1: 添加 Handler RBAC
```yaml
# test/e2e/kind/manifests/handler-rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: agent-handler
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agent-handler
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agent-handler
subjects:
- kind: ServiceAccount
  name: agent-handler
roleRef:
  kind: Role
  name: agent-handler
```

### Step 2: 修改 Handler main.go
添加 K8s client 初始化和使用。

### Step 3: 验证测试
确保 Handler 可以读取 agent ConfigMap。

## 结论

**已采用方案: Config Daemon (Solution M)**

通过Config Daemon将Agent配置写入hostPath，Handler通过hostPath挂载获取配置。此方案:
- ✓ 完全解决时序问题
- ✓ Handler无需K8s API权限
- ✓ 支持动态添加Agent
- ✓ 与ImageVolume和ShareProcessNamespace模式兼容

**E2E测试验证**:
- ✓ ADK Shared: 2 AIAgents → 1 Framework进程
- ✓ ADK Isolated: 3 AIAgents → 3 Framework进程
- ✓ OpenClaw Gateway: 2 AIAgents → 2 Gateway进程

**实现文件**: `cmd/config-daemon/main.go`, `Dockerfile.config-daemon`