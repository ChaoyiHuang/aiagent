// Config Daemon entry point.
// Watches AIAgent CRDs in the namespace and syncs AgentConfig to hostPath.
// This enables AgentHandler to read agent configurations without K8s API calls.
//
// Architecture:
// ┌─────────────────────────────────────────────────────────────────┐
// │ Namespace Config Daemon (DaemonSet)                             │
// │                                                                 │
// │  - Watches AIAgent CRDs via Informer                            │
// │  - For each AIAgent with AgentConfig:                           │
// │    - Write to /var/lib/aiagent/configs/<namespace>/<name>/      │
// │    - Create agent-config.json (from spec.agentConfig)           │
// │    - Create agent-meta.yaml (name, namespace, phase, etc.)      │
// │  - Maintains agent-index.yaml PER RUNTIME (not per namespace)   │
// │    - Path: /var/lib/aiagent/configs/<ns>/<runtime>/agent-index.yaml │
// │                                                                 │
// │  AgentRuntime Pod                                               │
// │  - Mounts hostPath for SPECIFIC RUNTIME                         │
// │  - Path: /var/lib/aiagent/configs/<ns>/<runtime>                │
// │  - Handler reads agent-index.yaml with ONLY its agents          │
// │  - No K8s API calls needed                                      │
// └─────────────────────────────────────────────────────────────────┘
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	namespace      = flag.String("namespace", "", "Namespace to watch (empty means all namespaces)")
	configBasePath = flag.String("configpath", "", "Base path for config files (default: /var/lib/aiagent/configs)")
	debug          = flag.Bool("debug", false, "Enable debug logging")
)

const (
	DefaultConfigBasePath = "/var/lib/aiagent/configs"

	// File names for each agent
	AgentConfigFile = "agent-config.json"
	AgentMetaFile   = "agent-meta.yaml"

	// Agent index file for namespace
	AgentIndexFile = "agent-index.yaml"
)

// GVR for AIAgent CRD
var aiAgentGVR = schema.GroupVersionResource{
	Group:    "agent.ai",
	Version:  "v1",
	Resource: "aiagents",
}

// AgentMeta contains metadata about an agent for the handler.
type AgentMeta struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Phase     string `yaml:"phase"`
	Runtime   string `yaml:"runtime,omitempty"`
	UID       string `yaml:"uid,omitempty"`
}

// AgentIndexEntry represents an entry in the agent index.
type AgentIndexEntry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Phase     string `yaml:"phase"`
	Runtime   string `yaml:"runtime,omitempty"`
	UID       string `yaml:"uid,omitempty"`
}

// AgentIndex represents the agent index structure.
type AgentIndex struct {
	Agents []AgentIndexEntry `yaml:"agents"`
}

// AgentInfo holds parsed agent information from unstructured data.
type AgentInfo struct {
	Name         string
	Namespace    string
	UID          types.UID
	Phase        string
	RuntimeName  string
	RuntimeType  string
	Description  string
	AgentConfig  *apiextensionsv1.JSON
}

// ConfigDaemon manages syncing AIAgent configs to hostPath.
type ConfigDaemon struct {
	k8sClient    *kubernetes.Clientset
	dynamicClient dynamic.Interface
	namespace    string
	configBase   string
	running      bool
	stopCh       chan struct{}
	agentTracker map[string]*AgentInfo // Track known agents by namespace/name
}

func main() {
	flag.Parse()

	// Get namespace from environment or flag
	ns := *namespace
	if ns == "" {
		ns = os.Getenv("WATCH_NAMESPACE")
		if ns == "" {
			ns = metav1.NamespaceAll // Watch all namespaces if not specified
		}
	}

	// Get config base path from environment or flag
	configPath := *configBasePath
	if configPath == "" {
		configPath = os.Getenv("CONFIG_BASE_PATH")
		if configPath == "" {
			configPath = DefaultConfigBasePath
		}
	}

	log.Printf("Config Daemon starting...")
	log.Printf("Namespace: %s", ns)
	log.Printf("Config Base Path: %s", configPath)

	// Create Kubernetes clients
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Try controller-runtime fallback
		cfg = ctrl.GetConfigOrDie()
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create dynamic client: %v", err)
	}

	// Create config daemon
	daemon := &ConfigDaemon{
		k8sClient:     k8sClient,
		dynamicClient: dynamicClient,
		namespace:     ns,
		configBase:    configPath,
		agentTracker:  make(map[string]*AgentInfo),
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		cancel()
		daemon.Stop()
	}()

	// Run the daemon
	if err := daemon.Run(ctx); err != nil {
		log.Fatalf("Config Daemon error: %v", err)
	}

	log.Printf("Config Daemon shutdown complete")
}

