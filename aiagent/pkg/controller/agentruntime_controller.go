// Package controller provides Kubernetes controllers for AI Agent resources.
// AgentRuntime Controller manages the lifecycle of AgentRuntime Pods.
// This controller is FRAMEWORK-AGNOSTIC - it does not know about ADK, OpenClaw, etc.
// All framework-specific configuration comes from the CRD spec.
package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"aiagent/api/v1"
)

const (
	// AgentRuntimeFinalizer is used for cleanup on deletion.
	AgentRuntimeFinalizer = "aiagent.io/agentruntime-finalizer"

	// HarnessConfigMapSuffix is added to Harness name for ConfigMap.
	HarnessConfigMapSuffix = "-harness-config"
)

// AgentRuntimeReconciler reconciles an AgentRuntime object.
// It is completely framework-agnostic - all framework-specific details
// come from the AgentRuntimeSpec (AgentHandlerSpec and AgentFrameworkSpec).
type AgentRuntimeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentRuntimeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.AgentRuntime{}).
		Owns(&corev1.Pod{}).
		Watches(
			&v1.Harness{},
			handler.EnqueueRequestsFromMapFunc(r.harnessToAgentRuntimeMapper),
		).
		Complete(r)
}

// harnessToAgentRuntimeMapper maps Harness changes to AgentRuntime reconciles.
func (r *AgentRuntimeReconciler) harnessToAgentRuntimeMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	harness := obj.(*v1.Harness)
	log := log.FromContext(ctx)

	runtimes := &v1.AgentRuntimeList{}
	if err := r.List(ctx, runtimes); err != nil {
		log.Error(err, "failed to list AgentRuntimes for Harness mapping")
		return nil
	}

	requests := []reconcile.Request{}
	for _, rt := range runtimes.Items {
		for _, ref := range rt.Spec.Harness {
			if ref.Name == harness.Name &&
				(ref.Namespace == harness.Namespace || ref.Namespace == "" && harness.Namespace == rt.Namespace) {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      rt.Name,
						Namespace: rt.Namespace,
					},
				})
				break
			}
		}
	}

	return requests
}

//+kubebuilder:rbac:groups=aiagent.io,resources=agentruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aiagent.io,resources=agentruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=aiagent.io,resources=agentruntimes/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=aiagent.io,resources=harnesses,verbs=get;list;watch

