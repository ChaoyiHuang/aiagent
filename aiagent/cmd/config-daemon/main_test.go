package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestAgentMetaYAML(t *testing.T) {
	meta := AgentMeta{
		Name:      "test-agent",
		Namespace: "default",
		Phase:     "Running",
		Runtime:   "test-runtime",
		UID:       "test-uid",
	}

	data, err := yaml.Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to marshal AgentMeta: %v", err)
	}

	// Verify the YAML structure
	var parsed AgentMeta
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal AgentMeta: %v", err)
	}

	if parsed.Name != meta.Name {
		t.Errorf("Name mismatch: got %s, want %s", parsed.Name, meta.Name)
	}
	if parsed.Namespace != meta.Namespace {
		t.Errorf("Namespace mismatch: got %s, want %s", parsed.Namespace, meta.Namespace)
	}
	if parsed.Phase != meta.Phase {
		t.Errorf("Phase mismatch: got %s, want %s", parsed.Phase, meta.Phase)
	}
}

func TestAgentIndexYAML(t *testing.T) {
	index := AgentIndex{
		Agents: []AgentIndexEntry{
			{
				Name:      "agent-1",
				Namespace: "default",
				Phase:     "Running",
				Runtime:   "runtime-1",
				UID:       "uid-1",
			},
			{
				Name:      "agent-2",
				Namespace: "default",
				Phase:     "Pending",
				Runtime:   "runtime-1",
				UID:       "uid-2",
			},
		},
	}

	data, err := yaml.Marshal(index)
	if err != nil {
		t.Fatalf("Failed to marshal AgentIndex: %v", err)
	}

	// Verify the YAML structure
	var parsed AgentIndex
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal AgentIndex: %v", err)
	}

	if len(parsed.Agents) != 2 {
		t.Errorf("Agent count mismatch: got %d, want 2", len(parsed.Agents))
	}

	if parsed.Agents[0].Name != "agent-1" {
		t.Errorf("First agent name mismatch: got %s, want agent-1", parsed.Agents[0].Name)
	}
}

func TestAgentConfigJSON(t *testing.T) {
	// Simulate agentConfig from AIAgent spec
	agentConfig := map[string]interface{}{
		"instruction": "You are a helpful assistant",
		"model":       "gpt-4",
		"tools": []interface{}{
			map[string]interface{}{
				"name": "search",
				"type": "web_search",
			},
		},
	}

	// Convert to JSON (as Config Daemon does)
	jsonData, err := json.MarshalIndent(agentConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal agentConfig: %v", err)
	}

	// Verify JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal agentConfig: %v", err)
	}

	if parsed["instruction"] != agentConfig["instruction"] {
		t.Errorf("Instruction mismatch")
	}
}

func TestConfigDaemonFileWriting(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "config-daemon-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Simulate agent directory structure
	agentDir := filepath.Join(tmpDir, "default", "test-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create agent directory: %v", err)
	}

	// Write agent-config.json
	agentConfig := map[string]interface{}{
		"instruction": "Test instruction",
		"model":       "test-model",
	}
	jsonData, _ := json.MarshalIndent(agentConfig, "", "  ")
	configPath := filepath.Join(agentDir, AgentConfigFile)
	if err := os.WriteFile(configPath, jsonData, 0644); err != nil {
		t.Fatalf("Failed to write agent config: %v", err)
	}

	// Write agent-meta.yaml
	meta := AgentMeta{
		Name:      "test-agent",
		Namespace: "default",
		Phase:     "Running",
		Runtime:   "test-runtime",
	}
	metaData, _ := yaml.Marshal(meta)
	metaPath := filepath.Join(agentDir, AgentMetaFile)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		t.Fatalf("Failed to write agent meta: %v", err)
	}

	// Write agent-index.yaml
	index := AgentIndex{
		Agents: []AgentIndexEntry{
			{
				Name:      "test-agent",
				Namespace: "default",
				Phase:     "Running",
				Runtime:   "test-runtime",
			},
		},
	}
	indexData, _ := yaml.Marshal(index)
	indexPath := filepath.Join(tmpDir, "default", AgentIndexFile)
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		t.Fatalf("Failed to write agent index: %v", err)
	}

	// Verify files exist
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("Agent config file not found: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("Agent meta file not found: %v", err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("Agent index file not found: %v", err)
	}

	// Read and verify agent config
	readData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read agent config: %v", err)
	}
	var readConfig map[string]interface{}
	if err := json.Unmarshal(readData, &readConfig); err != nil {
		t.Fatalf("Failed to parse agent config: %v", err)
	}
	if readConfig["instruction"] != agentConfig["instruction"] {
		t.Errorf("Agent config instruction mismatch")
	}
}

func TestCompareJSON(t *testing.T) {
	// Create a test daemon
	daemon := &ConfigDaemon{}

	// Test nil comparison
	if !daemon.compareJSON(nil, nil) {
		t.Error("compareJSON should return true for both nil")
	}

	// Test nil vs non-nil
	json1 := &apiextensionsv1.JSON{Raw: []byte(`{"key": "value"}`)}
	if daemon.compareJSON(nil, json1) {
		t.Error("compareJSON should return false for nil vs non-nil")
	}
	if daemon.compareJSON(json1, nil) {
		t.Error("compareJSON should return false for non-nil vs nil")
	}

	// Test equal JSON
	json2 := &apiextensionsv1.JSON{Raw: []byte(`{"key": "value"}`)}
	if !daemon.compareJSON(json1, json2) {
		t.Error("compareJSON should return true for equal JSON")
	}

	// Test different JSON
	json3 := &apiextensionsv1.JSON{Raw: []byte(`{"key": "different"}`)}
	if daemon.compareJSON(json1, json3) {
		t.Error("compareJSON should return false for different JSON")
	}
}

func TestShouldUpdateConfig(t *testing.T) {
	daemon := &ConfigDaemon{}

	oldInfo := &AgentInfo{
		Name:         "agent-1",
		Namespace:    "default",
		Phase:        "Running",
		RuntimeName:  "runtime-1",
		Description:  "Old description",
		AgentConfig:  &apiextensionsv1.JSON{Raw: []byte(`{"instruction": "old"}`)},
	}

	newInfo := &AgentInfo{
		Name:         "agent-1",
		Namespace:    "default",
		Phase:        "Running",
		RuntimeName:  "runtime-1",
		Description:  "New description",
		AgentConfig:  &apiextensionsv1.JSON{Raw: []byte(`{"instruction": "old"}`)},
	}

	// Description changed - should update
	if !daemon.shouldUpdateConfig(oldInfo, newInfo) {
		t.Error("shouldUpdateConfig should return true for description change")
	}

	// No changes - should not update
	newInfo.Description = "Old description"
	if daemon.shouldUpdateConfig(oldInfo, newInfo) {
		t.Error("shouldUpdateConfig should return false when no changes")
	}

	// AgentConfig changed - should update
	newInfo.AgentConfig = &apiextensionsv1.JSON{Raw: []byte(`{"instruction": "new"}`)}
	if !daemon.shouldUpdateConfig(oldInfo, newInfo) {
		t.Error("shouldUpdateConfig should return true for agentConfig change")
	}
}