// Package integration provides integration tests for Kubernetes controllers.
// These tests use envtest to simulate a real Kubernetes environment.
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"aiagent/api/v1"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.Background())

	// Setup envtest environment
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{os.Getenv("CRD_DIR")},
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
	}

	// If CRD_DIR is not set, skip integration tests
	if os.Getenv("CRD_DIR") == "" || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Println("Skipping controller integration tests: CRD_DIR or KUBEBUILDER_ASSETS not set")
		fmt.Println("To run these tests:")
		fmt.Println("  1. Generate CRDs: make manifests")
		fmt.Println("  2. Set env vars: export CRD_DIR=config/crd/bases")
		fmt.Println("  3. Install envtest: make envtest")
		os.Exit(0)
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Printf("Failed to start envtest: %v\n", err)
		os.Exit(1)
	}

	// Register scheme
	_ = v1.AddToScheme(scheme.Scheme)
	_ = corev1.AddToScheme(scheme.Scheme)

	// Create client
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		fmt.Printf("Failed to create client: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	cancel()
	if err := testEnv.Stop(); err != nil {
		fmt.Printf("Failed to stop envtest: %v\n", err)
	}

	os.Exit(code)
}

// TestHarnessCRD_Integration tests Harness CRD lifecycle.
func TestHarnessCRD_Integration(t *testing.T) {
	if k8sClient == nil {
		t.Skip("Integration tests skipped - envtest not configured")
	}

	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "harness-crd-test",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	// Cleanup namespace at end
	defer func() {
		_ = k8sClient.Delete(ctx, ns)
	}()

	// Test Case 1: Create Model Harness
	t.Log("Test Case 1: Create Model Harness")

	harness := &v1.Harness{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model-harness",
			Namespace: ns.Name,
		},
		Spec: v1.HarnessSpec{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				Endpoint:     "https://api.deepseek.com",
				DefaultModel: "deepseek-chat",
				Models: []v1.ModelConfig{
					{Name: "deepseek-chat", Allowed: true},
					{Name: "deepseek-reasoner", Allowed: true},
				},
			},
		},
	}

	if err := k8sClient.Create(ctx, harness); err != nil {
		t.Fatalf("Failed to create Harness: %v", err)
	}
	t.Logf("Created Harness: %s", harness.Name)

	// Test Case 2: Get Harness and verify
	t.Log("Test Case 2: Get Harness and verify")

	harnessCheck := &v1.Harness{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: harness.Name, Namespace: ns.Name}, harnessCheck); err != nil {
		t.Fatalf("Failed to get Harness: %v", err)
	}

	if harnessCheck.Spec.Type != v1.HarnessTypeModel {
		t.Errorf("Expected Model type, got %s", harnessCheck.Spec.Type)
	}
	if harnessCheck.Spec.Model.Provider != "deepseek" {
		t.Errorf("Expected deepseek provider, got %s", harnessCheck.Spec.Model.Provider)
	}

	// Test Case 3: Update Harness status
	t.Log("Test Case 3: Update Harness status")

	harnessCheck.Status.Phase = v1.HarnessPhaseAvailable
	harnessCheck.Status.ConnectionStatus = "connected"
	harnessCheck.Status.LastSyncTime = metav1.Now()

	if err := k8sClient.Status().Update(ctx, harnessCheck); err != nil {
		t.Fatalf("Failed to update Harness status: %v", err)
	}
	t.Logf("Updated Harness status to Available")

	// Test Case 4: Update Harness spec
	t.Log("Test Case 4: Update Harness spec")

	harnessCheck.Spec.Model.DefaultModel = "deepseek-reasoner"
	if err := k8sClient.Update(ctx, harnessCheck); err != nil {
		t.Fatalf("Failed to update Harness: %v", err)
	}
	t.Logf("Updated Harness default model to deepseek-reasoner")

	// Test Case 5: Delete Harness
	t.Log("Test Case 5: Delete Harness")

	if err := k8sClient.Delete(ctx, harness); err != nil {
		t.Fatalf("Failed to delete Harness: %v", err)
	}
	t.Logf("Deleted Harness")

	// Verify deletion
	harnessAfter := &v1.Harness{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: harness.Name, Namespace: ns.Name}, harnessAfter)
	if err == nil {
		t.Errorf("Expected Harness to be deleted")
	}
	t.Logf("Harness deletion verified")
}

