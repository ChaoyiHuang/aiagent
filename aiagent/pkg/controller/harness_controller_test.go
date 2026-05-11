// Package controller provides Kubernetes controllers for AI Agent resources.
package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"aiagent/api/v1"
)

func newTestHarness(name, namespace string, harnessType v1.HarnessType) *v1.Harness {
	harness := &v1.Harness{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.HarnessSpec{
			Type: harnessType,
		},
	}

	// Add type-specific spec based on harness type
	switch harnessType {
	case v1.HarnessTypeModel:
		harness.Spec.Model = &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			Endpoint:     "https://api.deepseek.com",
			DefaultModel: "deepseek-chat",
			Models: []v1.ModelConfig{
				{Name: "deepseek-chat", Allowed: true},
			},
		}
	case v1.HarnessTypeMCP:
		harness.Spec.MCP = &v1.MCPHarnessSpec{
			RegistryType:     "local",
			Endpoint:         "http://localhost:8080",
			DiscoveryEnabled: true,
			Servers: []v1.MCPServerConfig{
				{Name: "filesystem", Type: "local", Allowed: true},
			},
		}
	case v1.HarnessTypeMemory:
		harness.Spec.Memory = &v1.MemoryHarnessSpec{
			Type:              "redis",
			Endpoint:          "redis://localhost:6379",
			TTL:               3600,
			MaxSize:           "100MB",
			PersistenceEnabled: true,
		}
	case v1.HarnessTypeSandbox:
		harness.Spec.Sandbox = &v1.SandboxHarnessSpec{
			Type:     "gvisor",
			Mode:     v1.SandboxModeEmbedded,
			Endpoint: "",
			Timeout:  300,
		}
	case v1.HarnessTypeSkills:
		harness.Spec.Skills = &v1.SkillsHarnessSpec{
			HubType:    "local",
			Endpoint:   "",
			LocalPath:  "/skills",
			AutoUpdate: false,
			Skills: []v1.SkillConfig{
				{Name: "code-generator", Version: "1.0.0", Allowed: true},
			},
		}
	case v1.HarnessTypeKnowledge:
		harness.Spec.Knowledge = &v1.KnowledgeHarnessSpec{
			Type:           "vector",
			Endpoint:       "http://localhost:8081",
			EmbeddingModel: "text-embedding-3-small",
			Collections: []string{"docs", "examples"},
		}
	case v1.HarnessTypeState:
		harness.Spec.State = &v1.StateHarnessSpec{
			Type:       "file",
			Endpoint:   "/state",
			SessionTTL: 1800,
		}
	case v1.HarnessTypeGuardrail:
		harness.Spec.Guardrail = &v1.GuardrailHarnessSpec{
			Type:    "bedrock",
			Endpoint: "https://bedrock.amazonaws.com",
			Enabled: true,
			Rules: []v1.GuardrailRule{
				{Name: "content-filter", Type: "content", Severity: "high", Action: "block"},
			},
		}
	case v1.HarnessTypeSecurity:
		harness.Spec.Security = &v1.SecurityHarnessSpec{
			AuditEnabled: true,
			AuditLogPath: "/var/log/audit.log",
			Policies: []v1.SecurityPolicy{
				{Name: "default", Type: "rbac"},
			},
		}
	case v1.HarnessTypePolicy:
		harness.Spec.Policy = &v1.PolicyHarnessSpec{
			DefaultAction: "allow",
			Rules: []v1.PolicyRule{
				{Name: "deny-sensitive", Condition: "contains_secret", Action: "deny"},
			},
		}
	}

	return harness
}

func newTestHarnessReconciler() *HarnessReconciler {
	scheme := runtime.NewScheme()
	// Register both API groups properly
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Use the scheme from the API package for proper registration
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1.Harness{}, &corev1.ConfigMap{}).
		Build()

	return &HarnessReconciler{
		Client: client,
		Scheme: scheme,
	}
}

func TestHarnessReconciler_Reconcile_Create(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	// Create a test harness
	harness := newTestHarness("test-model", "default", v1.HarnessTypeModel)
	if err := reconciler.Create(ctx, harness); err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-model",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should requeue after adding finalizer
	if !result.Requeue {
		t.Errorf("expected requeue after adding finalizer")
	}

	// Verify finalizer was added
	harnessAfter := &v1.Harness{}
	if err := reconciler.Get(ctx, req.NamespacedName, harnessAfter); err != nil {
		t.Fatalf("failed to get harness: %v", err)
	}

	if !containsFinalizer(harnessAfter, HarnessFinalizer) {
		t.Errorf("expected finalizer to be added")
	}
}

