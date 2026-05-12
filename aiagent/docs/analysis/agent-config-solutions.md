# Agent ConfigMap 信息流问题 - 方案对比分析

## 实现状态 (2026-05-12)

**已采用方案: Solution M (Config Daemon)**

Config Daemon方案已实现并通过E2E测试验证。该方案通过hostPath + DaemonSet模式解决了时序问题，Handler无需K8s API权限即可获取Agent配置。

---

## 已实现方案: Config Daemon (Solution M)

### 实现架构

```
Config Daemon (DaemonSet on all nodes)
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

### 实现文件

| 文件 | 功能 |
|------|------|
| `cmd/config-daemon/main.go` | Config Daemon实现 |
| `pkg/controller/agentruntime_controller.go` | hostPath Volume配置 |
| `Dockerfile.config-daemon` | Daemon镜像 |

### 优势

1. **无RBAC需求**: Handler不需要K8s API访问权限
2. **时序解耦**: Pod创建时hostPath已准备，不受binding时序影响
3. **动态支持**: 新增Agent只需更新CRD，Daemon自动同步
4. **兼容性**: 与ShareProcessNamespace和ImageVolume模式完全兼容

### E2E测试验证

- ✓ ADK Shared模式: 2 AIAgents → 1 Framework进程
- ✓ ADK Isolated模式: 3 AIAgents → 3 Framework进程
- ✓ OpenClaw Gateway模式: 2 AIAgents → 2 Gateway进程

---

## 问题核心 (已解决)

Handler启动Framework需要读取Agent ConfigMap，但原始设计中存在时序问题：

```
原始时序流程:
1. AgentRuntime Controller 创建 Pod (此时 runtime.Status.Agents 为空)
2. Pod Running → Runtime phase = "Running"
3. AIAgent Controller 检测到 Runtime Running → 开始 binding
4. AIAgent Controller 创建 Agent ConfigMap + 更新 AgentIndex
5. Handler 尝试读取 Agent ConfigMap → 路径为空或 ConfigMap 不存在
```

**Config Daemon解决方案**: 将Agent配置写入hostPath，Pod通过hostPath挂载获取配置，绕过ConfigMap挂载的时序依赖。

---

## 方案 A: 合并 AIAgent 和 AgentRuntime Controller

### 架构变更
```
当前架构:
┌─────────────────────────────────────────────────────┐
│ AIAgent Controller          AgentRuntime Controller │
│ - Scheduling                - Pod 创建              │
│ - Agent ConfigMap 创建      - Harness ConfigMap     │
│ - AgentIndex 更新           - Volume 挂载           │
│ - PVC 管理                                          │
└─────────────────────────────────────────────────────┘

合并后架构:
┌─────────────────────────────────────────────────────┐
│ AgentRuntime Controller (合并版)                    │
│ - 接收 AIAgent CRD Watch                            │
│ - 处理 scheduling + binding                         │
│ - 创建所有 ConfigMaps (Agent, Harness, Index)       │
│ - 创建 Pod 时已知道所有 agent                        │
│ - Volume 挂载所有 agent ConfigMaps                  │
└─────────────────────────────────────────────────────┘
```

### 实现细节

```go
// 合合后的 AgentRuntime Controller
func (r *AgentRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) {
    runtime := &v1.AgentRuntime{}
    r.Get(ctx, req.NamespacedName, runtime)
    
    // Step 1: 查询所有绑定到此 Runtime 的 AIAgent
    agents := &v1.AIAgentList{}
    r.List(ctx, agents, client.MatchingFields{"spec.runtimeRef.name": runtime.Name})
    
    // Step 2: 为每个 agent 创建 ConfigMap (原 AIAgent Controller 的工作)
    for _, agent := range agents.Items {
        r.createAgentConfigMap(ctx, &agent)
    }
    
    // Step 3: 更新 runtime.Status.Agents
    runtime.Status.Agents = buildAgentBindings(agents)
    runtime.Status.AgentCount = len(agents.Items)
    r.Status().Update(ctx, runtime)
    
    // Step 4: 创建 AgentIndex ConfigMap
    r.createAgentIndexConfigMap(ctx, runtime, agents)
    
    // Step 5: 创建 Pod (此时已知道所有 agent)
    pod := r.buildPodSpec(runtime, agents)  // 包含所有 agent volumes
    r.Create(ctx, pod)
}
```

### 优点
- ✅ 彻底解决时序问题，Pod 创建时已知道所有 agent
- ✅ 简化 Controller 数量，减少复杂度
- ✅ Volume 挂载自然，无需额外机制
- ✅ Agent binding 状态一致性更好保证

### 缺点
- ❌ 破坏原设计的职责分离 (AgentRuntime 管理 Runtime，AIAgent 管理 Agent)
- ❌ AgentRuntime Controller 变得更复杂
- ❌ 如果 agent 数量变化，仍需更新 Pod spec (触发 Pod 重建)
- ❌ 原设计的 "Agent 可以跨 Runtime 迁移" 语义受影响

### 适用场景
- Agent 与 Runtime 强绑定，不支持动态迁移
- Agent 数量在 Runtime 创建时就已确定
- 简化部署模型优先

---

## 方案 B: 改变 Binding 流程顺序

### 核心思路
让 Agent binding 在 Pod 创建之前完成。

### 流程变更
```
原流程:
Runtime 创建 → Pod 创建 → Runtime Running → Agent binding

