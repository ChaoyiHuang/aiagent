# ImageVolume Architecture

## Overview

This document describes the ImageVolume architecture for AgentRuntime Pods, which solves the filesystem isolation problem between Handler and Framework containers.

## Problem

In the original design, Handler Container and Framework Container share PID namespace and Network namespace, but have separate Mount namespace (filesystem isolation). This caused a problem:

- Handler executes `exec.Command("openclaw")` runs process in Handler's mount namespace
- The process cannot access Framework's `/usr/lib/node_modules` or Framework's openclaw binary
- Handler cannot effectively manage Framework processes

## Solution: ImageVolume (KEP-127)

Kubernetes 1.31+ introduces ImageVolume feature, allowing a container image to be mounted as a volume. This enables Handler Container to access Framework's complete filesystem.

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│ Pod (AgentRuntime)                                              │
│                                                                 │
│  Handler Container (Process Manager)                            │
│  ┌────────────────────────────────────────────────────────────┐│
│  │ VolumeMounts:                                              ││
│  │   /framework-rootfs -> ImageVolume (Framework image)       ││
│  │   /etc/harness/<name> -> Harness ConfigMaps                ││
│  │   /shared/workdir -> EmptyDir (agent workspace)            ││
│  │                                                            ││
│  │ Handler starts Framework processes:                        ││
│  │   ADK: exec.Command("/framework-rootfs/adk-framework")     ││
│  │   OpenClaw: exec.Command(                                  ││
│  │     "/framework-rootfs/usr/local/bin/openclaw")            ││
│  │                                                            ││
│  │ Handler controls process lifecycle (start/stop/monitor)    ││
│  └────────────────────────────────────────────────────────────┘│
│                                                                 │
│  Framework Container (DUMMY - provides image content only)     │
│  ┌────────────────────────────────────────────────────────────┐│
│  │ ENTRYPOINT: pause (minimal process that sleeps forever)    ││
│  │                                                            ││
│  │ Provides image content for ImageVolume:                    ││
│  │   - Framework binaries (adk-framework, openclaw)           ││
│  │   - Runtime dependencies (Node.js, Go, etc.)               ││
│  │   - Framework libraries and configurations                 ││
│  │                                                            ││
│  │ Does NOT run any Framework processes                       ││
│  │ Handler manages all Framework processes                    ││
│  └────────────────────────────────────────────────────────────┘│
│                                                                 │
│  ShareProcessNamespace: true                                   │
│  - Handler can see/ctrl Framework processes                    │
│  - Handler is the process manager                              │
│                                                                 │
│  ShareNetworkNamespace: true (implicit in Pod)                 │
│  - Handler and Framework processes share localhost             │
│  - No port conflicts                                            │
└─────────────────────────────────────────────────────────────────┘
```

### Key Benefits

1. **Handler is the sole process manager**
   - Handler starts/stops Framework processes
   - No redundant process manager in Framework Container
   - Handler has full control over lifecycle

2. **Framework Container is minimal**
   - Only provides image content for ImageVolume
   - Uses `pause` entrypoint (sleeps forever)
   - No active Framework processes running in Framework Container

3. **Filesystem access**
   - Handler can access Framework's complete filesystem via `/framework-rootfs`
   - Handler-started processes use Framework's binaries and libraries
   - No binary copying needed

4. **Independent image releases**
   - Handler and Framework images can be released independently
   - ImageVolume references Framework image dynamically
   - No coupling between Handler and Framework binaries

### Implementation Details

#### Controller (pkg/controller/agentruntime_controller.go)

```go
// ImageVolume: Mount Framework image content to Handler Container
frameworkImageVolume := corev1.Volume{
    Name: "framework-image",
    VolumeSource: corev1.VolumeSource{
        Image: &corev1.ImageVolumeSource{
            Reference: runtime.Spec.AgentFramework.Image,
        },
    },
}

// Handler mounts Framework image at /framework-rootfs
handlerVolumeMounts = append(handlerVolumeMounts,
    corev1.VolumeMount{Name: "framework-image", MountPath: "/framework-rootfs"},
)

// Framework Container is DUMMY (pause process only)
func (r *AgentRuntimeReconciler) buildFrameworkDummyContainer(name string, spec v1.AgentFrameworkSpec) corev1.Container {
    return corev1.Container{
        Name:    name,
        Image:   spec.Image,
        Command: []string{"pause"}, // Minimal process that sleeps forever
    }
}
```

#### Handler Dockerfile (Dockerfile.adk-handler, Dockerfile.openclaw-handler)

```dockerfile
# Framework binary path via ImageVolume
ENV FRAMEWORK_BIN=/framework-rootfs/adk-framework
# or for OpenClaw:
ENV FRAMEWORK_BIN=/framework-rootfs/usr/local/bin/openclaw
```

#### Framework Dockerfile (Dockerfile.adk-framework, Dockerfile.openclaw-framework)

```dockerfile
# DUMMY entrypoint - just sleep forever
ENTRYPOINT ["/pause"]

# Provides binaries for Handler to use:
# ADK: /adk-framework
# OpenClaw: /usr/local/bin/openclaw
```

### Requirements

- Kubernetes 1.31+ (ImageVolume feature gate enabled by default)
- containerd 1.7+ (runtime support for ImageVolume)

### Cleanup

Removed old components:
- `Dockerfile.init-container` (deleted)
- `scripts/init-mount.sh` (deleted)
- InitContainer in Pod spec (removed)
- Bind mount setup logic (removed)

### Testing

Controller tests verify:
- No init containers (ImageVolume pattern doesn't need them)
- Framework Container has `pause` command (dummy container)
- ImageVolume is configured for Framework image
- Handler mounts ImageVolume at `/framework-rootfs`
- ShareProcessNamespace is enabled