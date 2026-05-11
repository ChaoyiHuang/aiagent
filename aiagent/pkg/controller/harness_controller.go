// Package controller provides Harness Controller for managing Harness CRDs.
// Harness Controller handles:
// - Harness ConfigMap generation
// - Harness status updates
// - Connection health checks
package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"aiagent/api/v1"
)

const (
	// HarnessFinalizer is used for cleanup on deletion.
	HarnessFinalizer = "agent.ai/harness-finalizer"

	// HarnessConfigSuffix is added to Harness name for ConfigMap.
	// Note: different from AgentRuntime's HarnessConfigMapSuffix
	HarnessConfigSuffix = "-harness-config"
)

// HarnessReconciler reconciles a Harness object.
type HarnessReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *HarnessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Harness{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=agent.ai,resources=harnesses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=agent.ai,resources=harnesses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=agent.ai,resources=harnesses/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles the reconciliation loop for Harness.
func (r *HarnessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling Harness", "name", req.Name, "namespace", req.Namespace)

	// Fetch the Harness
	harness := &v1.Harness{}
	if err := r.Get(ctx, req.NamespacedName, harness); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Harness not found, already deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !harness.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, harness)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(harness, HarnessFinalizer) {
		controllerutil.AddFinalizer(harness, HarnessFinalizer)
		if err := r.Update(ctx, harness); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Generate Harness ConfigMap
	if err := r.createHarnessConfigMap(ctx, harness); err != nil {
		log.Error(err, "failed to create harness ConfigMap")
		r.updateStatus(ctx, harness, v1.HarnessPhaseError, err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Check connection health
	healthy, err := r.checkHarnessHealth(ctx, harness)
	if err != nil {
		log.Error(err, "health check failed")
	}

	// Update status
	if healthy {
		r.updateStatus(ctx, harness, v1.HarnessPhaseAvailable, "")
	} else {
		r.updateStatus(ctx, harness, v1.HarnessPhaseUnavailable, "connection failed")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// handleDeletion handles the Harness deletion process.
func (r *HarnessReconciler) handleDeletion(ctx context.Context, harness *v1.Harness) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(harness, HarnessFinalizer) {
		// Delete associated ConfigMap
		cmName := harness.Name + HarnessConfigSuffix
		cm := &corev1.ConfigMap{}
		cm.Namespace = harness.Namespace
		cm.Name = cmName
		if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete harness ConfigMap", "name", cmName)
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(harness, HarnessFinalizer)
		if err := r.Update(ctx, harness); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// createHarnessConfigMap creates or updates the Harness ConfigMap.
func (r *HarnessReconciler) createHarnessConfigMap(ctx context.Context, harness *v1.Harness) error {
	log := log.FromContext(ctx)

	cmName := harness.Name + HarnessConfigSuffix
	cm := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      cmName,
			Namespace: harness.Namespace,
			Labels: map[string]string{
				"agent.ai/harness":    harness.Name,
				"agent.ai/harness-type": string(harness.Spec.Type),
				"agent.ai/component":  "harness-config",
			},
		},
		Data: r.generateHarnessConfigData(harness),
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(harness, cm, r.Scheme); err != nil {
		return err
	}

	// Create or update
	existingCM := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: harness.Namespace}, existingCM); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating Harness ConfigMap", "name", cmName)
			return r.Create(ctx, cm)
		}
		return err
	}

	// Update if changed
	existingCM.Data = cm.Data
	existingCM.Labels = cm.Labels
	log.Info("Updating Harness ConfigMap", "name", cmName)
	return r.Update(ctx, existingCM)
}

// generateHarnessConfigData generates ConfigMap data from Harness spec.
func (r *HarnessReconciler) generateHarnessConfigData(harness *v1.Harness) map[string]string {
	data := map[string]string{
		"harness-name": harness.Name,
		"harness-type": string(harness.Spec.Type),
	}

	// Generate type-specific config
	switch harness.Spec.Type {
	case v1.HarnessTypeModel:
		if harness.Spec.Model != nil {
			data["model.yaml"] = r.generateModelConfig(harness.Spec.Model)
		}
	case v1.HarnessTypeMCP:
		if harness.Spec.MCP != nil {
			data["mcp.yaml"] = r.generateMCPConfig(harness.Spec.MCP)
		}
	case v1.HarnessTypeMemory:
		if harness.Spec.Memory != nil {
			data["memory.yaml"] = r.generateMemoryConfig(harness.Spec.Memory)
		}
	case v1.HarnessTypeSandbox:
		if harness.Spec.Sandbox != nil {
			data["sandbox.yaml"] = r.generateSandboxConfig(harness.Spec.Sandbox)
		}
	case v1.HarnessTypeSkills:
		if harness.Spec.Skills != nil {
			data["skills.yaml"] = r.generateSkillsConfig(harness.Spec.Skills)
		}
	case v1.HarnessTypeKnowledge:
		if harness.Spec.Knowledge != nil {
			data["knowledge.yaml"] = r.generateKnowledgeConfig(harness.Spec.Knowledge)
		}
	case v1.HarnessTypeState:
		if harness.Spec.State != nil {
			data["state.yaml"] = r.generateStateConfig(harness.Spec.State)
		}
	case v1.HarnessTypeGuardrail:
		if harness.Spec.Guardrail != nil {
			data["guardrail.yaml"] = r.generateGuardrailConfig(harness.Spec.Guardrail)
		}
	case v1.HarnessTypeSecurity:
		if harness.Spec.Security != nil {
			data["security.yaml"] = r.generateSecurityConfig(harness.Spec.Security)
		}
	case v1.HarnessTypePolicy:
		if harness.Spec.Policy != nil {
			data["policy.yaml"] = r.generatePolicyConfig(harness.Spec.Policy)
		}
	}

	return data
}

// generateModelConfig generates YAML config for Model Harness.
func (r *HarnessReconciler) generateModelConfig(spec *v1.ModelHarnessSpec) string {
	config := fmt.Sprintf(`
provider: %s
endpoint: %s
defaultModel: %s
models:
`, spec.Provider, spec.Endpoint, spec.DefaultModel)

	for _, m := range spec.Models {
		config += fmt.Sprintf("  - name: %s\n    allowed: %v\n", m.Name, m.Allowed)
	}

	return config
}

// generateMCPConfig generates YAML config for MCP Harness.
func (r *HarnessReconciler) generateMCPConfig(spec *v1.MCPHarnessSpec) string {
	config := fmt.Sprintf(`
registryType: %s
endpoint: %s
discoveryEnabled: %v
servers:
`, spec.RegistryType, spec.Endpoint, spec.DiscoveryEnabled)

	for _, s := range spec.Servers {
		config += fmt.Sprintf("  - name: %s\n    type: %s\n    allowed: %v\n", s.Name, s.Type, s.Allowed)
	}

	return config
}

// generateMemoryConfig generates YAML config for Memory Harness.
func (r *HarnessReconciler) generateMemoryConfig(spec *v1.MemoryHarnessSpec) string {
	return fmt.Sprintf(`
type: %s
endpoint: %s
ttl: %d
maxSize: %s
persistenceEnabled: %v
`, spec.Type, spec.Endpoint, spec.TTL, spec.MaxSize, spec.PersistenceEnabled)
}

// generateSandboxConfig generates YAML config for Sandbox Harness.
func (r *HarnessReconciler) generateSandboxConfig(spec *v1.SandboxHarnessSpec) string {
	config := fmt.Sprintf(`
type: %s
mode: %s
endpoint: %s
apiKey: %s
authSecretRef: %s
templateRef: %s
warmPoolRef: %s
timeout: %d
`, spec.Type, string(spec.Mode), spec.Endpoint, spec.APIKey, spec.AuthSecretRef,
		spec.TemplateRef, spec.WarmPoolRef, spec.Timeout)

	if spec.ResourceLimits != nil {
		config += fmt.Sprintf(`
resourceLimits:
  cpu: %s
  memory: %s
  pids: %d
`, spec.ResourceLimits.CPU, spec.ResourceLimits.Memory, spec.ResourceLimits.PIDs)
	}

	if spec.NetworkPolicy != nil {
		config += fmt.Sprintf(`
networkPolicy:
  allowOutbound: %v
  allowInbound: %v
`, spec.NetworkPolicy.AllowOutbound, spec.NetworkPolicy.AllowInbound)
	}

	return config
}

// generateSkillsConfig generates YAML config for Skills Harness.
func (r *HarnessReconciler) generateSkillsConfig(spec *v1.SkillsHarnessSpec) string {
	config := fmt.Sprintf(`
hubType: %s
endpoint: %s
localPath: %s
autoUpdate: %v
skills:
`, spec.HubType, spec.Endpoint, spec.LocalPath, spec.AutoUpdate)

	for _, s := range spec.Skills {
		config += fmt.Sprintf("  - name: %s\n    version: %s\n    allowed: %v\n", s.Name, s.Version, s.Allowed)
	}

	return config
}

// generateKnowledgeConfig generates YAML config for Knowledge Harness.
func (r *HarnessReconciler) generateKnowledgeConfig(spec *v1.KnowledgeHarnessSpec) string {
	config := fmt.Sprintf(`
type: %s
endpoint: %s
embeddingModel: %s
collections:
`, spec.Type, spec.Endpoint, spec.EmbeddingModel)

	for _, c := range spec.Collections {
		config += fmt.Sprintf("  - %s\n", c)
	}

	return config
}

// generateStateConfig generates YAML config for State Harness.
func (r *HarnessReconciler) generateStateConfig(spec *v1.StateHarnessSpec) string {
	return fmt.Sprintf(`
type: %s
endpoint: %s
sessionTTL: %d
`, spec.Type, spec.Endpoint, spec.SessionTTL)
}

// generateGuardrailConfig generates YAML config for Guardrail Harness.
func (r *HarnessReconciler) generateGuardrailConfig(spec *v1.GuardrailHarnessSpec) string {
	config := fmt.Sprintf(`
type: %s
endpoint: %s
enabled: %v
rules:
`, spec.Type, spec.Endpoint, spec.Enabled)

	for _, rule := range spec.Rules {
		config += fmt.Sprintf("  - name: %s\n    type: %s\n    severity: %s\n    action: %s\n",
			rule.Name, rule.Type, rule.Severity, rule.Action)
	}

	return config
}

// generateSecurityConfig generates YAML config for Security Harness.
func (r *HarnessReconciler) generateSecurityConfig(spec *v1.SecurityHarnessSpec) string {
	config := fmt.Sprintf(`
auditEnabled: %v
auditLogPath: %s
policies:
`, spec.AuditEnabled, spec.AuditLogPath)

	for _, p := range spec.Policies {
		config += fmt.Sprintf("  - name: %s\n    type: %s\n", p.Name, p.Type)
	}

	return config
}

// generatePolicyConfig generates YAML config for Policy Harness.
func (r *HarnessReconciler) generatePolicyConfig(spec *v1.PolicyHarnessSpec) string {
	config := fmt.Sprintf(`
defaultAction: %s
rules:
`, spec.DefaultAction)

	for _, rule := range spec.Rules {
		config += fmt.Sprintf("  - name: %s\n    condition: %s\n    action: %s\n",
			rule.Name, rule.Condition, rule.Action)
	}

	return config
}

// checkHarnessHealth checks the connection health of the harness.
// For External Sandbox, this would call the health endpoint.
func (r *HarnessReconciler) checkHarnessHealth(ctx context.Context, harness *v1.Harness) (bool, error) {
	// Basic health check based on harness type
	// In production, would make actual connection checks

	switch harness.Spec.Type {
	case v1.HarnessTypeSandbox:
		if harness.Spec.Sandbox != nil {
			// For External Sandbox, would check endpoint health
			// For now, return true if endpoint is set
			if harness.Spec.Sandbox.Mode == v1.SandboxModeExternal {
				return harness.Spec.Sandbox.Endpoint != "", nil
			}
			return true, nil
		}
	case v1.HarnessTypeModel:
		if harness.Spec.Model != nil {
			// For Model, check if provider/endpoint configured
			return harness.Spec.Model.Provider != "" && harness.Spec.Model.DefaultModel != "", nil
		}
	case v1.HarnessTypeMemory:
		if harness.Spec.Memory != nil {
			// For Memory, check type configured
			return harness.Spec.Memory.Type != "", nil
		}
	}

	// Default: harness exists, considered healthy
	return true, nil
}

// updateStatus updates the Harness status.
func (r *HarnessReconciler) updateStatus(ctx context.Context, harness *v1.Harness, phase v1.HarnessPhase, errMsg string) {
	log := log.FromContext(ctx)

	if harness.Status.Phase != phase {
		harness.Status.Phase = phase
		harness.Status.LastSyncTime = metav1.Now()

		if errMsg != "" {
			harness.Status.ConnectionStatus = "error: " + errMsg
		} else {
			harness.Status.ConnectionStatus = "connected"
		}

		log.Info("Updating Harness status", "phase", phase, "connection", harness.Status.ConnectionStatus)
		if err := r.Status().Update(ctx, harness); err != nil {
			log.Error(err, "failed to update harness status")
		}
	}
}