新流程:
Runtime 创建 → 添加 "Pending" agents → 创建 Pod (带 agent volumes) → Runtime Running → Agent 已就绪
```

### 实现细节

```go
// AgentRuntime Controller: 预先声明 agents
func (r *AgentRuntimeReconciler) createOrUpdatePod(ctx context.Context, runtime *v1.AgentRuntime) {
    // 从 spec.agentConfig 或新字段获取预设 agent 列表
    // 或者从 spec.runtimeRef 约定的 agent 列表获取
    presetAgents := runtime.Spec.InitialAgents  // 新增字段
    
    // 为预设 agents 创建占位 ConfigMap (空内容或 minimal)
    for _, agentName := range presetAgents {
        r.createPlaceholderAgentConfigMap(ctx, agentName, runtime.Namespace)
    }
    
    // 创建 Pod (包含预设 agent volumes)
    pod := r.buildPodSpecWithAgents(runtime, presetAgents)
    r.Create(ctx, pod)
}

// AIAgent Controller: 填充 ConfigMap 内容
func (r *AIAgentReconciler) handleBinding(ctx context.Context, agent *v1.AIAgent) {
    // 不创建新 ConfigMap，而是更新已有的 placeholder
    cmName := "agent-config-" + agent.Name
    existingCM := &corev1.ConfigMap{}
    r.Get(ctx, cmName, existingCM)
    
    // 填充真实内容
    existingCM.Data["agent.yaml"] = r.generateAgentConfigYAML(agent)
    r.Update(ctx, existingCM)
    
    // 更新 AgentIndex
    r.updateAgentIndex(ctx, runtime, agent, "Running")
}
```

### 优点
- ✅ Pod 创建时已挂载 agent volumes
- ✅ ConfigMap 内容更新不会触发 Pod 重建
- ✅ 保持 Controller 分离设计

### 缺点
- ❌ 需要在 AgentRuntime spec 中预先声明 agent 列表 (限制灵活性)
- ❌ 无法动态添加新 agent (需要重建 Pod)
- ❌ 新增字段 "InitialAgents" 改变 CRD 结构
- ❌ 占位 ConfigMap 管理复杂

### 适用场景
- Agent 数量固定，创建时已知
- 类似 "Deployment + ReplicaSet" 的静态绑定模型

---

## 方案 C: Handler 从 AgentIndex ConfigMap 合成 Agent 配置

### 核心思路
Handler 不需要读取 Agent ConfigMap，而是从 AgentIndex + HarnessConfig 直接合成 Framework 配置。

### 信息流
```
Handler 输入:
1. AgentIndex ConfigMap (agent-index.yaml): agent 列表 + phase
2. Harness ConfigMap: model, mcp, skills 等配置
3. AgentIndex 包含的 configMap 名称 (仅作为引用，不读取)

