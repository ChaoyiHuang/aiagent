package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aiagent/api/v1"
)

// TestExternalSandboxClient_ExecuteTool tests tool execution through HTTP.
func TestExternalSandboxClient_ExecuteTool(t *testing.T) {
	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check request
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}

		// Parse request body
		var req ToolExecutionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Return mock response
		resp := ToolExecutionResponse{
			Output:     map[string]any{"result": "success", "tool": req.ToolName},
			Status:     "completed",
			Duration:   150,
			ExitCode:   0,
			SandboxID:  "sandbox-test-001",
			WorkspaceID: "workspace-test",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client
	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	// Execute tool
	req := &ToolExecutionRequest{
		ToolName: "read",
		Params:   map[string]any{"path": "/workspace/test.txt"},
		Context: &ToolExecutionContext{
			AgentID:    "agent-001",
			SessionKey: "session-001",
		},
	}

	resp, err := client.ExecuteTool(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}

	if resp.SandboxID == "" {
		t.Error("expected sandbox ID to be set")
	}
}

// TestExternalSandboxClient_ExecuteSkill tests skill execution through HTTP.
func TestExternalSandboxClient_ExecuteSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		resp := SkillExecutionResponse{
			Output:     map[string]any{"temp": 25, "location": req["location"]},
			Status:     "completed",
			Duration:   500,
			SandboxID:  "sandbox-skill-001",
			WorkspaceID: "workspace-test",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	req := &SkillExecutionRequest{
		SkillName: "weather",
		Params:    map[string]any{"location": "London"},
		Context: &ToolExecutionContext{
			AgentID:    "agent-001",
			SessionKey: "session-001",
		},
	}

	resp, err := client.ExecuteSkill(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}

	if resp.SandboxID == "" {
		t.Error("expected sandbox ID to be set")
	}
}

// TestExternalSandboxClient_CreateWorkspace tests workspace creation.
func TestExternalSandboxClient_CreateWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		resp := WorkspaceCreateResponse{
			WorkspaceID: "workspace-session-001",
			Path:        "/workspace/session-001",
			Status:      "created",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	resp, err := client.CreateWorkspace(ctx, &WorkspaceCreateRequest{
		SessionKey: "session-001",
		Scope:      "session",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.WorkspaceID == "" {
		t.Error("expected workspace ID to be set")
	}

	if resp.Status != "created" {
		t.Errorf("expected status 'created', got '%s'", resp.Status)
	}
}

// TestExternalSandboxClient_CleanupWorkspace tests workspace cleanup.
func TestExternalSandboxClient_CleanupWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}

		resp := WorkspaceCleanupResponse{
			Success: true,
			Status:  "cleanup_completed",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	resp, err := client.CleanupWorkspace(ctx, &WorkspaceCleanupRequest{
		WorkspaceID: "workspace-session-001",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Error("expected cleanup to succeed")
	}
}

// TestExternalSandboxClient_GetWorkspaceStatus tests workspace status.
func TestExternalSandboxClient_GetWorkspaceStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}

		resp := WorkspaceStatusResponse{
			WorkspaceID:  "workspace-session-001",
			Size:         10240,
			Files:        5,
			LastAccessed: time.Now(),
			Status:       "active",
			CreatedAt:    time.Now().Add(-1 * time.Hour),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	resp, err := client.GetWorkspaceStatus(ctx, "workspace-session-001")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", resp.Status)
	}

	if resp.Files != 5 {
		t.Errorf("expected 5 files, got %d", resp.Files)
	}
}

