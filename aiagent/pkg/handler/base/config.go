package base

import (
	"os"
	"path/filepath"
	"sync"
)

// ConfigManager manages configuration files in shared work directory.
type ConfigManager struct {
	configDir string
	mu        sync.Mutex
}

// NewConfigManager creates a new config manager.
func NewConfigManager(workDir string) *ConfigManager {
	configDir := filepath.Join(workDir, "config")
	os.MkdirAll(configDir, 0755)

	return &ConfigManager{
		configDir: configDir,
	}
}

// WriteConfig writes a configuration file.
func (m *ConfigManager) WriteConfig(agentID string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := filepath.Join(m.configDir, agentID+".yaml")
	return os.WriteFile(configPath, data, 0644)
}

// ReadConfig reads a configuration file.
func (m *ConfigManager) ReadConfig(agentID string) ([]byte, error) {
	configPath := filepath.Join(m.configDir, agentID+".yaml")
	return os.ReadFile(configPath)
}

// DeleteConfig deletes a configuration file.
func (m *ConfigManager) DeleteConfig(agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := filepath.Join(m.configDir, agentID+".yaml")
	return os.Remove(configPath)
}

// ConfigPath returns the path to a config file.
func (m *ConfigManager) ConfigPath(agentID string) string {
	return filepath.Join(m.configDir, agentID+".yaml")
}

// ConfigDir returns the config directory path.
func (m *ConfigManager) ConfigDir() string {
	return m.configDir
}

// ListConfigs lists all configuration files.
func (m *ConfigManager) ListConfigs() ([]string, error) {
	entries, err := os.ReadDir(m.configDir)
	if err != nil {
		return nil, err
	}

	configs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			// Remove .yaml extension to get agentID
			agentID := entry.Name()[:len(entry.Name())-5]
			configs = append(configs, agentID)
		}
	}
	return configs, nil
}

// Exists checks if a config file exists.
func (m *ConfigManager) Exists(agentID string) bool {
	configPath := filepath.Join(m.configDir, agentID+".yaml")
	_, err := os.Stat(configPath)
	return err == nil
}

// WorkDir manages shared work directory.
type WorkDir struct {
	path string
}

// NewWorkDir creates a work directory manager.
func NewWorkDir(path string) *WorkDir {
	os.MkdirAll(path, 0755)
	os.MkdirAll(filepath.Join(path, "config"), 0755)
	os.MkdirAll(filepath.Join(path, "sessions"), 0755)
	os.MkdirAll(filepath.Join(path, "artifacts"), 0755)

	return &WorkDir{path: path}
}

// Path returns the work directory path.
func (w *WorkDir) Path() string {
	return w.path
}

// ConfigDir returns the config subdirectory.
func (w *WorkDir) ConfigDir() string {
	return filepath.Join(w.path, "config")
}

// SessionDir returns the session subdirectory.
func (w *WorkDir) SessionDir() string {
	return filepath.Join(w.path, "sessions")
}

// ArtifactsDir returns the artifacts subdirectory.
func (w *WorkDir) ArtifactsDir() string {
	return filepath.Join(w.path, "artifacts")
}

// AgentWorkDir returns agent-specific work directory.
func (w *WorkDir) AgentWorkDir(agentID string) string {
	return filepath.Join(w.path, "agents", agentID)
}

// CreateAgentWorkDir creates agent-specific work directory.
func (w *WorkDir) CreateAgentWorkDir(agentID string) error {
	agentDir := w.AgentWorkDir(agentID)
	return os.MkdirAll(agentDir, 0755)
}

// Cleanup cleans up work directory.
func (w *WorkDir) Cleanup() error {
	// Only cleanup subdirectories, keep base
	configDir := w.ConfigDir()
	sessionDir := w.SessionDir()
	artifactsDir := w.ArtifactsDir()

	os.RemoveAll(configDir)
	os.RemoveAll(sessionDir)
	os.RemoveAll(artifactsDir)
	os.RemoveAll(filepath.Join(w.path, "agents"))

	// Recreate directories
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(sessionDir, 0755)
	os.MkdirAll(artifactsDir, 0755)

	return nil
}