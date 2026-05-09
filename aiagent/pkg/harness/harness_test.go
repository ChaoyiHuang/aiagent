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

func TestNewHarnessManager(t *testing.T) {
	m := NewHarnessManager()
	if m == nil {
		t.Error("expected harness manager to be created")
	}
}

func TestHarnessManager_Initialize(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
				Models: []v1.ModelConfig{
					{Name: "deepseek-chat", Allowed: true},
				},
			},
		},
		{
			Type: v1.HarnessTypeMemory,
			Memory: &v1.MemoryHarnessSpec{
				Type: "inmemory",
			},
		},
	}

	err := m.Initialize(ctx, specs)
	if err != nil {
		t.Errorf("unexpected error initializing harnesses: %v", err)
	}

	if m.GetModelHarness() == nil {
		t.Error("expected model harness to be initialized")
	}

	if m.GetMemoryHarness() == nil {
		t.Error("expected memory harness to be initialized")
	}
}

func TestHarnessManager_GetHarnessStatus(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
			},
		},
	}
	m.Initialize(ctx, specs)

	status := m.GetHarnessStatus(HarnessTypeModel)
	if status == nil {
		t.Error("expected harness status to be returned")
	}

	if !status.Available {
		t.Error("expected model harness to be available")
	}
}

func TestHarnessManager_IsHarnessAvailable(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
			},
		},
	}
	m.Initialize(ctx, specs)

	if !m.IsHarnessAvailable(HarnessTypeModel) {
		t.Error("expected model harness to be available")
	}

	if m.IsHarnessAvailable(HarnessTypeMCP) {
		t.Error("expected MCP harness to not be available (not initialized)")
	}
}

func TestModelHarness(t *testing.T) {
	spec := &v1.ModelHarnessSpec{
		Provider:     "deepseek",
		DefaultModel: "deepseek-chat",
		Models: []v1.ModelConfig{
			{Name: "deepseek-chat", Allowed: true},
			{Name: "deepseek-reasoner", Allowed: true},
		},
	}

	h := NewModelHarness(spec)
	if h == nil {
		t.Error("expected model harness to be created")
	}

	defaultModel := h.GetDefaultModel()
	if defaultModel != "deepseek-chat" {
		t.Errorf("expected default model 'deepseek-chat', got '%s'", defaultModel)
	}

	allowedModels := h.GetAllowedModels()
	if len(allowedModels) != 2 {
		t.Errorf("expected 2 allowed models, got %d", len(allowedModels))
	}

	if !h.IsModelAllowed("deepseek-chat") {
		t.Error("expected 'deepseek-chat' to be allowed")
	}

	if h.IsModelAllowed("unknown-model") {
		t.Error("expected 'unknown-model' to not be allowed")
	}
}

func TestModelHarness_Generate(t *testing.T) {
	spec := &v1.ModelHarnessSpec{
		Provider:     "deepseek",
		DefaultModel: "deepseek-chat",
		Models: []v1.ModelConfig{
			{Name: "deepseek-chat", Allowed: true},
		},
	}

	h := NewModelHarness(spec)
	ctx := context.Background()

	req := &GenerateRequest{
		Prompt: "Hello, world!",
	}

	resp, err := h.Generate(ctx, req)
	if err != nil {
		t.Errorf("unexpected error generating: %v", err)
	}

	if resp == nil {
		t.Error("expected response to be returned")
	}

	if resp.Text == "" {
		t.Error("expected response text to be non-empty")
	}
}

func TestModelHarness_CountTokens(t *testing.T) {
	spec := &v1.ModelHarnessSpec{
		Provider:     "deepseek",
		DefaultModel: "deepseek-chat",
	}
	h := NewModelHarness(spec)
	ctx := context.Background()

	count, err := h.CountTokens(ctx, "Hello, world!")
	if err != nil {
		t.Errorf("unexpected error counting tokens: %v", err)
	}

	if count < 0 {
		t.Error("expected token count to be positive")
	}
}