Handler 输出:
- Framework config 由 Handler 根据 Harness 默认值 + Agent 名称合成
- 不依赖 Agent ConfigMap 的详细内容
```

### 实现细节

```go
// Handler loadAgentSpec: 不读取 ConfigMap，返回基础 spec
func loadAgentSpec(agentName string, agentConfigDir string) (*v1.AIAgentSpec, error) {
    // 返回 minimal spec，由 Harness 填充
    return &v1.AIAgentSpec{
        Description: agentName,
        RuntimeRef:  v1.RuntimeReference{Type: "adk"},
    }, nil
}

// Handler GenerateFrameworkConfig: 使用 Harness 填充
func (h *ADKHandler) GenerateFrameworkConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
    config := &ADKAgentConfig{
        Name:        spec.Description,  // 即 agentName
        Description: spec.Description,
        Model:       harnessCfg.Model.DefaultModel,  // 从 Harness 取
        Tools:       extractToolsFromHarness(harnessCfg),  // 从 Harness 合成
        Instruction: defaultInstruction,  // 默认值或从 spec 取
    }
    return yaml.Marshal(config)
}
```

### Agent.yaml 内容增强
即使不读取 Agent ConfigMap，Handler 也能生成有效配置：
- Model: 从 Harness.Model.DefaultModel
- Tools/Skills: 从 Harness.Skills + Harness.MCP
- Instruction: 默认模板或让 spec.Description 作为 instruction

### 优点
- ✅ 完全解决时序问题，Handler 不依赖 Agent ConfigMap
- ✅ 保持原设计不变
- ✅ 支持动态添加 agent (只需更新 AgentIndex)
- ✅ 简化 Handler 实现

### 缺点
- ❌ Agent 无法自定义配置 (instruction, tools override)
- ❌ 丢失 HarnessOverride 语义 (原设计允许 agent 覆盖 harness 配置)
- ❌ agent.yaml 内容变得无用 (或只用于基本信息)
- ❌ 不适合需要 agent 级别个性化的场景

### 适用场景
- 所有 agent 使用相同 Harness 配置
- 不需要 agent 级别的 instruction 或 tools override
- 快速原型或简化部署

---

## 方案 D: 使用 projected volume + 动态更新

### 核心思路
使用 Kubernetes projected volume 将多个 ConfigMap 投射到同一目录，配合动态更新。

### 实现细节

```go
// AgentRuntime Controller: 使用 projected volume
func (r *AgentRuntimeReconciler) buildPodSpec(runtime *v1.AgentRuntime) corev1.PodSpec {
    // 构建 projected volume sources
    projectedSources := []corev1.VolumeProjection{}
    
    // 添加 AgentIndex
    projectedSources = append(projectedSources, corev1.VolumeProjection{
        ConfigMap: &corev1.ConfigMapProjection{
            LocalObjectReference: corev1.LocalObjectReference{
                Name: "agent-index-" + runtime.Name,
            },
        },
    })
    
    // 添加已知的 agent ConfigMaps
    for _, binding := range runtime.Status.Agents {
        projectedSources = append(projectedSources, corev1.VolumeProjection{
            ConfigMap: &corev1.ConfigMapProjection{
                LocalObjectReference: corev1.LocalObjectReference{
                    Name: "agent-config-" + binding.Name,
                },
                Items: []corev1.KeyToPath{
                    {Key: "agent.yaml", Path: binding.Name + "/agent.yaml"},
                },
            },
        })
    }
    
    volumes := []corev1.Volume{
        {
            Name: "agent-configs",
            VolumeSource: corev1.VolumeSource{
                Projected: &corev1.ProjectedVolumeSource{
                    Sources: projectedSources,
                },
            },
        },
    }
    
    // 单一挂载点
    handlerVolumeMounts := []corev1.VolumeMount{
        {Name: "agent-configs", MountPath: "/etc/agent-config"},
    }
}
```

### 动态更新机制
```go
// 监听 runtime.Status.Agents 变化
func (r *AgentRuntimeReconciler) watchAgentBindings(ctx context.Context) {
    // 当 agents 变化时，更新 Pod spec
    // projected volume 的 ConfigMap 列表会更新
    // Pod 可能重启 (取决于 kubelet 行为)
}
```

### 优点
- ✅ 统一的挂载路径结构
- ✅ Volume 数量减少 (单一 projected volume)
- ✅ 保持原设计

### 缺点
- ❌ 仍然存在时序问题 (Pod 创建时 runtime.Status.Agents 为空)
- ❌ projected volume 更新可能触发 Pod 行为变化
- ❌ 需要处理动态添加 agent 的场景

---

## 方案 E: Agent ConfigMap 内容写入 AgentIndex

### 核心思路
将 agent.yaml 内容直接嵌入 AgentIndex ConfigMap，Handler 只读取一个 ConfigMap。

### AgentIndex 结构变更
```yaml
# 原 agent-index.yaml
agents:
  - name: agent-1
    namespace: default
    configMap: agent-config-agent-1
    phase: Running

