// ADK Handler entry point.
// Process Manager for ADK Framework.
// Uses ImageVolume to access Framework's filesystem at /framework-rootfs.
//
// Architecture:
// ┌─────────────────────────────────────────────────────────────────┐
// │ Pod (AgentRuntime)                                              │
// │                                                                 │
// │  Handler Container (Process Manager)                            │
// │    - Starts: /framework-rootfs/adk-framework                    │
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
	"aiagent/pkg/handler/adk"
)

var (
	frameworkBin = flag.String("framework", "", "Framework binary path (ImageVolume: /framework-rootfs/adk-framework)")
	workDir      = flag.String("workdir", "", "Shared work directory (e.g., /shared/workdir)")
	configDir    = flag.String("configdir", "", "Shared config directory (e.g., /shared/config)")
	debug        = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Parse()

	// Get framework path from environment or flag
	// Default: ImageVolume mounts Framework image at /framework-rootfs
	fwBin := *frameworkBin
	if fwBin == "" {
		fwBin = os.Getenv("FRAMEWORK_BIN")
		if fwBin == "" {
			fwBin = "/framework-rootfs/adk-framework"
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

	// Get process mode from environment
	// Values: "shared" (single process multi-agent) or "isolated" (one process per agent)
	processMode := handler.ProcessModeIsolated // default
	if pm := os.Getenv("PROCESS_MODE"); pm != "" {
		processMode = handler.ProcessModeType(pm)
		log.Printf("Process Mode from env: %s", pm)
	}

	log.Printf("ADK Handler starting...")
	log.Printf("Framework Binary: %s", fwBin)
	log.Printf("Work Directory: %s", wd)
	log.Printf("Config Directory: %s", cfgDir)
	log.Printf("Process Mode: %s", processMode)

	// Verify framework binary exists (ImageVolume should provide it)
	if _, err := os.Stat(fwBin); err != nil {
		log.Fatalf("Framework binary not found: %s (ImageVolume may not be configured)", fwBin)
	}

	// Create Handler configuration
	handlerCfg := &handler.HandlerConfig{
		Type:         handler.HandlerTypeADK,
		FrameworkBin: fwBin,
		WorkDir:      wd,
		ConfigDir:    cfgDir,
		DebugMode:    *debug,
		ProcessMode:  processMode,
	}

	// Create ADK Handler
	h := adk.NewADKHandler(handlerCfg)

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

	log.Printf("ADK Handler shutdown complete")
}

// runHandler runs the handler service loop.
func runHandler(ctx context.Context, h *adk.ADKHandler) error {
	// For now, handler just waits for controller commands
	// In production, this would receive commands via Unix socket or other mechanism

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

	// Stop all framework processes
	log.Printf("Stopping framework processes...")
	if err := h.StopFramework(context.Background()); err != nil {
		log.Printf("Error stopping framework: %v", err)
	}

	return nil
}