func TestMCPHarness(t *testing.T) {
	spec := &v1.MCPHarnessSpec{
		RegistryType: "builtin",
		Servers: []v1.MCPServerConfig{
			{Name: "server1", Type: "stdio", Allowed: true},
			{Name: "server2", Type: "http", Allowed: false},
		},
	}

	h := NewMCPHarness(spec)
	if h == nil {
		t.Error("expected MCP harness to be created")
	}

	servers := h.ListServers()
	if len(servers) != 1 {
		t.Errorf("expected 1 allowed server, got %d", len(servers))
	}

	if !h.IsServerAllowed("server1") {
		t.Error("expected server1 to be allowed")
	}

	if h.IsServerAllowed("server2") {
		t.Error("expected server2 to not be allowed")
	}
}

func TestMCPHarness_ListAllTools(t *testing.T) {
	spec := &v1.MCPHarnessSpec{
		RegistryType: "builtin",
		Servers: []v1.MCPServerConfig{
			{Name: "server1", Type: "stdio", Allowed: true},
		},
	}

	h := NewMCPHarness(spec)
	ctx := context.Background()

	tools, err := h.ListAllTools(ctx)
	if err != nil {
		t.Errorf("unexpected error listing tools: %v", err)
	}

	if len(tools) == 0 {
		t.Error("expected tools to be returned")
	}
}

func TestMCPHarness_CallTool(t *testing.T) {
	spec := &v1.MCPHarnessSpec{
		RegistryType: "builtin",
		Servers: []v1.MCPServerConfig{
			{Name: "server1", Type: "stdio", Allowed: true},
		},
	}

	h := NewMCPHarness(spec)
	ctx := context.Background()

	result, err := h.CallTool(ctx, "server1", "server1_tool1", map[string]any{"input": "test"})
	if err != nil {
		t.Errorf("unexpected error calling tool: %v", err)
	}

	if result == nil {
		t.Error("expected result to be returned")
	}
}

func TestMemoryHarness(t *testing.T) {
	spec := &v1.MemoryHarnessSpec{
		Type: "inmemory",
		TTL:  3600,
	}

	h := NewMemoryHarness(spec)
	if h == nil {
		t.Error("expected memory harness to be created")
	}

	ctx := context.Background()

	// Test store
	err := h.Store(ctx, "test-key", []byte("test-value"))
	if err != nil {
		t.Errorf("unexpected error storing: %v", err)
	}

	// Test retrieve
	value, err := h.Retrieve(ctx, "test-key")
	if err != nil {
		t.Errorf("unexpected error retrieving: %v", err)
	}

	if string(value) != "test-value" {
		t.Errorf("expected value 'test-value', got '%s'", string(value))
	}

	// Test exists
	exists, err := h.Exists(ctx, "test-key")
	if err != nil {
		t.Errorf("unexpected error checking exists: %v", err)
	}
	if !exists {
		t.Error("expected key to exist")
	}

	// Test delete
	err = h.Delete(ctx, "test-key")
	if err != nil {
		t.Errorf("unexpected error deleting: %v", err)
	}

	// Verify deleted
	exists, _ = h.Exists(ctx, "test-key")
	if exists {
		t.Error("expected key to not exist after delete")
	}
}