// Run starts the config daemon's informer loop.
func (d *ConfigDaemon) Run(ctx context.Context) error {
	log.Printf("Starting Config Daemon...")

	// Create config base directory
	if err := os.MkdirAll(d.configBase, 0755); err != nil {
		log.Printf("Warning: failed to create config base directory: %v", err)
	}

	// Setup informer for AIAgent CRDs using dynamic client
	resyncInterval := 30 * time.Second
	informerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		d.dynamicClient,
		resyncInterval,
		d.namespace,
		nil,
	)

	agentInformer := informerFactory.ForResource(aiAgentGVR).Informer()

	// Register event handlers
	agentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.onAgentAdd,
		UpdateFunc: d.onAgentUpdate,
		DeleteFunc: d.onAgentDelete,
	})

	// Start informer
	d.stopCh = make(chan struct{})
	d.running = true
	informerFactory.Start(d.stopCh)

	// Wait for caches to sync
	if !cache.WaitForCacheSync(d.stopCh, agentInformer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}

	log.Printf("Informer synced, watching AIAgent CRDs")

	// Wait for stop signal
	<-ctx.Done()

	log.Printf("Stopping Config Daemon...")
	return nil
}

// Stop stops the daemon.
func (d *ConfigDaemon) Stop() {
	if d.running {
		close(d.stopCh)
		d.running = false
	}
}

// onAgentAdd handles new AIAgent creation.
func (d *ConfigDaemon) onAgentAdd(obj interface{}) {
	agent, err := d.parseAgentInfo(obj)
	if err != nil {
		log.Printf("Warning: failed to parse agent: %v", err)
		return
	}

	key := d.agentKey(agent)
	d.agentTracker[key] = agent

	if *debug {
		log.Printf("Agent added: %s/%s (phase: %s)", agent.Namespace, agent.Name, agent.Phase)
	}

	// Sync config to hostPath
	d.syncAgentConfig(agent)
}

// onAgentUpdate handles AIAgent updates.
func (d *ConfigDaemon) onAgentUpdate(oldObj, newObj interface{}) {
	oldAgent, err := d.parseAgentInfo(oldObj)
	if err != nil {
		return
	}

	newAgent, err := d.parseAgentInfo(newObj)
	if err != nil {
		return
	}

	key := d.agentKey(newAgent)
	d.agentTracker[key] = newAgent

	// Handle runtime binding changes: need to clean up old runtime and update both indexes
	if oldAgent.RuntimeName != newAgent.RuntimeName && oldAgent.RuntimeName != "" {
		// Remove config from old runtime directory
		oldAgentDir := filepath.Join(d.configBase, oldAgent.Namespace, oldAgent.RuntimeName, oldAgent.Name)
		if err := os.RemoveAll(oldAgentDir); err != nil {
			log.Printf("Error removing old agent config directory %s: %v", oldAgentDir, err)
		}
		// Update old runtime index
		d.updateRuntimeIndex(oldAgent.Namespace, oldAgent.RuntimeName)
		if *debug {
			log.Printf("Agent moved from runtime %s to %s: %s/%s", oldAgent.RuntimeName, newAgent.RuntimeName, newAgent.Namespace, newAgent.Name)
		}
	}

	// Check if relevant fields changed
	if d.shouldUpdateConfig(oldAgent, newAgent) {
		if *debug {
			log.Printf("Agent updated: %s/%s (phase: %s -> %s)",
				newAgent.Namespace, newAgent.Name, oldAgent.Phase, newAgent.Phase)
		}
		d.syncAgentConfig(newAgent)
	}

	// Always update agent index on phase changes
	if oldAgent.Phase != newAgent.Phase {
		d.updateRuntimeIndex(newAgent.Namespace, newAgent.RuntimeName)
	}
}

// onAgentDelete handles AIAgent deletion.
func (d *ConfigDaemon) onAgentDelete(obj interface{}) {
	agent, err := d.parseAgentInfo(obj)
	if err != nil {
		// Handle DeletedFinalStateUnknown
		dObj, ok := obj.(cache.DeletedFinalStateUnknown)
		if ok {
			agent, err = d.parseAgentInfo(dObj.Obj)
			if err != nil {
				return
			}
		} else {
			return
		}
	}

	key := d.agentKey(agent)
	delete(d.agentTracker, key)

	if *debug {
		log.Printf("Agent deleted: %s/%s", agent.Namespace, agent.Name)
	}

	// Remove config files (also updates runtime index)
	d.deleteAgentConfig(agent)
}