# 新 agent-index.yaml (嵌入内容)
agents:
  - name: agent-1
    namespace: default
    phase: Running
    config:
      name: agent-1
      description: "First agent"
      runtimeRef:
        type: adk
        name: runtime-1
      harnessOverride:
        model:
          - name: model-harness
            allowedModels: [gpt-4]
```

### 实现细节

```go
// AIAgent Controller: 将 agent.yaml 嵌入 AgentIndex
func (r *AIAgentReconciler) updateAgentIndex(ctx context.Context, runtime *v1.AgentRuntime, agent *v1.AIAgent, phase string) {
    // AgentIndex entry 包含完整配置
    entry := AgentIndexEntry{
        Name:      agent.Name,
        Namespace: agent.Namespace,
        Phase:     phase,
        Config:    r.generateAgentConfigYAML(agent),  // 嵌入配置内容
    }
    
    // 更新 AgentIndex ConfigMap
    // Handler 只读取这一个 ConfigMap
}
```

```go
// Handler: 从 AgentIndex 直接解析 agent 配置
func loadAgentsFromIndex(indexPath string) ([]*v1.AIAgentSpec, error) {
    data, _ := os.ReadFile(indexPath)
    var index AgentIndex
    yaml.Unmarshal(data, &index)
    
    specs := []*v1.AIAgentSpec{}
    for _, entry := range index.Agents {
        // 直接从 entry.Config 解析
        var spec v1.AIAgentSpec
        yaml.Unmarshal([]byte(entry.Config), &spec)
        specs = append(specs, &spec)
    }
    return specs, nil
}
```

### 优点
- ✅ Handler 只需读取一个 ConfigMap
- ✅ 解决 Agent ConfigMap 挂载时序问题
- ✅ 支持 agent 级别的个性化配置
- ✅ 动态添加 agent 只需更新 AgentIndex (无需 Pod 重建)

### 缺点
- ❌ AgentIndex ConfigMap 变大 (每个 agent 的完整配置)
- ❌ 如果配置敏感 (API key 等)，需要特殊处理
- ❌ 原设计的 "agent-config-%s" ConfigMap 变得多余
- ❌ 更新 AgentIndex 需要合并所有 agent 配置

### 适用场景
- Agent 配置较小
- 需要保留 agent 级别个性化
- 动态添加 agent

---

## 方案对比表

| 方案 | 时序问题 | 动态添加 Agent | Agent 个性化 | 实现复杂度 | 设计影响 |
|------|---------|---------------|-------------|-----------|---------|
| A: 合合 Controller | ✅ 完全解决 | ❌ 需重建 Pod | ✅ 支持 | 中 | 大 |
| B: 预设 Agent 列表 | ✅ 完全解决 | ❌ 需重建 Pod | ✅ 支持 | 高 | 中 |
| C: Handler 合成配置 | ✅ 完全解决 | ✅ 支持 | ❌ 无个性化 | 低 | 小 |
| D: projected volume | ❌ 未解决 | ❌ 可能重启 | ✅ 支持 | 中 | 小 |
| E: AgentIndex 嵌入 | ✅ 完全解决 | ✅ 支持 | ✅ 支持 | 中 | 中 |

---

## 推荐方案

### 推荐: 方案 E (AgentIndex 嵌入配置)

**理由:**
1. 完全解决时序问题，Pod 创建前 AgentIndex 已存在
2. 支持动态添加 agent，无需 Pod 重建
3. 保留 agent 级别个性化配置
4. 实现复杂度适中
5. Handler 读取逻辑简化 (只读一个文件)

### 备选: 方案 C (Handler 合成配置)

**适用场景:**
- 快速原型
- 不需要 agent 个性化
- 所有 agent 共享 Harness 配置

---

## 方案 E 实现步骤

### Step 1: 修改 AgentIndex 结构
```go
// pkg/controller/aigent_controller.go
type AgentIndexEntry struct {
    Name      string `yaml:"name"`
    Namespace string `yaml:"namespace"`
    Phase     string `yaml:"phase"`
    UID       string `yaml:"uid,omitempty"`
    
    // 新增: 嵌入 agent 配置
    Config    string `yaml:"config"`  // YAML 格式的 agent.yaml 内容
}
```

### Step 2: 修改 generateAgentIndexYAML
```go
func (r *AIAgentReconciler) generateAgentIndexYAML(entries []AgentIndexEntry) string {
    result := "agents:\n"
    for _, e := range entries {
        result += fmt.Sprintf("  - name: %s\n    namespace: %s\n    phase: %s\n    uid: %s\n    config:\n%s\n",
            e.Name, e.Namespace, e.Phase, e.UID, indentConfig(e.Config))
    }
    return result
}