// Reconcile handles the reconciliation loop for AgentRuntime.
func (r *AgentRuntimeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling AgentRuntime", "name", req.Name, "namespace", req.Namespace)

	// Fetch the AgentRuntime
	runtime := &v1.AgentRuntime{}
	if err := r.Get(ctx, req.NamespacedName, runtime); err != nil {
		if errors.IsNotFound(err) {
			log.Info("AgentRuntime not found, already deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !runtime.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, runtime)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(runtime, AgentRuntimeFinalizer) {
		controllerutil.AddFinalizer(runtime, AgentRuntimeFinalizer)
		if err := r.Update(ctx, runtime); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Resolve Harness references and generate ConfigMaps
	if err := r.resolveHarnessReferences(ctx, runtime); err != nil {
		log.Error(err, "failed to resolve harness references")
		r.updateStatus(ctx, runtime, v1.RuntimePhaseFailed, err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Create or update Pod (framework-agnostic)
	pod, err := r.createOrUpdatePod(ctx, runtime)
	if err != nil {
		log.Error(err, "failed to create/update Pod")
		r.updateStatus(ctx, runtime, v1.RuntimePhaseFailed, err.Error())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Update status based on Pod state
	r.updateStatusFromPod(ctx, runtime, pod)

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// handleDeletion handles the AgentRuntime deletion process.
func (r *AgentRuntimeReconciler) handleDeletion(ctx context.Context, runtime *v1.AgentRuntime) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(runtime, AgentRuntimeFinalizer) {
		if err := r.cleanupResources(ctx, runtime); err != nil {
			log.Error(err, "failed to cleanup resources")
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(runtime, AgentRuntimeFinalizer)
		if err := r.Update(ctx, runtime); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// cleanupResources cleans up resources created by the AgentRuntime.
func (r *AgentRuntimeReconciler) cleanupResources(ctx context.Context, runtime *v1.AgentRuntime) error {
	log := log.FromContext(ctx)

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(runtime.Namespace), client.MatchingFields{
		"metadata.ownerReferences": string(runtime.UID),
	}); err != nil {
		return err
	}

	for _, pod := range pods.Items {
		if err := r.Delete(ctx, &pod); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete Pod", "pod", pod.Name)
			return err
		}
	}

	for _, ref := range runtime.Spec.Harness {
		cmName := ref.Name + HarnessConfigMapSuffix
		cm := &corev1.ConfigMap{}
		cm.Namespace = runtime.Namespace
		cm.Name = cmName
		if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "failed to delete ConfigMap", "configmap", cmName)
		}
	}

	return nil
}

// resolveHarnessReferences resolves Harness CRD references and generates ConfigMaps.
func (r *AgentRuntimeReconciler) resolveHarnessReferences(ctx context.Context, runtime *v1.AgentRuntime) error {
	log := log.FromContext(ctx)

	for _, ref := range runtime.Spec.Harness {
		ns := ref.Namespace
		if ns == "" {
			ns = runtime.Namespace
		}

		harness := &v1.Harness{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, harness); err != nil {
			if errors.IsNotFound(err) {
				return fmt.Errorf("harness %s/%s not found", ns, ref.Name)
			}
			return err
		}

		cmName := ref.Name + HarnessConfigMapSuffix
		cm := &corev1.ConfigMap{
			ObjectMeta: ctrl.ObjectMeta{
				Name:      cmName,
				Namespace: runtime.Namespace,
			},
			Data: r.generateHarnessConfigData(harness),
		}

		if err := controllerutil.SetControllerReference(runtime, cm, r.Scheme); err != nil {
			return err
		}

		existingCM := &corev1.ConfigMap{}
		if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: runtime.Namespace}, existingCM); err != nil {
			if errors.IsNotFound(err) {
				log.Info("Creating Harness ConfigMap", "name", cmName)
				if err := r.Create(ctx, cm); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			if !reflect.DeepEqual(existingCM.Data, cm.Data) {
				existingCM.Data = cm.Data
				log.Info("Updating Harness ConfigMap", "name", cmName)
				if err := r.Update(ctx, existingCM); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// generateHarnessConfigData generates ConfigMap data from Harness spec.
func (r *AgentRuntimeReconciler) generateHarnessConfigData(harness *v1.Harness) map[string]string {
	data := map[string]string{}

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
	}

	data["harness-name"] = harness.Name
	data["harness-type"] = string(harness.Spec.Type)

	return data
}

// generateModelConfig generates YAML config for Model Harness.
func (r *AgentRuntimeReconciler) generateModelConfig(spec *v1.ModelHarnessSpec) string {
	return fmt.Sprintf(`
provider: %s
endpoint: %s
defaultModel: %s
models:
%s
`, spec.Provider, spec.Endpoint, spec.DefaultModel, r.generateModelList(spec.Models))
}

func (r *AgentRuntimeReconciler) generateModelList(models []v1.ModelConfig) string {
	result := ""
	for _, m := range models {
		result += fmt.Sprintf("  - name: %s\n    allowed: %v\n", m.Name, m.Allowed)
	}
	return result
}

func (r *AgentRuntimeReconciler) generateMCPConfig(spec *v1.MCPHarnessSpec) string {
	return fmt.Sprintf(`
registryType: %s
endpoint: %s
servers:
%s
`, spec.RegistryType, spec.Endpoint, r.generateServerList(spec.Servers))
}

func (r *AgentRuntimeReconciler) generateServerList(servers []v1.MCPServerConfig) string {
	result := ""
	for _, s := range servers {
		result += fmt.Sprintf("  - name: %s\n    type: %s\n    allowed: %v\n", s.Name, s.Type, s.Allowed)
	}
	return result
}

func (r *AgentRuntimeReconciler) generateMemoryConfig(spec *v1.MemoryHarnessSpec) string {
	return fmt.Sprintf(`
type: %s
endpoint: %s
ttl: %d
persistenceEnabled: %v
`, spec.Type, spec.Endpoint, spec.TTL, spec.PersistenceEnabled)
}

func (r *AgentRuntimeReconciler) generateSandboxConfig(spec *v1.SandboxHarnessSpec) string {
	return fmt.Sprintf(`
type: %s
mode: %s
endpoint: %s
timeout: %d
`, spec.Type, string(spec.Mode), spec.Endpoint, spec.Timeout)
}

func (r *AgentRuntimeReconciler) generateSkillsConfig(spec *v1.SkillsHarnessSpec) string {
	return fmt.Sprintf(`
hubType: %s
endpoint: %s
skills:
%s
`, spec.HubType, spec.Endpoint, r.generateSkillList(spec.Skills))
}

func (r *AgentRuntimeReconciler) generateSkillList(skills []v1.SkillConfig) string {
	result := ""
	for _, s := range skills {
		result += fmt.Sprintf("  - name: %s\n    version: %s\n    allowed: %v\n", s.Name, s.Version, s.Allowed)
	}
	return result
}

// createOrUpdatePod creates or updates the AgentRuntime Pod.
func (r *AgentRuntimeReconciler) createOrUpdatePod(ctx context.Context, runtime *v1.AgentRuntime) (*corev1.Pod, error) {
	log := log.FromContext(ctx)

	podName := runtime.Name + "-runtime"
	pod := &corev1.Pod{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      podName,
			Namespace: runtime.Namespace,
			Labels: map[string]string{
				"aiagent.io/runtime":      runtime.Name,
				"aiagent.io/type":         "agent-runtime",
				"aiagent.io/framework":    runtime.Spec.AgentFramework.Type,
			},
		},
		Spec: r.buildPodSpec(runtime),
	}

	if err := controllerutil.SetControllerReference(runtime, pod, r.Scheme); err != nil {
		return nil, err
	}

	existingPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: runtime.Namespace}, existingPod); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating AgentRuntime Pod", "name", podName, "framework", runtime.Spec.AgentFramework.Type)
			if err := r.Create(ctx, pod); err != nil {
				return nil, err
			}
			return pod, nil
		}
		return nil, err
	}

	if !reflect.DeepEqual(existingPod.Spec, pod.Spec) {
		log.Info("Updating AgentRuntime Pod", "name", podName)
		existingPod.Spec = pod.Spec
		if err := r.Update(ctx, existingPod); err != nil {
			return nil, err
		}
	}

	return existingPod, nil
}

// buildPodSpec builds the Pod specification from AgentRuntimeSpec.
// This function is FRAMEWORK-AGNOSTIC - all configuration comes from the spec.
// No hardcoded knowledge of ADK, OpenClaw, or any other framework.
//
// Architecture (ImageVolume pattern):
// ┌─────────────────────────────────────────────────────────────────┐
// │ Pod (AgentRuntime)                                              │
// │                                                                 │
// │  Handler Container (process manager)                            │
// │  ┌────────────────────────────────────────────────────────────┐│
// │  │ - Starts Framework processes via exec.Command             ││
// │  │ - Uses /framework-rootfs/<binary-path> for Framework exe   ││
// │  │ - Controls process lifecycle (start/stop/monitor)          ││
// │  │                                                            ││
// │  │ VolumeMounts:                                              ││
// │  │   /framework-rootfs -> ImageVolume (Framework image)       ││
// │  │   /etc/harness/<name> -> Harness ConfigMaps                ││
// │  │   /shared/workdir -> EmptyDir (agent workspace)            ││
// │  │   /shared/config -> EmptyDir (runtime configs)             ││
// │  │   /etc/agent-config -> EmptyDir (agent configs)            ││
// │  └────────────────────────────────────────────────────────────┘│
// │                                                                 │
// │  Framework Container (dummy - provides image content only)     │
// │  ┌────────────────────────────────────────────────────────────┐│
// │  │ - ENTRYPOINT: pause (just sleeps, no active process)       ││
// │  │ - Provides image content for ImageVolume                   ││
// │  │ - Does NOT run Framework processes                         ││
// │  │ - Handler manages Framework processes                      ││
// │  └────────────────────────────────────────────────────────────┘│
// │                                                                 │
// │  ShareProcessNamespace: true (Handler can see/ctrl Framework) │
// │  ShareNetworkNamespace: true (implicit in Pod)                 │
// └─────────────────────────────────────────────────────────────────┘
func (r *AgentRuntimeReconciler) buildPodSpec(runtime *v1.AgentRuntime) corev1.PodSpec {
	// Build volumes for Harness ConfigMaps
	volumes := []corev1.Volume{}
	handlerVolumeMounts := []corev1.VolumeMount{}

	for _, ref := range runtime.Spec.Harness {
		cmName := ref.Name + HarnessConfigMapSuffix
		volumes = append(volumes, corev1.Volume{
			Name: ref.Name + "-harness",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cmName,
					},
				},
			},
		})
		handlerVolumeMounts = append(handlerVolumeMounts, corev1.VolumeMount{
			Name:      ref.Name + "-harness",
			MountPath: "/etc/harness/" + ref.Name,
		})
	}

	// Shared EmptyDir volumes for runtime/agent workspace
	sharedVolumes := []corev1.Volume{
		{Name: "shared-workdir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "shared-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "agent-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	volumes = append(volumes, sharedVolumes...)

	// ImageVolume: Mount Framework image content to Handler Container
	// This allows Handler to access Framework's filesystem and start Framework processes
	// K8s 1.36+ ImageVolume format:
	//   volumes:
	//   - name: framework-image
	//     image:
	//       reference: aiagent/framework:test
	//       pullPolicy: IfNotPresent
	frameworkImageVolume := corev1.Volume{
		Name: "framework-image",
		VolumeSource: corev1.VolumeSource{
			Image: &corev1.ImageVolumeSource{
				Reference:  runtime.Spec.AgentFramework.Image,
				PullPolicy: corev1.PullIfNotPresent,
			},
		},
	}
	volumes = append(volumes, frameworkImageVolume)

	// Handler mounts Framework image at /framework-rootfs
	handlerVolumeMounts = append(handlerVolumeMounts,
		corev1.VolumeMount{Name: "framework-image", MountPath: "/framework-rootfs"},
		corev1.VolumeMount{Name: "shared-workdir", MountPath: "/shared/workdir"},
		corev1.VolumeMount{Name: "shared-config", MountPath: "/shared/config"},
		corev1.VolumeMount{Name: "agent-config", MountPath: "/etc/agent-config"},
	)

	// Build Handler container from spec
	handlerContainer := r.buildContainerFromHandlerSpec(
		"agent-handler",
		runtime.Spec.AgentHandler,
		handlerVolumeMounts,
	)

	// Build Framework container as DUMMY container (pause process only)
	// It provides the image content for ImageVolume, but does not run Framework processes
	// Handler Container is the process manager that starts/stops Framework processes
	frameworkContainer := r.buildFrameworkDummyContainer(
		"agent-framework",
		runtime.Spec.AgentFramework,
	)

	return corev1.PodSpec{
		ShareProcessNamespace: boolPtr(true),
		Containers:            []corev1.Container{handlerContainer, frameworkContainer},
		Volumes:               volumes,
		NodeSelector:          runtime.Spec.NodeSelector,
		Affinity:              runtime.Spec.Affinity,
		Tolerations:           runtime.Spec.Tolerations,
		RestartPolicy:         corev1.RestartPolicyAlways,
	}
}

// buildContainerFromHandlerSpec builds a container from HandlerSpec.
func (r *AgentRuntimeReconciler) buildContainerFromHandlerSpec(name string, spec v1.AgentHandlerSpec, volumeMounts []corev1.VolumeMount) corev1.Container {
	return r.buildContainerCommon(name, spec.Image, spec.Command, spec.Args, spec.Env, spec.Resources, volumeMounts)
}

// buildFrameworkDummyContainer builds a DUMMY framework container.
// This container does NOT run Framework processes - it only provides the image content
// for the ImageVolume. The Handler Container is the process manager.
func (r *AgentRuntimeReconciler) buildFrameworkDummyContainer(name string, spec v1.AgentFrameworkSpec) corev1.Container {
	// Use "pause" as the entrypoint - a minimal process that just sleeps forever
	// The container's filesystem content is exposed via ImageVolume to Handler
	// Handler Container starts actual Framework processes using /framework-rootfs/<binary>
	return corev1.Container{
		Name:    name,
		Image:   spec.Image,
		Command: []string{"pause"}, // Minimal process that sleeps forever
		Args:    []string{},
		SecurityContext: &corev1.SecurityContext{
			Privileged:             boolPtr(false),
			RunAsNonRoot:           boolPtr(false), // pause needs root
			ReadOnlyRootFilesystem: boolPtr(false),
		},
		// No volumeMounts - this container is just a dummy for ImageVolume
	}
}

// buildContainerCommon builds a container from common parameters.
func (r *AgentRuntimeReconciler) buildContainerCommon(name string, image string, command []string, args []string, env []v1.EnvVar, resources *v1.ContainerResources, volumeMounts []corev1.VolumeMount) corev1.Container {
	container := corev1.Container{
		Name:         name,
		Image:        image,
		VolumeMounts: volumeMounts,
	}

	if len(command) > 0 {
		container.Command = command
	}

	if len(args) > 0 {
		container.Args = args
	}

	container.Env = r.buildEnvVars(env)

	if resources != nil {
		container.Resources = r.buildResourceRequirements(resources)
	}

	container.SecurityContext = &corev1.SecurityContext{
		Privileged:             boolPtr(false),
		RunAsNonRoot:           boolPtr(true),
		RunAsUser:              int64Ptr(1000),
		ReadOnlyRootFilesystem: boolPtr(false),
	}

	return container
}

// buildEnvVars builds environment variables from EnvVar spec.
func (r *AgentRuntimeReconciler) buildEnvVars(envVars []v1.EnvVar) []corev1.EnvVar {
	env := []corev1.EnvVar{}
	for _, e := range envVars {
		envVar := corev1.EnvVar{
			Name:  e.Name,
			Value: e.Value,
		}
		if e.ValueFrom != nil {
			if e.ValueFrom.ConfigMapKeyRef != nil {
				envVar.ValueFrom = &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: e.ValueFrom.ConfigMapKeyRef.Name,
						},
						Key: e.ValueFrom.ConfigMapKeyRef.Key,
					},
				}
			} else if e.ValueFrom.SecretKeyRef != nil {
				envVar.ValueFrom = &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: e.ValueFrom.SecretKeyRef.Name,
						},
						Key: e.ValueFrom.SecretKeyRef.Key,
					},
				}
			}
		}
		env = append(env, envVar)
	}
	return env
}