// parseAgentInfo extracts AgentInfo from unstructured object.
func (d *ConfigDaemon) parseAgentInfo(obj interface{}) (*AgentInfo, error) {
	// Handle unstructured.Unstructured type (from dynamic informer)
	unstruct, ok := obj.(*unstructured.Unstructured)
	if ok {
		// Use UnstructuredContent() to get the map
		return d.parseAgentFromMap(unstruct.UnstructuredContent(), unstruct.GetNamespace(), unstruct.GetName(), unstruct.GetUID())
	}

	// Handle map[string]interface{} directly (for tests or other cases)
	unstructMap, ok := obj.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected object type: %T, expected *unstructured.Unstructured or map[string]interface{}", obj)
	}

	// Extract name and namespace from metadata
	metadata, ok := unstructMap["metadata"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing metadata")
	}

	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)
	uidStr, _ := metadata["uid"].(string)

	return d.parseAgentFromMap(unstructMap, namespace, name, types.UID(uidStr))
}

// parseAgentFromMap parses agent info from unstructured map.
func (d *ConfigDaemon) parseAgentFromMap(obj map[string]interface{}, ns, name string, uid types.UID) (*AgentInfo, error) {
	info := &AgentInfo{
		Name:      name,
		Namespace: ns,
		UID:       uid,
	}

	// Get spec
	spec, ok := obj["spec"].(map[string]interface{})
	if ok {
		// Get runtimeRef
		runtimeRef, ok := spec["runtimeRef"].(map[string]interface{})
		if ok {
			if rn, ok := runtimeRef["name"].(string); ok {
				info.RuntimeName = rn
			}
			if rt, ok := runtimeRef["type"].(string); ok {
				info.RuntimeType = rt
			}
		}

		// Get description
		if desc, ok := spec["description"].(string); ok {
			info.Description = desc
		}

		// Get agentConfig
		if ac, ok := spec["agentConfig"].(map[string]interface{}); ok {
			// Convert to JSON
			acJSON, err := json.Marshal(ac)
			if err == nil {
				info.AgentConfig = &apiextensionsv1.JSON{Raw: acJSON}
			}
		}
	}

	// Get status
	status, ok := obj["status"].(map[string]interface{})
	if ok {
		if phase, ok := status["phase"].(string); ok {
			info.Phase = phase
		}
	}

	return info, nil
}

// agentKey generates a unique key for an agent.
func (d *ConfigDaemon) agentKey(agent *AgentInfo) string {
	return fmt.Sprintf("%s/%s", agent.Namespace, agent.Name)
}

// shouldUpdateConfig determines if config files need updating.
func (d *ConfigDaemon) shouldUpdateConfig(old, new *AgentInfo) bool {
	// Check agentConfig changes
	if !d.compareJSON(old.AgentConfig, new.AgentConfig) {
		return true
	}

	// Check description changes
	if old.Description != new.Description {
		return true
	}

	// Check runtime binding changes
	if old.RuntimeName != new.RuntimeName || old.RuntimeType != new.RuntimeType {
		return true
	}

	return false
}

// compareJSON compares two JSON objects.
func (d *ConfigDaemon) compareJSON(a, b *apiextensionsv1.JSON) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return string(a.Raw) == string(b.Raw)
}