// TestAgentRuntimeCRD_Integration tests AgentRuntime CRD lifecycle.
func TestAgentRuntimeCRD_Integration(t *testing.T) {
	if k8sClient == nil {
		t.Skip("Integration tests skipped - envtest not configured")
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "runtime-crd-test"},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = k8sClient.Delete(ctx, ns) }()

	// Test Case 1: Create AgentRuntime
	t.Log("Test Case 1: Create AgentRuntime")

	runtime := &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-runtime",
			Namespace: ns.Name,
		},
		Spec: v1.AgentRuntimeSpec{
			AgentHandler: v1.AgentHandlerSpec{
				Image: "aiagent/handler:v1",
				Env: []v1.EnvVar{
					{Name: "LOG_LEVEL", Value: "debug"},
				},
			},
			AgentFramework: v1.AgentFrameworkSpec{
				Image: "aiagent/framework:v1",
				Type:  "adk-go",
			},
			Replicas: 1,
		},
	}

	if err := k8sClient.Create(ctx, runtime); err != nil {
		t.Fatalf("Failed to create AgentRuntime: %v", err)
	}
	t.Logf("Created AgentRuntime: %s", runtime.Name)

	// Test Case 2: Update status to Running
	t.Log("Test Case 2: Update status to Running")

	runtimeCheck := &v1.AgentRuntime{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: runtime.Name, Namespace: ns.Name}, runtimeCheck); err != nil {
		t.Fatalf("Failed to get AgentRuntime: %v", err)
	}

	runtimeCheck.Status.Phase = v1.RuntimePhaseRunning
	runtimeCheck.Status.ReadyReplicas = 1
	runtimeCheck.Status.AgentCount = 0
	runtimeCheck.Status.PodIPs = []string{"10.0.0.1"}

	if err := k8sClient.Status().Update(ctx, runtimeCheck); err != nil {
		t.Fatalf("Failed to update AgentRuntime status: %v", err)
	}
	t.Logf("Updated AgentRuntime status to Running")

	// Test Case 3: Add agent binding
	t.Log("Test Case 3: Add agent binding")

	runtimeCheck.Status.AgentCount = 1
	runtimeCheck.Status.Agents = []v1.AgentBindingStatus{
		{Name: "test-agent"},
	}

	if err := k8sClient.Status().Update(ctx, runtimeCheck); err != nil {
		t.Fatalf("Failed to add agent binding: %v", err)
	}
	t.Logf("Added agent binding to AgentRuntime")

	// Test Case 4: Delete AgentRuntime
	t.Log("Test Case 4: Delete AgentRuntime")

	if err := k8sClient.Delete(ctx, runtime); err != nil {
		t.Fatalf("Failed to delete AgentRuntime: %v", err)
	}
	t.Logf("Deleted AgentRuntime")
}