func TestMemoryHarness_List(t *testing.T) {
	spec := &v1.MemoryHarnessSpec{
		Type: "inmemory",
	}

	h := NewMemoryHarness(spec)
	ctx := context.Background()

	// Store multiple keys
	h.Store(ctx, "prefix:key1", []byte("value1"))
	h.Store(ctx, "prefix:key2", []byte("value2"))
	h.Store(ctx, "other:key3", []byte("value3"))

	// List with prefix
	keys, err := h.List(ctx, "prefix:")
	if err != nil {
		t.Errorf("unexpected error listing: %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("expected 2 keys with prefix, got %d", len(keys))
	}
}

func TestMemoryHarness_Clear(t *testing.T) {
	spec := &v1.MemoryHarnessSpec{
		Type: "inmemory",
	}

	h := NewMemoryHarness(spec)
	ctx := context.Background()

	h.Store(ctx, "key1", []byte("value1"))
	h.Store(ctx, "key2", []byte("value2"))

	err := h.Clear(ctx)
	if err != nil {
		t.Errorf("unexpected error clearing: %v", err)
	}

	keys, _ := h.List(ctx, "")
	if len(keys) != 0 {
		t.Error("expected all keys to be cleared")
	}
}

func TestSandboxHarness_EmbeddedMode(t *testing.T) {
	spec := &v1.SandboxHarnessSpec{
		Type: "docker",
		Mode: v1.SandboxModeEmbedded,
		Timeout: 30,
	}

	h := NewSandboxHarness(spec)
	if h == nil {
		t.Error("expected sandbox harness to be created")
	}

	if !h.IsEmbedded() {
		t.Error("expected sandbox to be in embedded mode")
	}

	if h.IsExternal() {
		t.Error("expected sandbox to not be in external mode")
	}

	if h.GetMode() != SandboxModeEmbedded {
		t.Errorf("expected mode 'embedded', got '%s'", h.GetMode())
	}
}

func TestSandboxHarness_ExternalMode(t *testing.T) {
	spec := &v1.SandboxHarnessSpec{
		Type: "gvisor",
		Mode: v1.SandboxModeExternal,
		Timeout: 60,
		ResourceLimits: &v1.SandboxResourceLimits{
			CPU:    "1",
			Memory: "512Mi",
		},
	}

	h := NewSandboxHarness(spec)
	if h == nil {
		t.Error("expected sandbox harness to be created")
	}

	if !h.IsExternal() {
		t.Error("expected sandbox to be in external mode")
	}

	if h.IsEmbedded() {
		t.Error("expected sandbox to not be in embedded mode")
	}
}

func TestSandboxHarness_Execute(t *testing.T) {
	spec := &v1.SandboxHarnessSpec{
		Type: "docker",
		Mode: v1.SandboxModeEmbedded,
	}

	h := NewSandboxHarness(spec)
	ctx := context.Background()

	req := &ExecuteRequest{
		ToolName: "test-tool",
		ToolType: "skill",
		Params:   map[string]any{"input": "test"},
	}

	resp, err := h.Execute(ctx, req)
	if err != nil {
		t.Errorf("unexpected error executing: %v", err)
	}

	if resp == nil {
		t.Error("expected response to be returned")
	}

	// Embedded mode should execute locally
	output := string(resp.Output)
	if output == "" {
		t.Error("expected output to be non-empty")
	}
}

func TestSandboxHarness_Execute_RemoteMode(t *testing.T) {
	// Use mock server instead of real endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"output":     map[string]any{"result": "remote execution"},
			"status":     "completed",
			"duration":   100,
			"sandboxId":  "sandbox-001",
			"workspaceId": "workspace-test",
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
}

func TestSandboxHarness_ResourcePool(t *testing.T) {
	spec := &v1.SandboxHarnessSpec{
		Type: "docker",
		Mode: v1.SandboxModeExternal,
	}

	h := NewSandboxHarness(spec)
	ctx := context.Background()

	// Acquire resource
	resourceID, err := h.AcquireResource(ctx)
	if err != nil {
		t.Errorf("unexpected error acquiring resource: %v", err)
	}

	if resourceID == "" {
		t.Error("expected resource ID to be returned")
	}

	// Check status
	status := h.GetPoolStatus()
	if status.InUse != 1 {
		t.Errorf("expected 1 in use, got %d", status.InUse)
	}

	// Release resource
	err = h.ReleaseResource(resourceID)
	if err != nil {
		t.Errorf("unexpected error releasing resource: %v", err)
	}

	status = h.GetPoolStatus()
	if status.Available != status.Total {
		t.Errorf("expected all resources to be available after release")
	}
}

func TestSkillsHarness_LocalExecution(t *testing.T) {
	sandboxSpec := &v1.SandboxHarnessSpec{
		Type: "docker",
		Mode: v1.SandboxModeEmbedded, // Embedded = local execution
	}
	sandbox := NewSandboxHarness(sandboxSpec)

	skillsSpec := &v1.SkillsHarnessSpec{
		HubType: "builtin",
		Skills: []v1.SkillConfig{
			{Name: "weather", Version: "1.0", Allowed: true},
			{Name: "calculator", Version: "1.0", Allowed: true},
			{Name: "blocked-skill", Allowed: false},
		},
	}

	h := NewSkillsHarness(skillsSpec, sandbox)
	if h == nil {
		t.Error("expected skills harness to be created")
	}

	// Check execution mode
	if h.IsRemoteExecution() {
		t.Error("expected local execution with embedded sandbox")
	}

	if h.GetSandboxMode() != SandboxModeEmbedded {
		t.Errorf("expected sandbox mode 'embedded', got '%s'", h.GetSandboxMode())
	}

	// List skills
	skills := h.ListSkills()
	if len(skills) != 2 {
		t.Errorf("expected 2 allowed skills, got %d", len(skills))
	}

	// Execute skill locally
	ctx := context.Background()
	result, err := h.ExecuteSkill(ctx, "weather", map[string]any{"location": "London"})
	if err != nil {
		t.Errorf("unexpected error executing skill: %v", err)
	}

	if result == nil {
		t.Error("expected result to be returned")
	}

	if result.Remote {
		t.Error("expected local execution, but result indicates remote")
	}
}

func TestSkillsHarness_RemoteExecution(t *testing.T) {
	// Use mock server for remote execution
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"output":     map[string]any{"temp": 25, "location": "Paris"},
			"status":     "completed",
			"duration":   200,
			"sandboxId":  "sandbox-001",
			"workspaceId": "workspace-test",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	sandboxSpec := &v1.SandboxHarnessSpec{
		Type:     "gvisor",
		Mode:     v1.SandboxModeExternal, // External = remote execution
		Endpoint: server.URL,
		APIKey:   "test-api-key",
	}
	sandbox := NewSandboxHarness(sandboxSpec)

	skillsSpec := &v1.SkillsHarnessSpec{
		HubType: "builtin",
		Skills: []v1.SkillConfig{
			{Name: "weather", Version: "1.0", Allowed: true},
		},
	}

	h := NewSkillsHarness(skillsSpec, sandbox)
	if h == nil {
		t.Error("expected skills harness to be created")
	}

	// Check execution mode
	if !h.IsRemoteExecution() {
		t.Error("expected remote execution with external sandbox")
	}

	// Execute skill remotely
	ctx := context.Background()
	result, err := h.ExecuteSkill(ctx, "weather", map[string]any{"location": "Paris"})
	if err != nil {
		t.Errorf("unexpected error executing skill: %v", err)
	}

	if result == nil {
		t.Error("expected result to be returned")
	}

	if !result.Remote {
		t.Error("expected remote execution, but result indicates local")
	}

	if result.ResourceID == "" {
		t.Error("expected resource ID for remote execution")
	}
}

func TestSkillsHarness_NotAllowed(t *testing.T) {
	sandbox := NewSandboxHarness(&v1.SandboxHarnessSpec{Mode: v1.SandboxModeEmbedded})
	skillsSpec := &v1.SkillsHarnessSpec{
		HubType: "builtin",
		Skills: []v1.SkillConfig{
			{Name: "allowed-skill", Allowed: true},
			{Name: "blocked-skill", Allowed: false},
		},
	}

	h := NewSkillsHarness(skillsSpec, sandbox)
	ctx := context.Background()

	// Try to execute blocked skill
	_, err := h.ExecuteSkill(ctx, "blocked-skill", map[string]any{})
	if err == nil {
		t.Error("expected error for blocked skill")
	}

	// Check skill status
	status := h.GetSkillStatus("allowed-skill")
	if status == nil {
		t.Error("expected skill status to be returned")
	}

	if !status.Available {
		t.Error("expected skill to be available")
	}
}

func TestSkillRouter(t *testing.T) {
	// Test with embedded sandbox
	embeddedSandbox := NewSandboxHarness(&v1.SandboxHarnessSpec{Mode: v1.SandboxModeEmbedded})
	routerEmbedded := NewSkillRouter(embeddedSandbox)

	if routerEmbedded.Route("test-skill") {
		t.Error("expected local routing with embedded sandbox")
	}

	if routerEmbedded.GetExecutionMode() != "local (embedded sandbox)" {
		t.Errorf("unexpected execution mode: %s", routerEmbedded.GetExecutionMode())
	}

	// Test with external sandbox
	externalSandbox := NewSandboxHarness(&v1.SandboxHarnessSpec{Mode: v1.SandboxModeExternal})
	routerExternal := NewSkillRouter(externalSandbox)

	if !routerExternal.Route("test-skill") {
		t.Error("expected remote routing with external sandbox")
	}

	if routerExternal.GetExecutionMode() != "remote (external sandbox)" {
		t.Errorf("unexpected execution mode: %s", routerExternal.GetExecutionMode())
	}

	// Test without sandbox
	routerNoSandbox := NewSkillRouter(nil)

	if routerNoSandbox.Route("test-skill") {
		t.Error("expected local routing without sandbox")
	}

	if routerNoSandbox.GetExecutionMode() != "local (no sandbox)" {
		t.Errorf("unexpected execution mode: %s", routerNoSandbox.GetExecutionMode())
	}
}

func TestKnowledgeHarness(t *testing.T) {
	spec := &v1.KnowledgeHarnessSpec{
		Type: "document",
	}

	h := NewKnowledgeHarness(spec)
	if h == nil {
		t.Error("expected knowledge harness to be created")
	}

	ctx := context.Background()

	// Store document
	doc := KnowledgeDocument{
		ID:      "doc1",
		Content: "This is a test document about AI agents.",
	}
	err := h.Store(ctx, doc)
	if err != nil {
		t.Errorf("unexpected error storing document: %v", err)
	}

	// Query knowledge
	results, err := h.Query(ctx, "AI", 10)
	if err != nil {
		t.Errorf("unexpected error querying: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected query results")
	}
}

func TestInMemoryStore_TTL(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	// Store with short TTL
	err := store.Store(ctx, "ttl-key", []byte("value"), 1) // 1 second TTL
	if err != nil {
		t.Errorf("unexpected error storing: %v", err)
	}

	// Should exist immediately
	exists, _ := store.Exists(ctx, "ttl-key")
	if !exists {
		t.Error("expected key to exist immediately")
	}

	// Wait for TTL
	time.Sleep(2 * time.Second)

	// Should be expired
	exists, _ = store.Exists(ctx, "ttl-key")
	if exists {
		t.Error("expected key to be expired after TTL")
	}
}

func TestMemoryKeys(t *testing.T) {
	stateKey := StateMemoryKey("agent1", "session1", "state1")
	if stateKey != "state:agent1:session1:state1" {
		t.Errorf("unexpected state key: %s", stateKey)
	}

	sessionKey := SessionMemoryKey("agent1", "session1")
	if sessionKey != "session:agent1:session1" {
		t.Errorf("unexpected session key: %s", sessionKey)
	}

	convKey := ConversationMemoryKey("agent1", "session1")
	if convKey != "conversation:agent1:session1" {
		t.Errorf("unexpected conversation key: %s", convKey)
	}
}

func TestHarnessManager_Shutdown(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
			},
		},
		{
			Type: v1.HarnessTypeMemory,
			Memory: &v1.MemoryHarnessSpec{
				Type: "inmemory",
			},
		},
	}
	m.Initialize(ctx, specs)

	err := m.Shutdown(ctx)
	if err != nil {
		t.Errorf("unexpected error shutting down: %v", err)
	}
}

