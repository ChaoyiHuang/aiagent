package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAIAgentSpec_RuntimeRef(t *testing.T) {
	agent := &AIAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-agent",
			Namespace: "default",
		},
		Spec: AIAgentSpec{
			RuntimeRef: RuntimeReference{
				Type: "adk",
			},
			Description: "Test agent",
			VolumePolicy: VolumePolicyRetain,
		},
	}

	if agent.Spec.RuntimeRef.Type != "adk" {
		t.Errorf("expected runtime type 'adk', got '%s'", agent.Spec.RuntimeRef.Type)
	}

	if agent.Spec.VolumePolicy != VolumePolicyRetain {
		t.Errorf("expected volume policy 'retain', got '%s'", agent.Spec.VolumePolicy)
	}
}

func TestAIAgentSpec_RuntimeRefByName(t *testing.T) {
	agent := &AIAgent{
		Spec: AIAgentSpec{
			RuntimeRef: RuntimeReference{
				Name: "runtime-001",
			},
		},
	}

	if agent.Spec.RuntimeRef.Name != "runtime-001" {
		t.Errorf("expected runtime name 'runtime-001', got '%s'", agent.Spec.RuntimeRef.Name)
	}
}

func TestAIAgentSpec_HarnessOverride(t *testing.T) {
	agent := &AIAgent{
		Spec: AIAgentSpec{
			HarnessOverride: HarnessOverrideSpec{
				MCP: []MCPOverrideSpec{
					{
						Name:          "mcp-registry-default",
						AllowedServers: []string{"github", "browser"},
						DeniedServers:  []string{"filesystem"},
					},
				},
			},
		},
	}

	if len(agent.Spec.HarnessOverride.MCP) != 1 {
		t.Errorf("expected 1 MCP override, got %d", len(agent.Spec.HarnessOverride.MCP))
	}

	mcpOverride := agent.Spec.HarnessOverride.MCP[0]
	if mcpOverride.Name != "mcp-registry-default" {
		t.Errorf("expected MCP override name 'mcp-registry-default', got '%s'", mcpOverride.Name)
	}

	if len(mcpOverride.AllowedServers) != 2 {
		t.Errorf("expected 2 allowed servers, got %d", len(mcpOverride.AllowedServers))
	}
}

func TestAIAgentStatus_Phase(t *testing.T) {
	status := AIAgentStatus{
		Phase: AgentPhaseRunning,
		RuntimeRef: RuntimeReferenceStatus{
			Name: "runtime-001",
		},
	}

	if status.Phase != AgentPhaseRunning {
		t.Errorf("expected phase 'Running', got '%s'", status.Phase)
	}

	if status.RuntimeRef.Name != "runtime-001" {
		t.Errorf("expected runtime name 'runtime-001', got '%s'", status.RuntimeRef.Name)
	}
}

func TestAgentRuntimeSpec_ProcessMode(t *testing.T) {
	runtime := &AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "runtime-001",
			Namespace: "default",
		},
		Spec: AgentRuntimeSpec{
			AgentHandler: AgentHandlerSpec{
				Image: "adk-handler:v1.0.0",
			},
			AgentFramework: AgentFrameworkSpec{
				Image: "adk-runtime:v1.0.0",
				Type:  "adk",
			},
			Replicas:     1,
			ProcessMode:  ProcessModeShared,
		},
	}

	if runtime.Spec.ProcessMode != ProcessModeShared {
		t.Errorf("expected process mode 'shared', got '%s'", runtime.Spec.ProcessMode)
	}

	if runtime.Spec.AgentFramework.Type != "adk" {
		t.Errorf("expected framework type 'adk', got '%s'", runtime.Spec.AgentFramework.Type)
	}
}

func TestAgentRuntimeSpec_HarnessReferences(t *testing.T) {
	runtime := &AgentRuntime{
		Spec: AgentRuntimeSpec{
			Harness: []HarnessReference{
				{Name: "model-deepseek"},
				{Name: "mcp-registry-default"},
				{Name: "redis-memory"},
			},
		},
	}

	if len(runtime.Spec.Harness) != 3 {
		t.Errorf("expected 3 harness references, got %d", len(runtime.Spec.Harness))
	}

	expectedNames := []string{"model-deepseek", "mcp-registry-default", "redis-memory"}
	for i, ref := range runtime.Spec.Harness {
		if ref.Name != expectedNames[i] {
			t.Errorf("expected harness[%d] name '%s', got '%s'", i, expectedNames[i], ref.Name)
		}
	}
}