func indentConfig(config string) string {
    // 将 agent.yaml 内容缩进 6 spaces (配合 YAML 结构)
    lines := strings.Split(config, "\n")
    result := ""
    for _, line := range lines {
        result += "      " + line + "\n"
    }
    return result
}
```

### Step 3: 修改 updateAgentIndex
```go
func (r *AIAgentReconciler) updateAgentIndex(ctx context.Context, runtime *v1.AgentRuntime, agent *v1.AIAgent, phase string) {
    entry := AgentIndexEntry{
        Name:      agent.Name,
        Namespace: agent.Namespace,
        Phase:     phase,
        UID:       string(agent.UID),
        Config:    r.generateAgentConfigYAML(agent),  // 嵌入完整配置
    }
    // ...
}
```

### Step 4: 修改 Handler loadAgentsFromIndex
```go
// cmd/adk-handler/main.go
type AgentIndexEntry struct {
    Name      string `yaml:"name"`
    Namespace string `yaml:"namespace"`
    Phase     string `yaml:"phase"`
    Config    string `yaml:"config"`
}

func loadAgentSpecFromIndex(agentName string, indexPath string) (*v1.AIAgentSpec, error) {
    data, err := os.ReadFile(indexPath)
    if err != nil {
        return nil, err
    }
    
    var index AgentIndex
    if err := yaml.Unmarshal(data, &index); err != nil {
        return nil, err
    }
    
    for _, entry := range index.Agents {
        if entry.Name == agentName {
            // 直接解析嵌入的配置
            var spec v1.AIAgentSpec
            if err := yaml.Unmarshal([]byte(entry.Config), &spec); err != nil {
                return nil, err
            }
            return &spec, nil
        }
    }
    
    // 未找到，返回 minimal spec
    return &v1.AIAgentSpec{Description: agentName}, nil
}
```

### Step 5: 移除 Agent ConfigMap 创建 (可选)
如果方案 E 完全满足需求，可以移除 `createAgentConfigMap()`:
- AgentIndex 已包含所有信息
- 不再需要单独的 agent-config-%s ConfigMap

---

## 需要决策的问题

1. **是否采用方案 E?**
2. **是否保留 Agent ConfigMap 作为备份?**
3. **AgentIndex ConfigMap 大小限制?**
4. **敏感信息 (如 API key) 如何处理?**