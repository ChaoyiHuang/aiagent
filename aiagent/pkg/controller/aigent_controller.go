// Package controller provides AIAgent Controller for managing AI Agent business objects.
// AIAgent Controller handles:
// - Agent scheduling to AgentRuntime
// - Agent ConfigMap creation
// - Agent Index ConfigMap updates (notify AgentHandler)
// - PVC lifecycle management
// - Agent migration support
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"gopkg.in/yaml.v3"

	"aiagent/api/v1"
	"aiagent/pkg/scheduler"
)

const (
	// AIAgentFinalizer is used for cleanup on deletion.
	AIAgentFinalizer = "agent.ai/aigent-finalizer"

	// AgentConfigMapPrefix is the prefix for agent-specific ConfigMaps.
	AgentConfigMapPrefix = "agent-config-"

	// AgentIndexConfigMapPrefix is the prefix for agent index ConfigMaps.
	AgentIndexConfigMapPrefix = "agent-index-"

	// AgentPVCPrefix is the prefix for agent PVCs.
	AgentPVCPrefix = "agent-pvc-"
)

// AIAgentReconciler reconciles an AIAgent object.
type AIAgentReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Scheduler scheduler.Scheduler
}

// SetupWithManager sets up the controller with the Manager.
func (r *AIAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.AIAgent{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Watches(
			&v1.AgentRuntime{},
			handler.EnqueueRequestsFromMapFunc(r.runtimeToAgentMapper),
		).
		Complete(r)
}

// runtimeToAgentMapper maps AgentRuntime changes to AIAgent reconciles.
// When a Runtime's status changes, we need to update bound agents.
func (r *AIAgentReconciler) runtimeToAgentMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	runtime := obj.(*v1.AgentRuntime)
	log := log.FromContext(ctx)

	// Find all AIAgents bound to this runtime
	agents := &v1.AIAgentList{}
	if err := r.List(ctx, agents, client.InNamespace(runtime.Namespace)); err != nil {
		log.Error(err, "failed to list AIAgents for runtime mapping")
		return nil
	}

	requests := []reconcile.Request{}
	for _, agent := range agents.Items {
		// Check if agent is bound to this runtime
		if agent.Status.RuntimeRef.Name == runtime.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      agent.Name,
					Namespace: agent.Namespace,
				},
			})
		}
	}

	// Also include agents in status.Agents (for completeness)
	for _, agentBinding := range runtime.Status.Agents {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      agentBinding.Name,
				Namespace: agentBinding.Namespace,
			},
		})
	}

	return requests
}