func TestAgentRuntimeStatus_Phase(t *testing.T) {
	status := AgentRuntimeStatus{
		Phase:         RuntimePhaseRunning,
		ReadyReplicas: 1,
		AgentCount:    3,
		PodIPs:        []string{"10.0.0.1"},
	}

	if status.Phase != RuntimePhaseRunning {
		t.Errorf("expected phase 'Running', got '%s'", status.Phase)
	}

	if status.AgentCount != 3 {
		t.Errorf("expected agent count 3, got %d", status.AgentCount)
	}

	if len(status.PodIPs) != 1 {
		t.Errorf("expected 1 Pod IP, got %d", len(status.PodIPs))
	}
}

func TestHarnessSpec_ModelType(t *testing.T) {
	harness := &Harness{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "model-deepseek",
			Namespace: "default",
		},
		Spec: HarnessSpec{
			Type: HarnessTypeModel,
			Model: &ModelHarnessSpec{
				Provider:     "deepseek",
				Endpoint:     "https://api.deepseek.com/v1",
				DefaultModel: "deepseek-chat",
				Models: []ModelConfig{
					{Name: "deepseek-chat", Allowed: true},
					{Name: "deepseek-coder", Allowed: true},
				},
			},
		},
	}

	if harness.Spec.Type != HarnessTypeModel {
		t.Errorf("expected harness type 'model', got '%s'", harness.Spec.Type)
	}

	if harness.Spec.Model.Provider != "deepseek" {
		t.Errorf("expected provider 'deepseek', got '%s'", harness.Spec.Model.Provider)
	}

	if len(harness.Spec.Model.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(harness.Spec.Model.Models))
	}
}

func TestHarnessSpec_MCPType(t *testing.T) {
	harness := &Harness{
		Spec: HarnessSpec{
			Type: HarnessTypeMCP,
			MCP: &MCPHarnessSpec{
				RegistryType: "builtin",
				Servers: []MCPServerConfig{
					{Name: "github", Type: "builtin", Allowed: true},
					{Name: "filesystem", Type: "builtin", Allowed: false},
				},
			},
		},
	}

	if harness.Spec.Type != HarnessTypeMCP {
		t.Errorf("expected harness type 'mcp', got '%s'", harness.Spec.Type)
	}

	if len(harness.Spec.MCP.Servers) != 2 {
		t.Errorf("expected 2 MCP servers, got %d", len(harness.Spec.MCP.Servers))
	}
}

func TestHarnessSpec_SandboxType(t *testing.T) {
	harness := &Harness{
		Spec: HarnessSpec{
			Type: HarnessTypeSandbox,
			Sandbox: &SandboxHarnessSpec{
				Type:       "gvisor",
				Mode:       SandboxModeExternal,
				TemplateRef: "secure-template",
				ResourceLimits: &SandboxResourceLimits{
					CPU:    "1",
					Memory: "512Mi",
				},
			},
		},
	}

	if harness.Spec.Type != HarnessTypeSandbox {
		t.Errorf("expected harness type 'sandbox', got '%s'", harness.Spec.Type)
	}

	if harness.Spec.Sandbox.Mode != SandboxModeExternal {
		t.Errorf("expected sandbox mode 'external', got '%s'", harness.Spec.Sandbox.Mode)
	}
}

func TestGroupVersion(t *testing.T) {
	if GroupVersion.Group != "agent.ai" {
		t.Errorf("expected group 'agent.ai', got '%s'", GroupVersion.Group)
	}

	if GroupVersion.Version != "v1" {
		t.Errorf("expected version 'v1', got '%s'", GroupVersion.Version)
	}
}

func TestResource(t *testing.T) {
	gr := Resource("aiagents")

	if gr.Group != "agent.ai" {
		t.Errorf("expected group 'agent.ai', got '%s'", gr.Group)
	}

	if gr.Resource != "aiagents" {
		t.Errorf("expected resource 'aiagents', got '%s'", gr.Resource)
	}
}

func TestAIAgentList(t *testing.T) {
	list := AIAgentList{
		Items: []AIAgent{
			{ObjectMeta: metav1.ObjectMeta{Name: "agent-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "agent-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(list.Items))
	}
}

func TestAgentRuntimeList(t *testing.T) {
	list := AgentRuntimeList{
		Items: []AgentRuntime{
			{ObjectMeta: metav1.ObjectMeta{Name: "runtime-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "runtime-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(list.Items))
	}
}

func TestHarnessList(t *testing.T) {
	list := HarnessList{
		Items: []Harness{
			{ObjectMeta: metav1.ObjectMeta{Name: "model-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "mcp-1"}},
		},
	}

	if len(list.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(list.Items))
	}
}