func TestHarnessReconciler_Reconcile_ConfigMapCreation(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	// Create harness with finalizer already set
	harness := newTestHarness("test-sandbox", "default", v1.HarnessTypeSandbox)
	controllerutil.AddFinalizer(harness, HarnessFinalizer)
	if err := reconciler.Create(ctx, harness); err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-sandbox",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify ConfigMap was created
	cmName := "test-sandbox" + HarnessConfigSuffix
	cm := &corev1.ConfigMap{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: cmName, Namespace: "default"}, cm); err != nil {
		t.Errorf("expected ConfigMap to be created: %v", err)
	}

	// Verify ConfigMap has correct labels
	if cm.Labels["agent.ai/harness"] != "test-sandbox" {
		t.Errorf("expected harness label, got %s", cm.Labels["agent.ai/harness"])
	}

	if cm.Labels["agent.ai/harness-type"] != "sandbox" {
		t.Errorf("expected harness-type label, got %s", cm.Labels["agent.ai/harness-type"])
	}

	// Verify harness type in data
	if cm.Data["harness-type"] != "sandbox" {
		t.Errorf("expected harness-type data, got %s", cm.Data["harness-type"])
	}
}

func TestHarnessReconciler_handleDeletion(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	// Create harness with finalizer
	harness := newTestHarness("test-delete", "default", v1.HarnessTypeMemory)
	controllerutil.AddFinalizer(harness, HarnessFinalizer)
	// Set deletion timestamp to simulate deletion state (this is for testing handleDeletion directly)
	now := metav1.Now()
	harness.DeletionTimestamp = &now
	if err := reconciler.Create(ctx, harness); err != nil {
		t.Fatalf("failed to create test harness: %v", err)
	}

	// Create associated ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-delete" + HarnessConfigSuffix,
			Namespace: "default",
		},
	}
	if err := reconciler.Create(ctx, cm); err != nil {
		t.Fatalf("failed to create test ConfigMap: %v", err)
	}

	// Test handleDeletion directly
	_, err := reconciler.handleDeletion(ctx, harness)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify ConfigMap was deleted
	cmAfter := &corev1.ConfigMap{}
	err = reconciler.Get(ctx, types.NamespacedName{Name: "test-delete" + HarnessConfigSuffix, Namespace: "default"}, cmAfter)
	if err == nil {
		t.Errorf("expected ConfigMap to be deleted")
	}

	// Note: The harness won't be "deleted" in fake client, but finalizer should be removed
	// In real Kubernetes, after finalizer removal, the object is deleted by the API server
}

func TestHarnessReconciler_GenerateModelConfig(t *testing.T) {
	reconciler := newTestHarnessReconciler()

	spec := &v1.ModelHarnessSpec{
		Provider:     "openai",
		Endpoint:     "https://api.openai.com",
		DefaultModel: "gpt-4",
		Models: []v1.ModelConfig{
			{Name: "gpt-4", Allowed: true},
			{Name: "gpt-3.5-turbo", Allowed: false},
		},
	}

	config := reconciler.generateModelConfig(spec)

	// Verify config contains expected values
	if !containsString(config, "provider: openai") {
		t.Errorf("expected provider in config")
	}

	if !containsString(config, "endpoint: https://api.openai.com") {
		t.Errorf("expected endpoint in config")
	}

	if !containsString(config, "defaultModel: gpt-4") {
		t.Errorf("expected defaultModel in config")
	}
}

func TestHarnessReconciler_GenerateSandboxConfig(t *testing.T) {
	reconciler := newTestHarnessReconciler()

	spec := &v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal,
		Endpoint: "https://sandbox.example.com",
		Timeout:  600,
		ResourceLimits: &v1.SandboxResourceLimits{
			CPU:    "2",
			Memory: "4Gi",
			PIDs:   100,
		},
		NetworkPolicy: &v1.SandboxNetworkPolicy{
			AllowOutbound: true,
			AllowInbound:  false,
		},
	}

	config := reconciler.generateSandboxConfig(spec)

	// Verify config contains expected values
	if !containsString(config, "type: gvisor") {
		t.Errorf("expected type in config")
	}

	if !containsString(config, "mode: external") {
		t.Errorf("expected mode in config")
	}

	if !containsString(config, "endpoint: https://sandbox.example.com") {
		t.Errorf("expected endpoint in config")
	}

	if !containsString(config, "timeout: 600") {
		t.Errorf("expected timeout in config")
	}
}