//+kubebuilder:rbac:groups=agent.ai,resources=aigents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=agent.ai,resources=aigents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=agent.ai,resources=aigents/finalizers,verbs=update
//+kubebuilder:rbac:groups=agent.ai,resources=agentruntimes,verbs=get;list;watch
//+kubebuilder:rbac:groups=agent.ai,resources=agentruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles the reconciliation loop for AIAgent.
func (r *AIAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling AIAgent", "name", req.Name, "namespace", req.Namespace)

	// Fetch the AIAgent
	agent := &v1.AIAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AIAgent not found, already deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !agent.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, agent)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(agent, AIAgentFinalizer) {
		controllerutil.AddFinalizer(agent, AIAgentFinalizer)
		if err := r.Update(ctx, agent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Phase: Pending -> Scheduling
	if agent.Status.Phase == v1.AgentPhasePending || agent.Status.Phase == "" {
		return r.handleScheduling(ctx, agent)
	}

	// Phase: Scheduling -> Running
	if agent.Status.Phase == v1.AgentPhaseScheduling {
		return r.handleBinding(ctx, agent)
	}

	// Phase: Running - maintain agent
	if agent.Status.Phase == v1.AgentPhaseRunning {
		return r.handleRunning(ctx, agent)
	}

	// Phase: Migrating
	if agent.Status.Phase == v1.AgentPhaseMigrating {
		return r.handleMigration(ctx, agent)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleDeletion handles the AIAgent deletion process.
func (r *AIAgentReconciler) handleDeletion(ctx context.Context, agent *v1.AIAgent) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(agent, AIAgentFinalizer) {
		// Cleanup resources
		if err := r.cleanupAgentResources(ctx, agent); err != nil {
			log.Error(err, "failed to cleanup agent resources")
			return ctrl.Result{}, err
		}

		// Remove from AgentRuntime status
		if err := r.unbindFromRuntime(ctx, agent); err != nil {
			log.Error(err, "failed to unbind from runtime")
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(agent, AIAgentFinalizer)
		if err := r.Update(ctx, agent); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// cleanupAgentResources cleans up ConfigMaps and PVCs created for the agent.
func (r *AIAgentReconciler) cleanupAgentResources(ctx context.Context, agent *v1.AIAgent) error {
	log := log.FromContext(ctx)

	// Delete Agent ConfigMap
	agentCMName := AgentConfigMapPrefix + agent.Name
	agentCM := &corev1.ConfigMap{}
	agentCM.Namespace = agent.Namespace
	agentCM.Name = agentCMName
	if err := r.Delete(ctx, agentCM); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "failed to delete agent ConfigMap", "name", agentCMName)
		return err
	}

	// Delete PVC if VolumePolicy is delete
	if agent.Spec.VolumePolicy == v1.VolumePolicyDelete {
		pvcName := AgentPVCPrefix + agent.Name
		pvc := &corev1.PersistentVolumeClaim{}
		pvc.Namespace = agent.Namespace
		pvc.Name = pvcName
		if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete agent PVC", "name", pvcName)
			return err
		}
	}

	return nil
}

// handleScheduling schedules the agent to an appropriate AgentRuntime.
func (r *AIAgentReconciler) handleScheduling(ctx context.Context, agent *v1.AIAgent) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Update phase to Scheduling
	agent.Status.Phase = v1.AgentPhaseScheduling
	if err := r.Status().Update(ctx, agent); err != nil {
		return ctrl.Result{}, err
	}

	// If runtime is already specified, skip scheduling
	if agent.Spec.RuntimeRef.Name != "" {
		log.Info("Agent has explicit runtime binding", "runtime", agent.Spec.RuntimeRef.Name)
		return ctrl.Result{Requeue: true}, nil
	}

	// Use scheduler to find matching runtime
	if r.Scheduler == nil {
		r.Scheduler = scheduler.NewDefaultScheduler()
	}

	runtimes := &v1.AgentRuntimeList{}
	if err := r.List(ctx, runtimes); err != nil {
		log.Error(err, "failed to list AgentRuntimes")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Filter by namespace
	var candidates []*v1.AgentRuntime
	for i := range runtimes.Items {
		rt := &runtimes.Items[i]
		// Same namespace or cross-namespace (if allowed)
		if rt.Namespace == agent.Namespace || rt.Status.Phase == v1.RuntimePhaseRunning {
			candidates = append(candidates, rt)
		}
	}

	if len(candidates) == 0 {
		log.Info("No available AgentRuntimes found")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Schedule using scheduler
	targetRuntime, err := r.Scheduler.Schedule(ctx, agent, candidates)
	if err != nil {
		log.Error(err, "scheduling failed")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Update agent spec with scheduled runtime
	agent.Spec.RuntimeRef.Name = targetRuntime.Name
	agent.Spec.RuntimeRef.Type = targetRuntime.Spec.AgentFramework.Type
	if err := r.Update(ctx, agent); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Agent scheduled to runtime", "runtime", targetRuntime.Name)
	return ctrl.Result{Requeue: true}, nil
}

// handleBinding binds the agent to the runtime and creates resources.
func (r *AIAgentReconciler) handleBinding(ctx context.Context, agent *v1.AIAgent) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get the target runtime
	runtimeName := agent.Spec.RuntimeRef.Name
	if runtimeName == "" {
		log.Error(nil, "no runtime specified for binding")
		agent.Status.Phase = v1.AgentPhaseFailed
		r.Status().Update(ctx, agent)
		return ctrl.Result{}, nil
	}

	runtime := &v1.AgentRuntime{}
	if err := r.Get(ctx, types.NamespacedName{Name: runtimeName, Namespace: agent.Namespace}, runtime); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "target runtime not found", "runtime", runtimeName)
			agent.Status.Phase = v1.AgentPhaseFailed
			r.Status().Update(ctx, agent)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Check runtime is ready
	if runtime.Status.Phase != v1.RuntimePhaseRunning {
		log.Info("Runtime not ready, waiting", "runtime", runtimeName, "phase", runtime.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Create Agent ConfigMap
	if err := r.createAgentConfigMap(ctx, agent); err != nil {
		log.Error(err, "failed to create agent ConfigMap")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Create PVC if needed
	if err := r.createAgentPVC(ctx, agent); err != nil {
		log.Error(err, "failed to create agent PVC")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Update Agent Index ConfigMap (notify AgentHandler)
	if err := r.updateAgentIndex(ctx, runtime, agent, "Pending"); err != nil {
		log.Error(err, "failed to update agent index")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Bind to runtime status
	if err := r.bindToRuntime(ctx, runtime, agent); err != nil {
		log.Error(err, "failed to bind to runtime")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Update agent status
	agent.Status.Phase = v1.AgentPhaseRunning
	agent.Status.RuntimeRef = v1.RuntimeReferenceStatus{
		Name: runtime.Name,
		UID:  string(runtime.UID),
	}
	agent.Status.AgentID = agent.Name // Use name as AgentID
	if err := r.Status().Update(ctx, agent); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Agent bound to runtime", "runtime", runtimeName)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleRunning maintains the running agent.
func (r *AIAgentReconciler) handleRunning(ctx context.Context, agent *v1.AIAgent) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Check runtime status
	runtime := &v1.AgentRuntime{}
	runtimeName := agent.Status.RuntimeRef.Name
	if runtimeName == "" {
		log.Error(nil, "no runtime bound")
		agent.Status.Phase = v1.AgentPhaseFailed
		r.Status().Update(ctx, agent)
		return ctrl.Result{}, nil
	}

	if err := r.Get(ctx, types.NamespacedName{Name: runtimeName, Namespace: agent.Namespace}, runtime); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Runtime deleted, need migration")
			agent.Status.Phase = v1.AgentPhaseMigrating
			agent.Spec.RuntimeRef.Name = "" // Clear binding
			r.Update(ctx, agent)
			r.Status().Update(ctx, agent)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Check runtime phase
	if runtime.Status.Phase == v1.RuntimePhaseFailed || runtime.Status.Phase == v1.RuntimePhaseTerminating {
		log.Info("Runtime unhealthy, triggering migration")
		agent.Status.Phase = v1.AgentPhaseMigrating
		r.Status().Update(ctx, agent)
		return ctrl.Result{Requeue: true}, nil
	}

	// Update agent index phase to Running
	if err := r.updateAgentIndex(ctx, runtime, agent, "Running"); err != nil {
		log.Error(err, "failed to update agent index phase")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// handleMigration handles agent migration between runtimes.
func (r *AIAgentReconciler) handleMigration(ctx context.Context, agent *v1.AIAgent) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Find new runtime
	if r.Scheduler == nil {
		r.Scheduler = scheduler.NewDefaultScheduler()
	}
	runtimes := &v1.AgentRuntimeList{}
	r.List(ctx, runtimes)

	var candidates []*v1.AgentRuntime
	for i := range runtimes.Items {
		rt := &runtimes.Items[i]
		if rt.Namespace == agent.Namespace && rt.Status.Phase == v1.RuntimePhaseRunning {
			candidates = append(candidates, rt)
		}
	}

	if len(candidates) == 0 {
		log.Info("No available runtimes for migration")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Schedule to new runtime
	targetRuntime, err := r.Scheduler.Schedule(ctx, agent, candidates)
	if err != nil {
		log.Error(err, "migration scheduling failed")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Unbind from old runtime (if any)
	if agent.Status.RuntimeRef.Name != "" {
		oldRuntime := &v1.AgentRuntime{}
		if err := r.Get(ctx, types.NamespacedName{Name: agent.Status.RuntimeRef.Name, Namespace: agent.Namespace}, oldRuntime); err == nil {
			r.unbindFromRuntimeStatus(ctx, oldRuntime, agent)
		}
	}

	// Update agent index on new runtime
	if err := r.updateAgentIndex(ctx, targetRuntime, agent, "Pending"); err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Bind to new runtime
	if err := r.bindToRuntime(ctx, targetRuntime, agent); err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Update agent status
	agent.Status.Phase = v1.AgentPhaseRunning
	agent.Status.RuntimeRef = v1.RuntimeReferenceStatus{
		Name: targetRuntime.Name,
		UID:  string(targetRuntime.UID),
	}
	agent.Spec.RuntimeRef.Name = targetRuntime.Name
	r.Update(ctx, agent)
	r.Status().Update(ctx, agent)

	log.Info("Agent migrated to new runtime", "runtime", targetRuntime.Name)
	return ctrl.Result{Requeue: true}, nil
}

// createAgentConfigMap creates the agent-specific ConfigMap.
func (r *AIAgentReconciler) createAgentConfigMap(ctx context.Context, agent *v1.AIAgent) error {
	cmName := AgentConfigMapPrefix + agent.Name
	cm := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      cmName,
			Namespace: agent.Namespace,
			Labels: map[string]string{
				"agent.ai/agent":     agent.Name,
				"agent.ai/component": "agent-config",
			},
		},
		Data: map[string]string{
			"agent.yaml": r.generateAgentConfigYAML(agent),
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(agent, cm, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingCM := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: agent.Namespace}, existingCM); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, cm)
		}
		return err
	}

	existingCM.Data = cm.Data
	return r.Update(ctx, existingCM)
}

// generateAgentConfigYAML generates YAML config from AIAgent spec.
func (r *AIAgentReconciler) generateAgentConfigYAML(agent *v1.AIAgent) string {
	// Build config as map for proper YAML serialization
	config := map[string]interface{}{
		"name":         agent.Name,
		"description":  agent.Spec.Description,
		"runtimeRef": map[string]string{
			"type": agent.Spec.RuntimeRef.Type,
			"name": agent.Spec.RuntimeRef.Name,
		},
		"volumePolicy": string(agent.Spec.VolumePolicy),
	}

	// Add AgentConfig if present (this is the key field for framework-specific config)
	if agent.Spec.AgentConfig != nil && len(agent.Spec.AgentConfig.Raw) > 0 {
		var agentConfigData map[string]interface{}
		if err := json.Unmarshal(agent.Spec.AgentConfig.Raw, &agentConfigData); err == nil {
			config["agentConfig"] = agentConfigData
		}
	}

	// Add HarnessOverride if present
	if len(agent.Spec.HarnessOverride.MCP) > 0 ||
		len(agent.Spec.HarnessOverride.Memory) > 0 ||
		len(agent.Spec.HarnessOverride.Sandbox) > 0 ||
		len(agent.Spec.HarnessOverride.Skills) > 0 ||
		len(agent.Spec.HarnessOverride.Model) > 0 {
		harnessOverride := map[string]interface{}{}

		if len(agent.Spec.HarnessOverride.MCP) > 0 {
			mcpOverrides := []map[string]interface{}{}
			for _, o := range agent.Spec.HarnessOverride.MCP {
				mcpOverrides = append(mcpOverrides, map[string]interface{}{
					"name":           o.Name,
					"allowedServers": o.AllowedServers,
					"deniedServers":  o.DeniedServers,
					"deny":           o.Deny,
				})
			}
			harnessOverride["mcp"] = mcpOverrides
		}

		if len(agent.Spec.HarnessOverride.Memory) > 0 {
			memoryOverrides := []map[string]interface{}{}
			for _, o := range agent.Spec.HarnessOverride.Memory {
				entry := map[string]interface{}{"name": o.Name}
				if o.Config != nil && len(o.Config.Raw) > 0 {
					var configData map[string]interface{}
					if err := json.Unmarshal(o.Config.Raw, &configData); err == nil {
						entry["config"] = configData
					}
				}
				memoryOverrides = append(memoryOverrides, entry)
			}
			harnessOverride["memory"] = memoryOverrides
		}

		if len(agent.Spec.HarnessOverride.Sandbox) > 0 {
			sandboxOverrides := []map[string]interface{}{}
			for _, o := range agent.Spec.HarnessOverride.Sandbox {
				sandboxOverrides = append(sandboxOverrides, map[string]interface{}{
					"name": o.Name,
					"deny": o.Deny,
				})
			}
			harnessOverride["sandbox"] = sandboxOverrides
		}

		if len(agent.Spec.HarnessOverride.Skills) > 0 {
			skillsOverrides := []map[string]interface{}{}
			for _, o := range agent.Spec.HarnessOverride.Skills {
				skillsOverrides = append(skillsOverrides, map[string]interface{}{
					"name":          o.Name,
					"allowedSkills": o.AllowedSkills,
					"deniedSkills":  o.DeniedSkills,
				})
			}
			harnessOverride["skills"] = skillsOverrides
		}

		if len(agent.Spec.HarnessOverride.Model) > 0 {
			modelOverrides := []map[string]interface{}{}
			for _, o := range agent.Spec.HarnessOverride.Model {
				modelOverrides = append(modelOverrides, map[string]interface{}{
					"name":           o.Name,
					"allowedModels":  o.AllowedModels,
					"deniedModels":   o.DeniedModels,
				})
			}
			harnessOverride["model"] = modelOverrides
		}

		config["harnessOverride"] = harnessOverride
	}

	// Convert to YAML
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		// Fallback to simple format
		return fmt.Sprintf("name: %s\ndescription: %s\n", agent.Name, agent.Spec.Description)
	}
	return string(yamlData)
}

// createAgentPVC creates the agent's PVC if needed.
func (r *AIAgentReconciler) createAgentPVC(ctx context.Context, agent *v1.AIAgent) error {
	// Only create PVC if volumePolicy is retain
	if agent.Spec.VolumePolicy != v1.VolumePolicyRetain {
		return nil
	}

	pvcName := AgentPVCPrefix + agent.Name
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      pvcName,
			Namespace: agent.Namespace,
			Labels: map[string]string{
				"agent.ai/agent":     agent.Name,
				"agent.ai/component": "agent-storage",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(agent, pvc, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingPVC := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: agent.Namespace}, existingPVC); err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, pvc)
		}
		return err
	}

	return nil
}

// updateAgentIndex updates the Agent Index ConfigMap on the runtime.
// This is the notification mechanism for AgentHandler.
func (r *AIAgentReconciler) updateAgentIndex(ctx context.Context, runtime *v1.AgentRuntime, agent *v1.AIAgent, phase string) error {
	log := log.FromContext(ctx)

	indexCMName := AgentIndexConfigMapPrefix + runtime.Name

	// Build agent index entry
	entry := AgentIndexEntry{
		Name:      agent.Name,
		Namespace: agent.Namespace,
		ConfigMap: AgentConfigMapPrefix + agent.Name,
		Phase:     phase,
		UID:       string(agent.UID),
	}

	// Get or create index ConfigMap
	indexCM := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: indexCMName, Namespace: runtime.Namespace}, indexCM); err != nil {
		if errors.IsNotFound(err) {
			// Create new index ConfigMap
			indexCM = &corev1.ConfigMap{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      indexCMName,
					Namespace: runtime.Namespace,
					Labels: map[string]string{
						"agent.ai/runtime":   runtime.Name,
						"agent.ai/component": "agent-index",
					},
				},
				Data: map[string]string{
					"agent-index.yaml": r.generateAgentIndexYAML([]AgentIndexEntry{entry}),
				},
			}
			// Set owner reference to runtime
			if err := controllerutil.SetControllerReference(runtime, indexCM, r.Scheme); err != nil {
				return err
			}
			log.Info("Creating agent index ConfigMap", "name", indexCMName)
			return r.Create(ctx, indexCM)
		}
		return err
	}

	// Update existing index
	var index AgentIndex
	if err := r.parseAgentIndexYAML(indexCM.Data["agent-index.yaml"], &index); err != nil {
		// If parse fails, start fresh
		index = AgentIndex{Agents: []AgentIndexEntry{}}
	}

	// Find and update entry, or add new entry
	found := false
	for i, e := range index.Agents {
		if e.Name == agent.Name && e.Namespace == agent.Namespace {
			index.Agents[i] = entry
			found = true
			break
		}
	}
	if !found {
		index.Agents = append(index.Agents, entry)
	}

	indexCM.Data["agent-index.yaml"] = r.generateAgentIndexYAML(index.Agents)
	log.Info("Updating agent index ConfigMap", "name", indexCMName, "agent", agent.Name, "phase", phase)
	return r.Update(ctx, indexCM)
}