// buildResourceRequirements builds Kubernetes resource requirements.
func (r *AgentRuntimeReconciler) buildResourceRequirements(resources *v1.ContainerResources) corev1.ResourceRequirements {
	req := corev1.ResourceRequirements{}
	if resources.Limits != nil {
		req.Limits = corev1.ResourceList{}
		for k, v := range resources.Limits {
			req.Limits[corev1.ResourceName(k)] = resource.MustParse(string(v))
		}
	}
	if resources.Requests != nil {
		req.Requests = corev1.ResourceList{}
		for k, v := range resources.Requests {
			req.Requests[corev1.ResourceName(k)] = resource.MustParse(string(v))
		}
	}
	return req
}

// updateStatusFromPod updates AgentRuntime status from Pod state.
func (r *AgentRuntimeReconciler) updateStatusFromPod(ctx context.Context, runtime *v1.AgentRuntime, pod *corev1.Pod) {
	if pod == nil {
		r.updateStatus(ctx, runtime, v1.RuntimePhasePending, "")
		return
	}

	phase := v1.RuntimePhasePending
	message := ""

	switch pod.Status.Phase {
	case corev1.PodPending:
		phase = v1.RuntimePhaseCreating
		message = "Pod is being created"
	case corev1.PodRunning:
		allReady := true
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false
				break
			}
		}
		if allReady {
			phase = v1.RuntimePhaseRunning
			message = "All containers are running"
		} else {
			phase = v1.RuntimePhaseCreating
			message = "Containers are starting"
		}
	case corev1.PodFailed:
		phase = v1.RuntimePhaseFailed
		message = "Pod failed"
	case corev1.PodSucceeded:
		phase = v1.RuntimePhaseFailed
		message = "Pod unexpectedly succeeded"
	}

	if len(pod.Status.PodIPs) > 0 {
		runtime.Status.PodIPs = []string{}
		for _, ip := range pod.Status.PodIPs {
			runtime.Status.PodIPs = append(runtime.Status.PodIPs, ip.IP)
		}
	}

	r.updateStatus(ctx, runtime, phase, message)
}

// updateStatus updates the AgentRuntime status.
func (r *AgentRuntimeReconciler) updateStatus(ctx context.Context, runtime *v1.AgentRuntime, phase v1.RuntimePhase, message string) {
	log := log.FromContext(ctx)

	if runtime.Status.Phase != phase {
		runtime.Status.Phase = phase
		log.Info("Updating AgentRuntime status", "phase", phase, "message", message)
		if err := r.Status().Update(ctx, runtime); err != nil {
			log.Error(err, "failed to update status")
		}
	}
}

// Helper functions for pointer types
func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}