// syncAgentConfig writes agent config files to hostPath.
// Directory structure: /var/lib/aiagent/configs/<namespace>/<runtime-name>/<agent-name>/agent-config.json
// This ensures Handler mounting runtime-specific directory can find agent configs.
// IMPORTANT: Resolves tokenSecretRef to actual token values from Secrets.
func (d *ConfigDaemon) syncAgentConfig(agent *AgentInfo) {
	// Skip agents without runtime binding
	if agent.RuntimeName == "" {
		if *debug {
			log.Printf("Skipping sync for agent %s with no runtime binding", agent.Name)
		}
		return
	}

	// Create agent config directory inside runtime directory
	// Path: /var/lib/aiagent/configs/<namespace>/<runtime-name>/<agent-name>/
	agentDir := filepath.Join(d.configBase, agent.Namespace, agent.RuntimeName, agent.Name)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		log.Printf("Error creating agent config directory %s: %v", agentDir, err)
		return
	}

	// Write agent-config.json (from spec.agentConfig)
	// Resolve any tokenSecretRef to actual token values
	if agent.AgentConfig != nil && len(agent.AgentConfig.Raw) > 0 {
		configPath := filepath.Join(agentDir, AgentConfigFile)
		// Validate JSON and process secret references
		var jsonData interface{}
		if err := json.Unmarshal(agent.AgentConfig.Raw, &jsonData); err != nil {
			log.Printf("Error parsing agentConfig JSON for %s: %v", d.agentKey(agent), err)
			// Write raw data anyway for debugging
			os.WriteFile(configPath, agent.AgentConfig.Raw, 0644)
		} else {
			// Resolve all tokenSecretRef references recursively (framework-agnostic)
			jsonData = d.resolveTokenSecretRefs(jsonData, agent.Namespace)

			// Pretty-print JSON
			prettyJSON, _ := json.MarshalIndent(jsonData, "", "  ")
			if err := os.WriteFile(configPath, prettyJSON, 0644); err != nil {
				log.Printf("Error writing agent config for %s: %v", d.agentKey(agent), err)
			} else if *debug {
				log.Printf("Wrote agent config for %s to %s", d.agentKey(agent), configPath)
			}
		}
	}

	// Write agent-meta.yaml
	meta := AgentMeta{
		Name:      agent.Name,
		Namespace: agent.Namespace,
		Phase:     agent.Phase,
		Runtime:   agent.RuntimeName,
		UID:       string(agent.UID),
	}
	metaData, err := yaml.Marshal(meta)
	if err != nil {
		log.Printf("Error marshaling agent meta for %s: %v", d.agentKey(agent), err)
		return
	}
	metaPath := filepath.Join(agentDir, AgentMetaFile)
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		log.Printf("Error writing agent meta for %s: %v", d.agentKey(agent), err)
	} else if *debug {
		log.Printf("Wrote agent meta for %s to %s", d.agentKey(agent), metaPath)
	}

	// Update runtime index for this specific runtime
	d.updateRuntimeIndex(agent.Namespace, agent.RuntimeName)
}

// deleteAgentConfig removes agent config files from runtime directory.
func (d *ConfigDaemon) deleteAgentConfig(agent *AgentInfo) {
	// Skip if no runtime binding
	if agent.RuntimeName == "" {
		return
	}

	// Remove from runtime-specific directory
	agentDir := filepath.Join(d.configBase, agent.Namespace, agent.RuntimeName, agent.Name)
	if err := os.RemoveAll(agentDir); err != nil {
		log.Printf("Error removing agent config directory %s: %v", agentDir, err)
	}

	// Update runtime index for this specific runtime
	d.updateRuntimeIndex(agent.Namespace, agent.RuntimeName)
}

// updateRuntimeIndex writes agent-index.yaml for a specific runtime.
// Path: /var/lib/aiagent/configs/<namespace>/<runtime-name>/agent-index.yaml
func (d *ConfigDaemon) updateRuntimeIndex(namespace string, runtimeName string) {
	// Collect all agents for this specific runtime
	entries := []AgentIndexEntry{}
	for _, agent := range d.agentTracker {
		if agent.Namespace != namespace || agent.RuntimeName != runtimeName {
			continue
		}
		// Only include agents in Running, Pending, Scheduling, or Migrating phase
		if agent.Phase != "Running" && agent.Phase != "Pending" && agent.Phase != "Scheduling" && agent.Phase != "Migrating" {
			continue
		}
		entries = append(entries, AgentIndexEntry{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Phase:     agent.Phase,
			Runtime:   agent.RuntimeName,
			UID:       string(agent.UID),
		})
	}

	// Create runtime directory
	runtimeDir := filepath.Join(d.configBase, namespace, runtimeName)
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		log.Printf("Error creating runtime directory %s: %v", runtimeDir, err)
		return
	}

	// Write agent-index.yaml for this runtime
	index := AgentIndex{Agents: entries}
	indexData, err := yaml.Marshal(index)
	if err != nil {
		log.Printf("Error marshaling agent index for runtime %s/%s: %v", namespace, runtimeName, err)
		return
	}

	indexPath := filepath.Join(runtimeDir, AgentIndexFile)
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		log.Printf("Error writing agent index for runtime %s/%s: %v", namespace, runtimeName, err)
	} else if *debug {
		log.Printf("Updated agent index for runtime %s/%s (%d agents)", namespace, runtimeName, len(entries))
	}
}