// TestAIAgentCRD_Integration tests AIAgent CRD lifecycle.
func TestAIAgentCRD_Integration(t *testing.T) {
	if k8sClient == nil {
		t.Skip("Integration tests skipped - envtest not configured")
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "agent-crd-test"},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = k8sClient.Delete(ctx, ns) }()

	// Create prerequisite AgentRuntime
	runtime := &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "prereq-runtime", Namespace: ns.Name},
		Spec: v1.AgentRuntimeSpec{
			AgentHandler:   v1.AgentHandlerSpec{Image: "handler:v1"},
			AgentFramework: v1.AgentFrameworkSpec{Image: "framework:v1", Type: "adk-go"},
			Replicas:       1,
		},
	}
	if err := k8sClient.Create(ctx, runtime); err != nil {
		t.Fatalf("Failed to create prerequisite AgentRuntime: %v", err)
	}

	// Set runtime to running
	runtime.Status.Phase = v1.RuntimePhaseRunning
	runtime.Status.ReadyReplicas = 1
	_ = k8sClient.Status().Update(ctx, runtime)

	// Test Case 1: Create AIAgent
	t.Log("Test Case 1: Create AIAgent")

	agent := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ai-agent",
			Namespace: ns.Name,
		},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{
				Name: runtime.Name,
				Type: "adk-go",
			},
			Description:  "Integration test agent",
			VolumePolicy: v1.VolumePolicyRetain,
		},
	}

	if err := k8sClient.Create(ctx, agent); err != nil {
		t.Fatalf("Failed to create AIAgent: %v", err)
	}
	t.Logf("Created AIAgent: %s", agent.Name)

	// Test Case 2: Update AIAgent status (scheduling phase)
	t.Log("Test Case 2: Update AIAgent status - Scheduling phase")

	agentCheck := &v1.AIAgent{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: ns.Name}, agentCheck); err != nil {
		t.Fatalf("Failed to get AIAgent: %v", err)
	}

	agentCheck.Status.Phase = v1.AgentPhaseScheduling
	if err := k8sClient.Status().Update(ctx, agentCheck); err != nil {
		t.Fatalf("Failed to update AIAgent status: %v", err)
	}
	t.Logf("Updated AIAgent status to Scheduling")

	// Test Case 3: Update to Running with runtime binding
	t.Log("Test Case 3: Update to Running with runtime binding")

	agentCheck.Status.Phase = v1.AgentPhaseRunning
	agentCheck.Status.RuntimeRef = v1.RuntimeReferenceStatus{
		Name: runtime.Name,
		UID:  string(runtime.UID),
	}
	agentCheck.Status.AgentID = "agent-fw-123"

	if err := k8sClient.Status().Update(ctx, agentCheck); err != nil {
		t.Fatalf("Failed to update AIAgent to Running: %v", err)
	}
	t.Logf("Updated AIAgent to Running, bound to runtime: %s", agentCheck.Status.RuntimeRef.Name)

	// Test Case 4: Verify agent-runtime binding
	t.Log("Test Case 4: Verify agent-runtime binding")

	agentVerify := &v1.AIAgent{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: ns.Name}, agentVerify); err != nil {
		t.Fatalf("Failed to get AIAgent: %v", err)
	}

	if agentVerify.Status.Phase != v1.AgentPhaseRunning {
		t.Errorf("Expected Running phase, got %s", agentVerify.Status.Phase)
	}
	if agentVerify.Status.RuntimeRef.Name != runtime.Name {
		t.Errorf("Expected RuntimeRef.Name %s, got %s", runtime.Name, agentVerify.Status.RuntimeRef.Name)
	}
	if agentVerify.Status.AgentID != "agent-fw-123" {
		t.Errorf("Expected AgentID agent-fw-123, got %s", agentVerify.Status.AgentID)
	}

	// Test Case 5: Delete AIAgent
	t.Log("Test Case 5: Delete AIAgent")

	if err := k8sClient.Delete(ctx, agent); err != nil {
		t.Fatalf("Failed to delete AIAgent: %v", err)
	}
	t.Logf("Deleted AIAgent")
}

