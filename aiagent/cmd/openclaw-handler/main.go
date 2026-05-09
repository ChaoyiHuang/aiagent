// OpenClaw Handler entry point.
// Process Manager for OpenClaw Framework.
// Uses ImageVolume to access Framework's filesystem at /framework-rootfs.
//
// Architecture:
// ┌─────────────────────────────────────────────────────────────────┐
// │ Pod (AgentRuntime)                                              │
// │                                                                 │
// │  Handler Container (Process Manager)                            │
// │    - Starts: /framework-rootfs/usr/local/bin/openclaw           │
// │    - Controls process lifecycle (start/stop/monitor)            │
// │                                                                 │
// │  Framework Container (DUMMY)                                    │
// │    - ENTRYPOINT: pause (just sleeps)                            │
// │    - Provides image content for ImageVolume                     │
// └─────────────────────────────────────────────────────────────────┘
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"aiagent/pkg/handler"
	"aiagent/pkg/handler/openclaw"
)

var (
	gatewayURL   = flag.String("gateway", "", "OpenClaw Gateway URL (e.g., http://localhost:18789)")
	workDir      = flag.String("workdir", "", "Shared work directory (e.g., /shared/workdir)")
	configDir    = flag.String("configdir", "", "Shared config directory (e.g., /shared/config)")
	frameworkBin = flag.String("framework", "", "Framework binary path (ImageVolume: /framework-rootfs/usr/local/bin/openclaw)")
	debug        = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Parse()

	// Get Gateway URL from environment or flag
	gwURL := *gatewayURL
	if gwURL == "" {
		gwURL = os.Getenv("OPENCLAW_GATEWAY_URL")
		if gwURL == "" {
			gwURL = "http://localhost:18789"
		}
	}

	// Get work directory from environment or flag
	wd := *workDir
	if wd == "" {
		wd = os.Getenv("WORK_DIR")
		if wd == "" {
			wd = "/shared/workdir"
		}
	}

	// Get config directory from environment or flag
	cfgDir := *configDir
	if cfgDir == "" {
		cfgDir = os.Getenv("CONFIG_DIR")
		if cfgDir == "" {
			cfgDir = "/shared/config"
		}
	}

	// Get framework binary path from environment or flag
	// Default: ImageVolume mounts Framework image at /framework-rootfs
	fwBin := *frameworkBin
	if fwBin == "" {
		fwBin = os.Getenv("FRAMEWORK_BIN")
		if fwBin == "" {
			fwBin = "/framework-rootfs/usr/local/bin/openclaw"
		}
	}

	log.Printf("OpenClaw Handler starting...")
	log.Printf("Gateway URL: %s", gwURL)
	log.Printf("Work Directory: %s", wd)
	log.Printf("Config Directory: %s", cfgDir)
	log.Printf("Framework Binary: %s", fwBin)

	// Create Handler configuration
	handlerCfg := &handler.HandlerConfig{
		Type:         handler.HandlerTypeOpenClaw,
		FrameworkBin: fwBin,
		WorkDir:      wd,
		ConfigDir:    cfgDir,
		DebugMode:    *debug,
	}

	// Create OpenClaw Handler
	h := openclaw.NewOpenClawHandler(handlerCfg)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		cancel()
	}()

	// Run handler service
	if err := runHandler(ctx, h); err != nil {
		log.Fatalf("Handler error: %v", err)
	}

	log.Printf("OpenClaw Handler shutdown complete")
}

// runHandler runs the handler service loop.
func runHandler(ctx context.Context, h *openclaw.OpenClawHandler) error {
	// Wait for shutdown signal
	<-ctx.Done()

	// Cleanup: stop all agents
	log.Printf("Stopping all agents...")
	agents, err := h.ListAgents(context.Background())
	if err != nil {
		log.Printf("Error listing agents: %v", err)
	} else {
		for _, info := range agents {
			if err := h.StopAgent(context.Background(), info.ID); err != nil {
				log.Printf("Error stopping agent %s: %v", info.ID, err)
			}
		}
	}

	// Stop Gateway
	log.Printf("Stopping Gateway...")
	if err := h.StopFramework(context.Background()); err != nil {
		log.Printf("Error stopping Gateway: %v", err)
	}

	return nil
}