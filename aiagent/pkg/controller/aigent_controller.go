// Package controller provides AIAgent Controller for managing AI Agent business objects.
// AIAgent Controller handles:
// - Agent scheduling to AgentRuntime
// - PVC lifecycle management
// - Agent migration support
// Note: AgentConfig and AgentIndex are managed by Config Daemon via hostPath.
package controller

import (
	"context"
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

	"aiagent/api/v1"
	"aiagent/pkg/scheduler"
)

const (
	// AIAgentFinalizer is used for cleanup on deletion.
	AIAgentFinalizer = "agent.ai/aigent-finalizer"

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

// cleanupAgentResources cleans up PVCs created for the agent.
// Note: ConfigMaps are managed by Config Daemon, no need to cleanup here.
func (r *AIAgentReconciler) cleanupAgentResources(ctx context.Context, agent *v1.AIAgent) error {
	log := log.FromContext(ctx)

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
// Note: AgentConfig and AgentIndex are now managed by Config Daemon via hostPath.
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

	// Create PVC if needed
	if err := r.createAgentPVC(ctx, agent); err != nil {
		log.Error(err, "failed to create agent PVC")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Bind to runtime status (Config Daemon will pick up the binding and write to hostPath)
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
// Note: AgentIndex updates are handled by Config Daemon via hostPath.
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

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// handleMigration handles agent migration between runtimes.
// Note: Config Daemon will detect the runtimeRef change and update hostPath accordingly.
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

	// Bind to new runtime (Config Daemon will detect and update hostPath)
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

// unbindFromRuntime removes the agent from runtime status.
// Note: Config Daemon will detect the removal and cleanup hostPath.
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

	return r.Status().Update(ctx, runtime)
}

// metav1Now returns current time as metav1.Time.
func metav1Now() metav1.Time {
	return metav1.Now()
}