// TestExternalSandboxClient_Health tests health check.
func TestExternalSandboxClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}

		resp := struct {
			Healthy      bool `json:"healthy"`
			Available    bool `json:"available"`
			ResourcePool struct {
				Total     int `json:"total"`
				Available int `json:"available"`
				InUse     int `json:"inUse"`
			} `json:"resourcePool"`
		}{
			Healthy:   true,
			Available: true,
			ResourcePool: struct {
				Total     int `json:"total"`
				Available int `json:"available"`
				InUse     int `json:"inUse"`
			}{
				Total:     10,
				Available: 8,
				InUse:     2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	health, err := client.Health(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !health.Healthy {
		t.Error("expected sandbox to be healthy")
	}

	if !health.Available {
		t.Error("expected sandbox to be available")
	}

	if health.ResourceCount != 8 {
		t.Errorf("expected 8 available resources, got %d", health.ResourceCount)
	}
}

// TestExternalSandboxClient_ErrorHandling tests error handling.
func TestExternalSandboxClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return error
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client := NewExternalSandboxClient(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	_, err := client.ExecuteTool(ctx, &ToolExecutionRequest{
		ToolName: "test",
		Context:  &ToolExecutionContext{}, // Add empty context to avoid nil
	})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

// TestExternalSandboxClientWrapper_Execute tests the wrapper Execute method.
func TestExternalSandboxClientWrapper_Execute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ToolExecutionResponse{
			Output:     map[string]any{"result": "success"},
			Status:     "completed",
			Duration:   100,
			ExitCode:   0,
			SandboxID:  "sandbox-wrapper-test",
			WorkspaceID: "workspace-test",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	wrapper := NewExternalSandboxClientWrapper(&ExternalSandboxConfig{
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	})

	ctx := context.Background()

	req := &ExecuteRequest{
		ToolName: "write",
		Params:   map[string]any{"path": "/workspace/test.txt", "content": "hello"},
		AgentID:  "agent-001",
		SessionID: "session-001",
	}

	resp, err := wrapper.Execute(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}

	if resp.ResourceID == "" {
		t.Error("expected resource ID to be set")
	}
}

// TestSandboxHarness_ExternalModeWithMockServer tests external mode with mock server.
func TestSandboxHarness_ExternalModeWithMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ToolExecutionResponse{
			Output:     map[string]any{"tool": "remote-tool", "result": "executed remotely"},
			Status:     "completed",
			Duration:   200,
			ExitCode:   0,
			SandboxID:  "sandbox-001",
			WorkspaceID: "workspace-default",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create harness with custom client using mock server
	spec := &v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal,
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	}

	h := NewSandboxHarness(spec)

	// Verify it's external mode
	if !h.IsExternal() {
		t.Error("expected external mode")
	}

	ctx := context.Background()

	req := &ExecuteRequest{
		ToolName: "remote-tool",
		ToolType: "skill",
		Params:   map[string]any{"query": "test"},
	}

	resp, err := h.Execute(ctx, req)
	if err != nil {
		t.Errorf("unexpected error executing: %v", err)
	}

	// External mode should indicate remote execution
	if resp.ResourceID == "" {
		t.Error("expected resource ID for remote execution")
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
}

// TestSandboxHarness_ExecuteTool tests direct tool execution method.
func TestSandboxHarness_ExecuteTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ToolExecutionResponse{
			Output:     "file content",
			Status:     "completed",
			Duration:   50,
			ExitCode:   0,
			SandboxID:  "sandbox-direct-tool",
			WorkspaceID: "workspace-session-001",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	spec := &v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal,
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	}

	h := NewSandboxHarness(spec)
	ctx := context.Background()

	resp, err := h.ExecuteTool(ctx, "read", map[string]any{"path": "/workspace/test.txt"}, &ToolExecutionContext{
		SessionKey: "session-001",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
}

// TestSandboxHarness_ExecuteSkill tests direct skill execution method.
func TestSandboxHarness_ExecuteSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SkillExecutionResponse{
			Output:     map[string]any{"temp": 20},
			Status:     "completed",
			Duration:   100,
			SandboxID:  "sandbox-direct-skill",
			WorkspaceID: "workspace-session-001",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	spec := &v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal,
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	}

	h := NewSandboxHarness(spec)
	ctx := context.Background()

	resp, err := h.ExecuteSkill(ctx, "weather", map[string]any{"location": "Paris"}, &ToolExecutionContext{
		SessionKey: "session-001",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}
}

// TestResolveWorkspaceID tests workspace ID resolution.
func TestResolveWorkspaceID(t *testing.T) {
	tests := []struct {
		sessionKey     string
		defaultWorkspace string
		expected       string
	}{
		{"", "workspace-default", "workspace-default"},
		{"default", "workspace-default", "workspace-default"},
		{"telegram:123:456:session-001", "workspace-default", "workspace-session-001"},
		{"session-002", "workspace-default", "workspace-session-002"},
		{"complex:abc:def:ghi:session-003", "workspace-default", "workspace-session-003"},
	}

	for _, test := range tests {
		result := resolveWorkspaceID(test.sessionKey, test.defaultWorkspace)
		if result != test.expected {
			t.Errorf("resolveWorkspaceID(%s, %s) = %s, expected %s",
				test.sessionKey, test.defaultWorkspace, result, test.expected)
		}
	}
}

// TestConvertToolResponse tests response conversion.
func TestConvertToolResponse(t *testing.T) {
	toolResp := &ToolExecutionResponse{
		Output:     map[string]any{"key": "value"},
		Status:     "completed",
		Duration:   150,
		ExitCode:   0,
		SandboxID:  "sandbox-convert-test",
	}

	resp := ConvertToolResponse(toolResp)

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}

	if resp.Duration != 150*time.Millisecond {
		t.Errorf("expected duration 150ms, got %v", resp.Duration)
	}

	if resp.ResourceID != "sandbox-convert-test" {
		t.Errorf("expected resource ID 'sandbox-convert-test', got '%s'", resp.ResourceID)
	}
}

// TestConvertSkillResponse tests skill response conversion.
func TestConvertSkillResponse(t *testing.T) {
	skillResp := &SkillExecutionResponse{
		Output:     map[string]any{"result": "skill output"},
		Status:     "completed",
		Duration:   200,
		SandboxID:  "sandbox-skill-convert",
	}

	resp := ConvertSkillResponse(skillResp)

	if resp.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", resp.Status)
	}

	if resp.Duration != 200*time.Millisecond {
		t.Errorf("expected duration 200ms, got %v", resp.Duration)
	}

	if resp.ResourceID != "sandbox-skill-convert" {
		t.Errorf("expected resource ID 'sandbox-skill-convert', got '%s'", resp.ResourceID)
	}
}