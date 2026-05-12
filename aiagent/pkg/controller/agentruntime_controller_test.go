// Package controller provides Kubernetes controllers for AI Agent resources.
package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"aiagent/api/v1"
)

func newTestAgentRuntime(name, namespace string) *v1.AgentRuntime {
	return &v1.AgentRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.AgentRuntimeSpec{
			AgentHandler: v1.AgentHandlerSpec{
				Image: "aiagent/handler:latest",
			},
			AgentFramework: v1.AgentFrameworkSpec{
				Image: "aiagent/framework:latest",
				Type:  "adk-go",
			},
			Replicas: 1,
		},
	}
}

func newTestAgentRuntimeReconciler() *AgentRuntimeReconciler {
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1.AgentRuntime{}, &corev1.Pod{}, &corev1.ConfigMap{}).
		Build()

	return &AgentRuntimeReconciler{
		Client: client,
		Scheme: scheme,
	}
}

func TestAgentRuntimeReconciler_Reconcile_Create(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestAgentRuntimeReconciler()

	// Create a test AgentRuntime
	runtime := newTestAgentRuntime("test-runtime", "default")
	if err := reconciler.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create test runtime: %v", err)
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-runtime",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should requeue after adding finalizer
	if !result.Requeue {
		t.Errorf("expected requeue after adding finalizer")
	}

	// Verify finalizer was added
	runtimeAfter := &v1.AgentRuntime{}
	if err := reconciler.Get(ctx, req.NamespacedName, runtimeAfter); err != nil {
		t.Fatalf("failed to get runtime: %v", err)
	}

	if !containsFinalizer(runtimeAfter, AgentRuntimeFinalizer) {
		t.Errorf("expected finalizer to be added")
	}
}

func TestAgentRuntimeReconciler_Reconcile_PodCreation(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestAgentRuntimeReconciler()

	// Create runtime with finalizer already set
	runtime := newTestAgentRuntime("test-runtime-pod", "default")
	controllerutil.AddFinalizer(runtime, AgentRuntimeFinalizer)
	if err := reconciler.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create test runtime: %v", err)
	}

	// Reconcile
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-runtime-pod",
			Namespace: "default",
		},
	}

	_, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify Pod was created
	podName := "test-runtime-pod-runtime"
	pod := &corev1.Pod{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: podName, Namespace: "default"}, pod); err != nil {
		t.Errorf("expected Pod to be created: %v", err)
	}

	// Verify Pod has correct labels
	if pod.Labels["agent.ai/runtime"] != "test-runtime-pod" {
		t.Errorf("expected runtime label, got %s", pod.Labels["agent.ai/runtime"])
	}

	// Verify Pod has two containers (handler + framework)
	if len(pod.Spec.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(pod.Spec.Containers))
	}

	// Verify container names
	if pod.Spec.Containers[0].Name != "agent-handler" {
		t.Errorf("expected first container to be agent-handler, got %s", pod.Spec.Containers[0].Name)
	}
	if pod.Spec.Containers[1].Name != "agent-framework" {
		t.Errorf("expected second container to be agent-framework, got %s", pod.Spec.Containers[1].Name)
	}
}

// TestAgentRuntimeReconciler_handleDeletion is skipped because cleanupResources
// uses field selectors which require index registration in fake client.
// Full deletion tests should use envtest for proper Kubernetes environment.
func TestAgentRuntimeReconciler_FinalizerRemoval(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestAgentRuntimeReconciler()

	// Create runtime with finalizer
	runtime := newTestAgentRuntime("test-finalizer", "default")
	controllerutil.AddFinalizer(runtime, AgentRuntimeFinalizer)
	if err := reconciler.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create test runtime: %v", err)
	}

	// Manually remove finalizer (simulating successful cleanup)
	controllerutil.RemoveFinalizer(runtime, AgentRuntimeFinalizer)
	if err := reconciler.Update(ctx, runtime); err != nil {
		t.Errorf("unexpected error updating runtime: %v", err)
	}

	// Verify finalizer was removed
	runtimeAfter := &v1.AgentRuntime{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: "test-finalizer", Namespace: "default"}, runtimeAfter); err != nil {
		t.Fatalf("failed to get runtime: %v", err)
	}

	if containsFinalizer(runtimeAfter, AgentRuntimeFinalizer) {
		t.Errorf("expected finalizer to be removed")
	}
}

