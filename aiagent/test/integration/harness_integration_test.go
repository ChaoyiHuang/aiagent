// Package integration provides integration tests for Harness components.
// These tests verify the complete workflow of harness components with mock HTTP servers.
package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aiagent/api/v1"
	"aiagent/pkg/harness"
)

// TestHarnessManager_Integration tests the complete Harness Manager workflow.
func TestHarnessManager_Integration(t *testing.T) {
	ctx := context.Background()
	m := harness.NewHarnessManager()

	// Test Case 1: Initialize multiple harness types
	t.Log("Test Case 1: Initialize multiple harness types")

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				Endpoint:     "https://api.deepseek.com",
				DefaultModel: "deepseek-chat",
				Models: []v1.ModelConfig{
					{Name: "deepseek-chat", Allowed: true},
				},
			},
		},
		{
			Type: v1.HarnessTypeMemory,
			Memory: &v1.MemoryHarnessSpec{
				Type:              "inmemory",
				TTL:               3600,
			},
		},
		{
			Type: v1.HarnessTypeSandbox,
			Sandbox: &v1.SandboxHarnessSpec{
				Type:     "gvisor",
				Mode:     v1.SandboxModeEmbedded,
				Timeout:  300,
			},
		},
		{
			Type: v1.HarnessTypeSkills,
			Skills: &v1.SkillsHarnessSpec{
				HubType:    "local",
				Skills: []v1.SkillConfig{
					{Name: "web-search", Version: "1.0.0", Allowed: true},
				},
			},
		},
	}

	if err := m.Initialize(ctx, specs); err != nil {
		t.Fatalf("Failed to initialize Harness Manager: %v", err)
	}

	// Verify harnesses
	t.Log("Verifying harness availability")
	if m.GetModelHarness() == nil {
		t.Error("Model Harness should be initialized")
	}
	if m.GetMemoryHarness() == nil {
		t.Error("Memory Harness should be initialized")
	}
	if m.GetSandboxHarness() == nil {
		t.Error("Sandbox Harness should be initialized")
	}
	if m.GetSkillsHarness() == nil {
		t.Error("Skills Harness should be initialized")
	}

	// Test Model Harness
	modelHarness := m.GetModelHarness()
	if !modelHarness.IsModelAllowed("deepseek-chat") {
		t.Error("deepseek-chat should be allowed")
	}

	// Test Memory Harness
	memoryHarness := m.GetMemoryHarness()
	testData := []byte(`{"test": "data"}`)
	if err := memoryHarness.Store(ctx, "test-key", testData); err != nil {
		t.Errorf("Memory store failed: %v", err)
	}
	retrieved, err := memoryHarness.Retrieve(ctx, "test-key")
	if err != nil || string(retrieved) != string(testData) {
		t.Errorf("Memory retrieve failed or mismatch")
	}

	// Test Skills Harness
	skillsHarness := m.GetSkillsHarness()
	skills := skillsHarness.ListSkills()
	if len(skills) == 0 {
		t.Error("Should have at least one skill")
	}

	t.Log("Harness Manager integration test completed")
}

// TestExternalSandboxClient_Integration tests External Sandbox HTTP client with mock server.
func TestExternalSandboxClient_Integration(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/tools/test-tool":
			w.Write([]byte(`{"output": "result", "status": "completed", "duration": 150, "exitCode": 0}`))
		case "/skills/test-skill":
			w.Write([]byte(`{"output": {"data": "value"}, "status": "completed", "duration": 200}`))
		case "/workspace/create":
			w.Write([]byte(`{"workspaceId": "ws-123", "path": "/workspace/ws-123"}`))
		case "/health":
			w.Write([]byte(`{"healthy": true, "available": true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &harness.ExternalSandboxConfig{
		Endpoint: mockServer.URL,
		APIKey:   "test-key",
		Timeout:  30 * time.Second,
	}
	client := harness.NewExternalSandboxClient(cfg)
	ctx := context.Background()

	// Health check
	health, err := client.Health(ctx)
	if err != nil || !health.Healthy {
		t.Errorf("Health check failed")
	}

	// Create workspace
	wsResp, err := client.CreateWorkspace(ctx, &harness.WorkspaceCreateRequest{
		SessionKey: "test-session",
	})
	if err != nil || wsResp.WorkspaceID != "ws-123" {
		t.Errorf("Create workspace failed")
	}

	// Execute tool
	toolResp, err := client.ExecuteTool(ctx, &harness.ToolExecutionRequest{
		ToolName: "test-tool",
		Params:   map[string]interface{}{"input": "test"},
		Context:  &harness.ToolExecutionContext{SessionKey: "test-session"},
	})
	if err != nil || toolResp.Status != "completed" {
		t.Errorf("Execute tool failed")
	}

	// Execute skill
	skillResp, err := client.ExecuteSkill(ctx, &harness.SkillExecutionRequest{
		SkillName: "test-skill",
		Params:    map[string]interface{}{"input": "test"},
		Context:   &harness.ToolExecutionContext{SessionKey: "test-session"},
	})
	if err != nil || skillResp.Status != "completed" {
		t.Errorf("Execute skill failed")
	}

	t.Log("External Sandbox Client integration test completed")
}

// TestSkillsSandboxRouting_Integration tests Skills execution routing between embedded and external sandbox.
func TestSkillsSandboxRouting_Integration(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"output": "remote result", "status": "completed", "duration": 300}`))
	}))
	defer mockServer.Close()

	ctx := context.Background()

	// Embedded sandbox - local execution
	embeddedSandbox := harness.NewSandboxHarness(&v1.SandboxHarnessSpec{
		Type:    "gvisor",
		Mode:    v1.SandboxModeEmbedded,
	})
	skillsEmbedded := harness.NewSkillsHarness(&v1.SkillsHarnessSpec{
		Skills: []v1.SkillConfig{{Name: "local-skill", Allowed: true}},
	}, embeddedSandbox)

	result, err := skillsEmbedded.ExecuteSkill(ctx, "local-skill", map[string]interface{}{})
	if err != nil {
		t.Errorf("Embedded execution failed: %v", err)
	}
	if result.Remote {
		t.Error("Embedded should return Remote=false")
	}

	// External sandbox - remote execution
	externalSandbox := harness.NewSandboxHarness(&v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal,
		Endpoint: mockServer.URL,
	})
	skillsExternal := harness.NewSkillsHarness(&v1.SkillsHarnessSpec{
		Skills: []v1.SkillConfig{{Name: "remote-skill", Allowed: true}},
	}, externalSandbox)

	if !skillsExternal.IsRemoteExecution() {
		t.Error("External should indicate remote execution")
	}

	t.Log("Skills-Sandbox routing test completed")
}