func TestSandboxHealth(t *testing.T) {
	spec := &v1.SandboxHarnessSpec{
		Type: "docker",
		Mode: v1.SandboxModeEmbedded,
	}

	h := NewSandboxHarness(spec)
	ctx := context.Background()

	health, err := h.Health(ctx)
	if err != nil {
		t.Errorf("unexpected error checking health: %v", err)
	}

	if !health.Healthy {
		t.Error("expected sandbox to be healthy")
	}
}
// TestHarnessManager_CheckHarnessHealth tests health check functionality.
func TestHarnessManager_CheckHarnessHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"healthy":   true,
			"available": true,
			"resourcePool": map[string]int{
				"total":     10,
				"available": 8,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeSandbox,
			Sandbox: &v1.SandboxHarnessSpec{
				Type:     "gvisor",
				Mode:     v1.SandboxModeExternal,
				Endpoint: server.URL,
				APIKey:   "test-key",
			},
		},
	}
	m.Initialize(ctx, specs)

	status, err := m.CheckHarnessHealth(ctx, HarnessTypeSandbox)
	if err != nil {
		t.Errorf("unexpected error checking health: %v", err)
	}

	if !status.Available {
		t.Error("expected sandbox to be available")
	}
}

// TestHarnessManager_GetSandboxEndpoint tests endpoint retrieval.
func TestHarnessManager_GetSandboxEndpoint(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	// Without sandbox
	if m.GetSandboxEndpoint() != "" {
		t.Error("expected empty endpoint without sandbox")
	}

	// With external sandbox
	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeSandbox,
			Sandbox: &v1.SandboxHarnessSpec{
				Type:     "gvisor",
				Mode:     v1.SandboxModeExternal,
				Endpoint: "http://sandbox.example.com:9000",
				APIKey:   "test-key",
			},
		},
	}
	m.Initialize(ctx, specs)

	endpoint := m.GetSandboxEndpoint()
	if endpoint != "http://sandbox.example.com:9000" {
		t.Errorf("expected endpoint 'http://sandbox.example.com:9000', got '%s'", endpoint)
	}

	apiKey := m.GetSandboxAPIKey()
	if apiKey != "test-key" {
		t.Errorf("expected API key 'test-key', got '%s'", apiKey)
	}
}

