// Package main provides the entry point for the AI Agent Controller Manager.
// The manager runs all Kubernetes controllers for AIAgent, AgentRuntime, and Harness CRDs.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"aiagent/api/v1"
	"aiagent/pkg/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var namespace string
	var leaderElectionID string
	var webhookPort int

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "aiagent-controller-manager",
		"The ID to use for leader election.")
	flag.StringVar(&namespace, "namespace", "",
		"Namespace to watch. If empty, watches all namespaces.")
	flag.IntVar(&webhookPort, "webhook-port", 9443,
		"Port for webhook server. Set to 0 to disable webhooks.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Get Kubernetes config using controller-runtime's GetConfigOrDie
	// This handles in-cluster config and kubeconfig automatically
	config := ctrl.GetConfigOrDie()

	// Wait for CRDs to be available (useful when running locally for testing)
	setupLog.Info("Waiting for CRDs to be available...")
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitForCRDs(waitCtx, config); err != nil {
		setupLog.Error(err, "CRDs not available")
		os.Exit(1)
	}
	setupLog.Info("CRDs are available")

	// Create manager options
	managerOpts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
	}

	if webhookPort > 0 {
		managerOpts.WebhookServer = webhook.NewServer(webhook.Options{
			Port: webhookPort,
		})
	}

	if namespace != "" {
		managerOpts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace: {},
			},
		}
	}

	// Create the manager
	mgr, err := ctrl.NewManager(config, managerOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup controllers
	setupLog.Info("Setting up controllers")

	// Harness Controller
	if err := (&controller.HarnessReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Harness")
		os.Exit(1)
	}
	setupLog.Info("Harness controller setup complete")

	// AgentRuntime Controller
	if err := (&controller.AgentRuntimeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AgentRuntime")
		os.Exit(1)
	}
	setupLog.Info("AgentRuntime controller setup complete")

	// AIAgent Controller
	if err := (&controller.AIAgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AIAgent")
		os.Exit(1)
	}
	setupLog.Info("AIAgent controller setup complete")

	// Setup health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")

	// Start the manager
	ctx := ctrl.SetupSignalHandler()
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// waitForCRDs waits for CRDs to be available before starting controllers
func waitForCRDs(ctx context.Context, config *rest.Config) error {
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	crds := []struct {
		group    string
		version  string
		resource string
		kind     string
	}{
		{"agent.ai", "v1", "harnesses", "HarnessList"},
		{"agent.ai", "v1", "agentruntimes", "AgentRuntimeList"},
		{"agent.ai", "v1", "aiagents", "AIAgentList"},
	}

	for i := 0; i < 30; i++ {
		allFound := true
		for _, crd := range crds {
			gvk := schema.GroupVersionKind{
				Group:   crd.group,
				Version: crd.version,
				Kind:    crd.kind,
			}
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(gvk)
			if err := c.List(ctx, list); err != nil {
				allFound = false
				setupLog.V(1).Info("CRD not yet available", "crd", crd.resource)
				break
			}
		}
		if allFound {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("CRDs not available after 30 seconds")
}