// TestFullLifecycle_Integration tests the complete flow from Harness to AIAgent.
func TestFullLifecycle_Integration(t *testing.T) {
	if k8sClient == nil {
		t.Skip("Integration tests skipped - envtest not configured")
	}

	nsName := fmt.Sprintf("full-lifecycle-%d", time.Now().UnixNano())
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = k8sClient.Delete(ctx, ns) }()

	// Phase 1: Create Harnesses
	t.Log("Phase 1: Creating Harnesses")

	harnesses := []*v1.Harness{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "model-harness", Namespace: nsName},
			Spec: v1.HarnessSpec{
				Type: v1.HarnessTypeModel,
				Model: &v1.ModelHarnessSpec{
					Provider:     "deepseek",
					DefaultModel: "deepseek-chat",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "memory-harness", Namespace: nsName},
			Spec: v1.HarnessSpec{
				Type: v1.HarnessTypeMemory,
				Memory: &v1.MemoryHarnessSpec{
					Type: "inmemory",
					TTL:  3600,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "sandbox-harness", Namespace: nsName},
			Spec: v1.HarnessSpec{
				Type: v1.HarnessTypeSandbox,
				Sandbox: &v1.SandboxHarnessSpec{
					Type: "gvisor",
					Mode: v1.SandboxModeEmbedded,
				},
			},
		},
	}

	for _, h := range harnesses {
		if err := k8sClient.Create(ctx, h); err != nil {
			t.Fatalf("Failed to create Harness %s: %v", h.Name, err)
		}
		// Set status
		h.Status.Phase = v1.HarnessPhaseAvailable
		_ = k8sClient.Status().Update(ctx, h)
		t.Logf("Created Harness: %s (type: %s)", h.Name, h.Spec.Type)
	}

	// Phase 2: Create AgentRuntime referencing Harnesses
	t.Log("Phase 2: Creating AgentRuntime")

	runtime := &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{Name: "main-runtime", Namespace: nsName},
		Spec: v1.AgentRuntimeSpec{
			AgentHandler: v1.AgentHandlerSpec{
				Image: "aiagent/handler:latest",
				Env: []v1.EnvVar{
					{Name: "HANDLER_MODE", Value: "production"},
				},
			},
			AgentFramework: v1.AgentFrameworkSpec{
				Image: "aiagent/adk-framework:latest",
				Type:  "adk-go",
			},
			Harness: []v1.HarnessReference{
				{Name: "model-harness"},
				{Name: "memory-harness"},
				{Name: "sandbox-harness"},
			},
			Replicas: 1,
		},
	}

	if err := k8sClient.Create(ctx, runtime); err != nil {
		t.Fatalf("Failed to create AgentRuntime: %v", err)
	}

	// Set runtime status to Running
	runtime.Status.Phase = v1.RuntimePhaseRunning
	runtime.Status.ReadyReplicas = 1
	runtime.Status.PodIPs = []string{"10.0.0.1"}
	_ = k8sClient.Status().Update(ctx, runtime)
	t.Logf("Created AgentRuntime: %s with %d Harness references", runtime.Name, len(runtime.Spec.Harness))

	// Phase 3: Create AIAgent
	t.Log("Phase 3: Creating AIAgent")

	agent := &v1.AIAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "production-agent", Namespace: nsName},
		Spec: v1.AIAgentSpec{
			RuntimeRef: v1.RuntimeReference{
				Name: runtime.Name,
				Type: "adk-go",
			},
			HarnessOverride: v1.HarnessOverrideSpec{
				Model: []v1.ModelOverrideSpec{
					{Name: "model-harness", AllowedModels: []string{"deepseek-reasoner"}},
				},
			},
			Description:  "Production AI Agent",
			VolumePolicy: v1.VolumePolicyRetain,
		},
	}

	if err := k8sClient.Create(ctx, agent); err != nil {
		t.Fatalf("Failed to create AIAgent: %v", err)
	}
	t.Logf("Created AIAgent: %s", agent.Name)

	// Phase 4: Schedule AIAgent to AgentRuntime
	t.Log("Phase 4: Scheduling AIAgent")

	agent.Status.Phase = v1.AgentPhaseRunning
	agent.Status.RuntimeRef = v1.RuntimeReferenceStatus{
		Name: runtime.Name,
	}
	agent.Status.AgentID = "agent-production-001"
	_ = k8sClient.Status().Update(ctx, agent)

	// Update runtime agent count and binding
	runtimeCheck := &v1.AgentRuntime{}
	_ = k8sClient.Get(ctx, types.NamespacedName{Name: runtime.Name, Namespace: nsName}, runtimeCheck)
	runtimeCheck.Status.AgentCount = 1
	runtimeCheck.Status.Agents = []v1.AgentBindingStatus{
		{Name: agent.Name},
	}
	_ = k8sClient.Status().Update(ctx, runtimeCheck)

	// Phase 5: Verify complete setup
	t.Log("Phase 5: Verifying complete setup")

	// List all resources
	allHarnesses := &v1.HarnessList{}
	if err := k8sClient.List(ctx, allHarnesses, client.InNamespace(nsName)); err != nil {
		t.Fatalf("Failed to list Harnesses: %v", err)
	}
	if len(allHarnesses.Items) != 3 {
		t.Errorf("Expected 3 Harnesses, got %d", len(allHarnesses.Items))
	}

	allRuntimes := &v1.AgentRuntimeList{}
	if err := k8sClient.List(ctx, allRuntimes, client.InNamespace(nsName)); err != nil {
		t.Fatalf("Failed to list AgentRuntimes: %v", err)
	}
	if len(allRuntimes.Items) != 1 {
		t.Errorf("Expected 1 AgentRuntime, got %d", len(allRuntimes.Items))
	}

	allAgents := &v1.AIAgentList{}
	if err := k8sClient.List(ctx, allAgents, client.InNamespace(nsName)); err != nil {
		t.Fatalf("Failed to list AIAgents: %v", err)
	}
	if len(allAgents.Items) != 1 {
		t.Errorf("Expected 1 AIAgent, got %d", len(allAgents.Items))
	}

	t.Log("Phase 6: Cleanup in reverse order")

	// Delete AIAgent
	_ = k8sClient.Delete(ctx, agent)
	t.Logf("Deleted AIAgent")

	// Delete AgentRuntime
	_ = k8sClient.Delete(ctx, runtime)
	t.Logf("Deleted AgentRuntime")

	// Delete Harnesses
	for _, h := range harnesses {
		_ = k8sClient.Delete(ctx, h)
	}
	t.Logf("Deleted all Harnesses")

	t.Log("Full lifecycle integration test completed successfully")
}