// updateAllRuntimeIndexesInNamespace writes agent-index.yaml for ALL runtimes in a namespace.
// This is useful for initial setup or when we need to refresh all runtime indexes.
// For incremental updates, use updateRuntimeIndex() instead.
func (d *ConfigDaemon) updateAllRuntimeIndexesInNamespace(namespace string) {
	// Group agents by runtime within this namespace
	runtimeAgents := make(map[string][]AgentIndexEntry)

	for _, agent := range d.agentTracker {
		if agent.Namespace != namespace {
			continue
		}
		// Only include agents in Running, Pending, Scheduling, or Migrating phase
		// Migrating agents should still be processed by handler during pod restarts
		if agent.Phase != "Running" && agent.Phase != "Pending" && agent.Phase != "Scheduling" && agent.Phase != "Migrating" {
			continue
		}

		// Skip agents without runtime binding
		if agent.RuntimeName == "" {
			if *debug {
				log.Printf("Skipping agent %s with no runtime binding", agent.Name)
			}
			continue
		}

		runtimeAgents[agent.RuntimeName] = append(runtimeAgents[agent.RuntimeName], AgentIndexEntry{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Phase:     agent.Phase,
			Runtime:   agent.RuntimeName,
			UID:       string(agent.UID),
		})
	}

	// Write agent-index.yaml for each runtime
	nsDir := filepath.Join(d.configBase, namespace)
	if err := os.MkdirAll(nsDir, 0755); err != nil {
		log.Printf("Error creating namespace directory %s: %v", nsDir, err)
		return
	}

	for runtimeName, entries := range runtimeAgents {
		// Create runtime directory
		runtimeDir := filepath.Join(nsDir, runtimeName)
		if err := os.MkdirAll(runtimeDir, 0755); err != nil {
			log.Printf("Error creating runtime directory %s: %v", runtimeDir, err)
			continue
		}

		// Create index for this runtime
		index := AgentIndex{Agents: entries}
		indexData, err := yaml.Marshal(index)
		if err != nil {
			log.Printf("Error marshaling agent index for runtime %s/%s: %v", namespace, runtimeName, err)
			continue
		}

		indexPath := filepath.Join(runtimeDir, AgentIndexFile)
		if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
			log.Printf("Error writing agent index for runtime %s/%s: %v", namespace, runtimeName, err)
		} else if *debug {
			log.Printf("Updated agent index for runtime %s/%s (%d agents)", namespace, runtimeName, len(entries))
		}
	}

	// Also write a namespace-level index for debugging/listing purposes
	// This is NOT used by Handler, but useful for admin to see all agents
	allEntries := []AgentIndexEntry{}
	for _, entries := range runtimeAgents {
		allEntries = append(allEntries, entries...)
	}
	allIndex := AgentIndex{Agents: allEntries}
	allIndexData, err := yaml.Marshal(allIndex)
	if err != nil {
		log.Printf("Error marshaling full namespace index: %v", err)
		return
	}
	allIndexPath := filepath.Join(nsDir, "all-agents.yaml")
	if err := os.WriteFile(allIndexPath, allIndexData, 0644); err != nil {
		log.Printf("Error writing full namespace index: %v", err)
	} else if *debug {
		log.Printf("Updated full namespace index at %s (%d agents)", allIndexPath, len(allEntries))
	}
}

// resolveTokenSecretRefs recursively walks the JSON tree and resolves any tokenSecretRef
// fields to actual token values from K8s Secrets. This is fully framework-agnostic —
// it finds tokenSecretRef at ANY path (channels.discord, channels.telegram, or any
// future framework-specific path) without understanding the semantics.
func (d *ConfigDaemon) resolveTokenSecretRefs(data interface{}, namespace string) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// If this map has a tokenSecretRef, resolve it to the actual token
		if ref, ok := v["tokenSecretRef"].(string); ok && ref != "" {
			secret, err := d.k8sClient.CoreV1().Secrets(namespace).Get(context.TODO(), ref, metav1.GetOptions{})
			if err != nil {
				log.Printf("Error reading secret %s for tokenSecretRef: %v", ref, err)
			} else if tokenValue, ok := secret.Data["token"]; ok {
				v["token"] = string(tokenValue)
				delete(v, "tokenSecretRef")
				if *debug {
					log.Printf("Resolved tokenSecretRef %s", ref)
				}
			} else {
				log.Printf("Secret %s does not have 'token' key", ref)
			}
		}
		// Recursively process all values
		for key, val := range v {
			v[key] = d.resolveTokenSecretRefs(val, namespace)
		}
		return v
	case []interface{}:
		for i, val := range v {
			v[i] = d.resolveTokenSecretRefs(val, namespace)
		}
		return v
	default:
		return data
	}
}

// GetAgentTracker returns the current agent tracker state.
func (d *ConfigDaemon) GetAgentTracker() map[string]*AgentInfo {
	return d.agentTracker
}