// TestHarnessManager_GetSandboxMode tests mode retrieval.
func TestHarnessManager_GetSandboxMode(t *testing.T) {
	m := NewHarnessManager()

	// Without sandbox - should default to embedded
	if m.GetSandboxMode() != SandboxModeEmbedded {
		t.Error("expected embedded mode as default")
	}

	ctx := context.Background()

	// With embedded sandbox
	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeSandbox,
			Sandbox: &v1.SandboxHarnessSpec{
				Type: "docker",
				Mode: v1.SandboxModeEmbedded,
			},
		},
	}
	m.Initialize(ctx, specs)

	if m.GetSandboxMode() != SandboxModeEmbedded {
		t.Error("expected embedded mode")
	}

	// With external sandbox
	m2 := NewHarnessManager()
	specs2 := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeSandbox,
			Sandbox: &v1.SandboxHarnessSpec{
				Type:     "gvisor",
				Mode:     v1.SandboxModeExternal,
				Endpoint: "http://sandbox.example.com:9000",
			},
		},
	}
	m2.Initialize(ctx, specs2)

	if m2.GetSandboxMode() != SandboxModeExternal {
		t.Error("expected external mode")
	}

	if !m2.IsExternalSandbox() {
		t.Error("expected IsExternalSandbox to return true")
	}
}