// AgentIndex represents the agent index structure.
type AgentIndex struct {
	Agents []AgentIndexEntry `yaml:"agents"`
}

// AgentIndexEntry represents an entry in the agent index.
type AgentIndexEntry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	ConfigMap string `yaml:"configMap"`
	Phase     string `yaml:"phase"`
	UID       string `yaml:"uid,omitempty"`
}

// generateAgentIndexYAML generates YAML for agent index.
func (r *AIAgentReconciler) generateAgentIndexYAML(entries []AgentIndexEntry) string {
	result := "agents:\n"
	for _, e := range entries {
		result += fmt.Sprintf("  - name: %s\n    namespace: %s\n    configMap: %s\n    phase: %s\n    uid: %s\n",
			e.Name, e.Namespace, e.ConfigMap, e.Phase, e.UID)
	}
	return result
}

// parseAgentIndexYAML parses agent index YAML.
func (r *AIAgentReconciler) parseAgentIndexYAML(yamlStr string, index *AgentIndex) error {
	if yamlStr == "" {
		*index = AgentIndex{Agents: []AgentIndexEntry{}}
		return nil
	}

	if err := yaml.Unmarshal([]byte(yamlStr), index); err != nil {
		return fmt.Errorf("failed to parse agent index YAML: %w", err)
	}

	// Ensure Agents slice is not nil
	if index.Agents == nil {
		index.Agents = []AgentIndexEntry{}
	}

	return nil
}

