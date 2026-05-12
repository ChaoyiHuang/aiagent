# ImageVolume Architecture

## Overview

This document describes the ImageVolume architecture for AgentRuntime Pods, which solves the filesystem isolation problem between Handler and Framework containers. This architecture has been **verified in E2E tests** with Kubernetes 1.35.

## Problem

In the original design, Handler Container and Framework Container share PID namespace and Network namespace, but have separate Mount namespace (filesystem isolation). This caused a problem:

- Handler executes `exec.Command("openclaw")` runs process in Handler's mount namespace
- The process cannot access Framework's `/usr/lib/node_modules` or Framework's openclaw binary
- Handler cannot effectively manage Framework processes

## Solution: ImageVolume (KEP-127)

Kubernetes 1.35+ introduces ImageVolume feature (enabled by default), allowing a container image to be mounted as a volume. This enables Handler Container to access Framework's complete filesystem.

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
│  │   /etc/agent-config -> hostPath (Config Daemon)            ││
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
│  │ ENTRYPOINT: ["sleep", "infinity"]                          ││
│  │                                                            ││
│  │ Provides image content for ImageVolume:                    ││
│  │   - Framework binaries (adk-framework, openclaw)           ││
│  │   - Runtime dependencies (Go 1.25, Node.js, etc.)          ││
│  │   - Framework libraries and configurations                 ││
│  │   - adk-go library (for ADK integration)                   ││
│  │                                                            ││
│  │ Does NOT run any Framework processes                       ││
│  │ Handler manages all Framework processes                    ││
│  └────────────────────────────────────────────────────────────┘│
│                                                                 │
│  ShareProcessNamespace: true                                   │
│  - Handler can see/ctrl Framework processes                    │
│  - Handler is the process manager                              │
│                                                                 │
│  ImageVolume Configuration:                                    │
│  │   reference: aiagent/adk-framework:test                    ││
│  │   pullPolicy: IfNotPresent                                  ││
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
   - Uses `sleep infinity` entrypoint (native Alpine command)
   - No active Framework processes running in Framework Container

3. **Filesystem access**
   - Handler can access Framework's complete filesystem via `/framework-rootfs`
   - Handler-started processes use Framework's binaries and libraries
   - No binary copying needed

4. **Independent image releases**
   - Handler and Framework images can be released independently
   - ImageVolume references Framework image dynamically
   - No coupling between Handler and Framework binaries

5. **ADK-Go Library Integration**
   - adk-framework imports adk-go via local replace directive
   - Framework creates real agents using `llmagent.New()`
   - Runner executes agents with proper session management

### Implementation Details

#### Controller (pkg/controller/agentruntime_controller.go)

```go
// ImageVolume: Mount Framework image content to Handler Container
frameworkImageVolume := corev1.Volume{
    Name: "framework-image",
    VolumeSource: corev1.VolumeSource{
        Image: &corev1.ImageVolumeSource{
            Reference: runtime.Spec.AgentFramework.Image,
            PullPolicy: corev1.PullIfNotPresent,
        },
    },
}

// Handler mounts Framework image at /framework-rootfs
handlerVolumeMounts = append(handlerVolumeMounts,
    corev1.VolumeMount{Name: "framework-image", MountPath: "/framework-rootfs"},
)

// Framework Container is DUMMY (sleep infinity)
func (r *AgentRuntimeReconciler) buildFrameworkDummyContainer(name string, spec v1.AgentFrameworkSpec) corev1.Container {
    return corev1.Container{
        Name:    name,
        Image:   spec.Image,
        Command: []string{"sleep", "infinity"},
    }
}
```

#### Handler Dockerfile (Dockerfile.adk-handler)

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /workspace

# Copy adk-go module for local replace
COPY adk-go/ /adk-go/

# Copy aiagent module
COPY aiagent/go.mod aiagent/go.sum ./
RUN go mod download

COPY aiagent/api/ api/
COPY aiagent/pkg/ pkg/
COPY aiagent/cmd/ cmd/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o adk-handler ./cmd/adk-handler

FROM alpine:3.18
COPY --from=builder /workspace/adk-handler /adk-handler

ENV FRAMEWORK_BIN=/framework-rootfs/adk-framework
ENV WORK_DIR=/shared/workdir
ENV CONFIG_DIR=/shared/config

ENTRYPOINT ["/adk-handler"]
```

#### Framework Dockerfile (Dockerfile.adk-framework)

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /workspace

# Copy adk-go module (for local replace directive)
COPY adk-go/ /adk-go/

# Copy aiagent module
COPY aiagent/go.mod aiagent/go.sum ./
RUN go mod download

# Copy source code
COPY aiagent/cmd/ cmd/

# Build ADK Framework binary (with adk-go integration)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o adk-framework ./cmd/adk-framework

FROM alpine:3.18
COPY --from=builder /workspace/adk-framework /adk-framework

# DUMMY entrypoint - just sleep forever
ENTRYPOINT ["sleep", "infinity"]
```

### Requirements

- Kubernetes 1.35+ (ImageVolume feature gate enabled by default)
- Go 1.25+ (adk-go library requires Go 1.25)
- containerd 1.7+ (runtime support for ImageVolume)

### E2E Test Verification

Tests verify:
- ✓ ImageVolume configured with proper pullPolicy (IfNotPresent)
- ✓ Framework Container has `sleep infinity` command (DUMMY)
- ✓ ShareProcessNamespace enabled
- ✓ Handler shows correct agent count in status logs
- ✓ All 3 process modes work: ADK Shared, ADK Isolated, OpenClaw Gateway