// TestHarnessManager_UpdateHarness tests dynamic update.
func TestHarnessManager_UpdateHarness(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	// Initialize with model harness
	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
			},
		},
	}
	m.Initialize(ctx, specs)

	// Update model harness
	newSpec := &v1.HarnessSpec{
		Type: v1.HarnessTypeModel,
		Model: &v1.ModelHarnessSpec{
			Provider:     "openai",
			DefaultModel: "gpt-4",
		},
	}

	err := m.UpdateHarness(ctx, newSpec)
	if err != nil {
		t.Errorf("unexpected error updating harness: %v", err)
	}

	// Verify update
	modelHarness := m.GetModelHarness()
	if modelHarness.GetDefaultModel() != "gpt-4" {
		t.Errorf("expected default model 'gpt-4', got '%s'", modelHarness.GetDefaultModel())
	}
}

// TestHarnessManager_CheckAllHarnessHealth tests health check for all harnesses.
func TestHarnessManager_CheckAllHarnessHealth(t *testing.T) {
	m := NewHarnessManager()
	ctx := context.Background()

	specs := []*v1.HarnessSpec{
		{
			Type: v1.HarnessTypeModel,
			Model: &v1.ModelHarnessSpec{
				Provider:     "deepseek",
				DefaultModel: "deepseek-chat",
			},
		},
		{
			Type: v1.HarnessTypeMemory,
			Memory: &v1.MemoryHarnessSpec{
				Type: "inmemory",
			},
		},
	}
	m.Initialize(ctx, specs)

	// Check all health
	results := m.CheckAllHarnessHealth(ctx)

	if len(results) < 2 {
		t.Errorf("expected at least 2 health results, got %d", len(results))
	}

	for hType, status := range results {
		if !status.Available {
			t.Errorf("expected harness %s to be available", hType)
		}
	}
}