// bindToRuntime binds the agent to the runtime's status.
func (r *AIAgentReconciler) bindToRuntime(ctx context.Context, runtime *v1.AgentRuntime, agent *v1.AIAgent) error {
	// Check if already bound
	for _, binding := range runtime.Status.Agents {
		if binding.Name == agent.Name && binding.Namespace == agent.Namespace {
			return nil // Already bound
		}
	}

	// Add binding
	runtime.Status.Agents = append(runtime.Status.Agents, v1.AgentBindingStatus{
		Name:      agent.Name,
		Namespace: agent.Namespace,
		UID:       string(agent.UID),
		Phase:     agent.Status.Phase,
		BoundAt:   metav1Now(),
	})
	runtime.Status.AgentCount = int32(len(runtime.Status.Agents))

	return r.Status().Update(ctx, runtime)
}

// unbindFromRuntime removes the agent from runtime status and updates index.
func (r *AIAgentReconciler) unbindFromRuntime(ctx context.Context, agent *v1.AIAgent) error {
	if agent.Status.RuntimeRef.Name == "" {
		return nil
	}

	runtime := &v1.AgentRuntime{}
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Status.RuntimeRef.Name, Namespace: agent.Namespace}, runtime); err != nil {
		if errors.IsNotFound(err) {
			return nil // Runtime already deleted
		}
		return err
	}

	return r.unbindFromRuntimeStatus(ctx, runtime, agent)
}