func TestHarnessReconciler_GenerateSkillsConfig(t *testing.T) {
	reconciler := newTestHarnessReconciler()

	spec := &v1.SkillsHarnessSpec{
		HubType:    "hub",
		Endpoint:   "https://skills.example.com",
		LocalPath:  "",
		AutoUpdate: true,
		Skills: []v1.SkillConfig{
			{Name: "web-search", Version: "2.0.0", Allowed: true},
			{Name: "code-review", Version: "1.5.0", Allowed: false},
		},
	}

	config := reconciler.generateSkillsConfig(spec)

	// Verify config contains expected values
	if !containsString(config, "hubType: hub") {
		t.Errorf("expected hubType in config")
	}

	if !containsString(config, "endpoint: https://skills.example.com") {
		t.Errorf("expected endpoint in config")
	}

	if !containsString(config, "autoUpdate: true") {
		t.Errorf("expected autoUpdate in config")
	}
}

func TestHarnessReconciler_CheckHarnessHealth(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	tests := []struct {
		name     string
		harness  *v1.Harness
		expected bool
	}{
		{
			name:     "model harness with provider",
			harness:  newTestHarness("test", "default", v1.HarnessTypeModel),
			expected: true,
		},
		{
			name:     "memory harness with type",
			harness:  newTestHarness("test", "default", v1.HarnessTypeMemory),
			expected: true,
		},
		{
			name:     "sandbox embedded mode",
			harness:  newTestHarness("test", "default", v1.HarnessTypeSandbox),
			expected: true,
		},
		{
			name: "sandbox external mode with endpoint",
			harness: func() *v1.Harness {
				h := newTestHarness("test", "default", v1.HarnessTypeSandbox)
				h.Spec.Sandbox.Mode = v1.SandboxModeExternal
				h.Spec.Sandbox.Endpoint = "https://sandbox.example.com"
				return h
			}(),
			expected: true,
		},
		{
			name: "sandbox external mode without endpoint",
			harness: func() *v1.Harness {
				h := newTestHarness("test", "default", v1.HarnessTypeSandbox)
				h.Spec.Sandbox.Mode = v1.SandboxModeExternal
				h.Spec.Sandbox.Endpoint = ""
				return h
			}(),
			expected: false,
		},
		{
			name:     "unknown harness type defaults to healthy",
			harness:  func() *v1.Harness { h := newTestHarness("test", "default", v1.HarnessTypeGuardrail); return h }(),
			expected: true, // Default case returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			healthy, _ := reconciler.checkHarnessHealth(ctx, tt.harness)
			if healthy != tt.expected {
				t.Errorf("expected healthy=%v, got %v", tt.expected, healthy)
			}
		})
	}
}

func TestHarnessReconciler_UpdateStatus(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	// Create harness
	harness := newTestHarness("test-status", "default", v1.HarnessTypeModel)
	harness.Status.Phase = "" // Initial empty phase
	if err := reconciler.Create(ctx, harness); err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}

	// Update status
	reconciler.updateStatus(ctx, harness, v1.HarnessPhaseAvailable, "")

	// Verify status was updated
	harnessAfter := &v1.Harness{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "test-status", Namespace: "default"}, harnessAfter); err != nil {
		t.Fatalf("failed to get harness: %v", err)
	}

	if harnessAfter.Status.Phase != v1.HarnessPhaseAvailable {
		t.Errorf("expected phase Available, got %s", harnessAfter.Status.Phase)
	}

	if harnessAfter.Status.ConnectionStatus != "connected" {
		t.Errorf("expected connection status 'connected', got %s", harnessAfter.Status.ConnectionStatus)
	}
}

func TestHarnessReconciler_UpdateStatusWithError(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestHarnessReconciler()

	// Create harness
	harness := newTestHarness("test-error", "default", v1.HarnessTypeModel)
	harness.Status.Phase = "" // Initial empty phase
	if err := reconciler.Create(ctx, harness); err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}

	// Update status with error
	reconciler.updateStatus(ctx, harness, v1.HarnessPhaseError, "connection failed")

	// Verify status was updated
	harnessAfter := &v1.Harness{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "test-error", Namespace: "default"}, harnessAfter); err != nil {
		t.Fatalf("failed to get harness: %v", err)
	}

	if harnessAfter.Status.Phase != v1.HarnessPhaseError {
		t.Errorf("expected phase Error, got %s", harnessAfter.Status.Phase)
	}

	if harnessAfter.Status.ConnectionStatus != "error: connection failed" {
		t.Errorf("expected error connection status, got %s", harnessAfter.Status.ConnectionStatus)
	}
}

// Helper functions
func containsFinalizer(obj metav1.Object, finalizer string) bool {
	for _, f := range obj.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}