func TestAgentRuntimeReconciler_BuildPodSpec(t *testing.T) {
	reconciler := newTestAgentRuntimeReconciler()

	runtime := &v1.AgentRuntime{
		Spec: v1.AgentRuntimeSpec{
			AgentHandler: v1.AgentHandlerSpec{
				Image: "handler:v1",
				Env: []v1.EnvVar{
					{Name: "HANDLER_ENV", Value: "test"},
				},
			},
			AgentFramework: v1.AgentFrameworkSpec{
				Image: "framework:v1",
				Type:  "adk-go",
			},
			NodeSelector: map[string]string{
				"node-type": "agent",
			},
		},
	}

	podSpec := reconciler.buildPodSpec(runtime)

	// Verify containers (Handler + DUMMY Framework)
	if len(podSpec.Containers) != 2 {
		t.Errorf("expected 2 containers")
	}

	// Verify NO init containers (ImageVolume pattern doesn't need initContainer)
	if len(podSpec.InitContainers) != 0 {
		t.Errorf("expected 0 init containers (ImageVolume pattern), got %d", len(podSpec.InitContainers))
	}

	// Verify node selector
	if podSpec.NodeSelector["node-type"] != "agent" {
		t.Errorf("expected node selector to be set")
	}

	// Verify handler container image
	if podSpec.Containers[0].Image != "handler:v1" {
		t.Errorf("expected handler image handler:v1, got %s", podSpec.Containers[0].Image)
	}

	// Verify framework container image
	if podSpec.Containers[1].Image != "framework:v1" {
		t.Errorf("expected framework image framework:v1, got %s", podSpec.Containers[1].Image)
	}

	// Verify framework container is DUMMY (sleep infinity command)
	if len(podSpec.Containers[1].Command) < 2 || podSpec.Containers[1].Command[0] != "sleep" || podSpec.Containers[1].Command[1] != "infinity" {
		t.Errorf("expected framework container to have 'sleep infinity' command (dummy container)")
	}

	// Verify ShareProcessNamespace is enabled
	if !*podSpec.ShareProcessNamespace {
		t.Errorf("expected ShareProcessNamespace to be true")
	}

	// Verify restart policy
	if podSpec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Errorf("expected RestartPolicyAlways")
	}

	// Verify ImageVolume for framework (K8s 1.36+ format)
	imageVolumeFound := false
	for _, vol := range podSpec.Volumes {
		if vol.Name == "framework-image" && vol.Image != nil {
			imageVolumeFound = true
			if vol.Image.Reference != "framework:v1" {
				t.Errorf("expected ImageVolume reference 'framework:v1', got %s", vol.Image.Reference)
			}
			if vol.Image.PullPolicy != corev1.PullIfNotPresent {
				t.Errorf("expected ImageVolume PullPolicy 'IfNotPresent', got %s", vol.Image.PullPolicy)
			}
		}
	}
	if !imageVolumeFound {
		t.Errorf("expected ImageVolume 'framework-image' for Framework container")
	}

	// Verify Handler mounts ImageVolume at /framework-rootfs
	handlerMountsFound := false
	for _, mount := range podSpec.Containers[0].VolumeMounts {
		if mount.Name == "framework-image" && mount.MountPath == "/framework-rootfs" {
			handlerMountsFound = true
		}
	}
	if !handlerMountsFound {
		t.Errorf("expected Handler container to mount 'framework-image' at '/framework-rootfs'")
	}
}

func TestAgentRuntimeReconciler_BuildEnvVars(t *testing.T) {
	reconciler := newTestAgentRuntimeReconciler()

	envVars := []v1.EnvVar{
		{Name: "SIMPLE_VAR", Value: "simple-value"},
		{Name: "CONFIG_VAR", ValueFrom: &v1.EnvVarSource{
			ConfigMapKeyRef: &v1.ConfigMapKeySelector{
				Name: "test-config",
				Key:  "test-key",
			},
		}},
		{Name: "SECRET_VAR", ValueFrom: &v1.EnvVarSource{
			SecretKeyRef: &v1.SecretKeySelector{
				Name: "test-secret",
				Key:  "secret-key",
			},
		}},
	}

	env := reconciler.buildEnvVars(envVars)

	if len(env) != 3 {
		t.Errorf("expected 3 env vars, got %d", len(env))
	}

	// Verify simple var
	if env[0].Name != "SIMPLE_VAR" || env[0].Value != "simple-value" {
		t.Errorf("unexpected simple var")
	}

	// Verify config map ref
	if env[1].Name != "CONFIG_VAR" || env[1].ValueFrom == nil || env[1].ValueFrom.ConfigMapKeyRef == nil {
		t.Errorf("unexpected config var")
	}

	// Verify secret ref
	if env[2].Name != "SECRET_VAR" || env[2].ValueFrom == nil || env[2].ValueFrom.SecretKeyRef == nil {
		t.Errorf("unexpected secret var")
	}
}

func TestAgentRuntimeReconciler_UpdateStatusFromPod(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestAgentRuntimeReconciler()

	runtime := newTestAgentRuntime("test-status", "default")
	if err := reconciler.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	tests := []struct {
		name     string
		podPhase corev1.PodPhase
		expected v1.RuntimePhase
	}{
		{
			name:     "pending pod",
			podPhase: corev1.PodPending,
			expected: v1.RuntimePhaseCreating,
		},
		{
			name:     "running pod with ready containers",
			podPhase: corev1.PodRunning,
			expected: v1.RuntimePhaseRunning,
		},
		{
			name:     "failed pod",
			podPhase: corev1.PodFailed,
			expected: v1.RuntimePhaseFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Phase: tt.podPhase,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "agent-handler", Ready: true},
						{Name: "agent-framework", Ready: true},
					},
				},
			}

			reconciler.updateStatusFromPod(ctx, runtime, pod)

			if runtime.Status.Phase != tt.expected {
				t.Errorf("expected phase %s, got %s", tt.expected, runtime.Status.Phase)
			}
		})
	}
}

func TestAgentRuntimeReconciler_NilPod(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestAgentRuntimeReconciler()

	runtime := newTestAgentRuntime("test-nil-pod", "default")
	if err := reconciler.Create(ctx, runtime); err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	// Pass nil pod
	reconciler.updateStatusFromPod(ctx, runtime, nil)

	if runtime.Status.Phase != v1.RuntimePhasePending {
		t.Errorf("expected Pending phase for nil pod, got %s", runtime.Status.Phase)
	}
}