// unbindFromRuntimeStatus removes agent from runtime status.
func (r *AIAgentReconciler) unbindFromRuntimeStatus(ctx context.Context, runtime *v1.AgentRuntime, agent *v1.AIAgent) error {
	// Remove from Agents list
	newAgents := []v1.AgentBindingStatus{}
	for _, binding := range runtime.Status.Agents {
		if binding.Name != agent.Name || binding.Namespace != agent.Namespace {
			newAgents = append(newAgents, binding)
		}
	}
	runtime.Status.Agents = newAgents
	runtime.Status.AgentCount = int32(len(newAgents))

	// Remove from agent index
	indexCMName := AgentIndexConfigMapPrefix + runtime.Name
	indexCM := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: indexCMName, Namespace: runtime.Namespace}, indexCM); err == nil {
		var index AgentIndex
		r.parseAgentIndexYAML(indexCM.Data["agent-index.yaml"], &index)
		newEntries := []AgentIndexEntry{}
		for _, e := range index.Agents {
			if e.Name != agent.Name || e.Namespace != agent.Namespace {
				newEntries = append(newEntries, e)
			}
		}
		indexCM.Data["agent-index.yaml"] = r.generateAgentIndexYAML(newEntries)
		r.Update(ctx, indexCM)
	}

	return r.Status().Update(ctx, runtime)
}

// metav1Now returns current time as metav1.Time.
func metav1Now() metav1.Time {
	return metav1.Now()
}