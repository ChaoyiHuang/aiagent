# AI Agent Kubernetes Controller

This project implements a Kubernetes-native abstraction layer for running AI agents from multiple frameworks (ADK-Go, OpenClaw, LangChain, etc.) in a unified manner.

## Architecture

The system consists of:

1. **Controller Manager** (`cmd/manager`) - Kubernetes controller that manages:
   - Harness CRDs - External capabilities (Model, MCP, Memory, Sandbox, Skills)
   - AgentRuntime CRDs - Runtime environments for AI agents
   - AIAgent CRDs - Individual AI agent instances

2. **Handler Process** (`cmd/handler`) - Process that runs inside AgentRuntime pods:
   - Watches configuration files for updates
   - Loads and manages AI agent instances
   - Communicates with framework-specific handlers

3. **Handler Registry** (`pkg/handler`) - Registry for framework-specific handlers:
   - ADK-Go handler (`pkg/handler/adk`)
   - OpenClaw handler (`pkg/handler/openclaw`)
   - Extensible to support additional frameworks

4. **Harness Manager** (`pkg/harness`) - Manages external capabilities:
   - Model harness - LLM provider integration
   - Memory harness - State storage
   - Sandbox harness - Execution isolation
   - Skills harness - Tool/skill modules

## Deployment to Kind Cluster

### Prerequisites

- Go 1.23+
- Docker
- Kind
- kubectl

### Quick Start

```bash
# Build and deploy everything
./scripts/kind-deploy.sh full

# Or step by step:
./scripts/kind-deploy.sh create  # Create kind cluster
./scripts/kind-deploy.sh build   # Build Docker images
./scripts/kind-deploy.sh load    # Load images into kind
./scripts/kind-deploy.sh crds    # Install CRDs
./scripts/kind-deploy.sh deploy  # Deploy controller
./scripts/kind-deploy.sh samples # Deploy sample resources
```

### Manual Deployment

```bash
# 1. Create kind cluster
kind create cluster --name aiagent-cluster --config scripts/kind-config.yaml

# 2. Build images
docker build -t aiagent/controller-manager:latest -f Dockerfile.manager .
docker build -t aiagent/handler:latest -f Dockerfile.handler .

# 3. Load images
kind load docker-image aiagent/controller-manager:latest --name aiagent-cluster
kind load docker-image aiagent/handler:latest --name aiagent-cluster

# 4. Install CRDs
kubectl apply -f config/crd/bases/

# 5. Deploy controller
kubectl apply -f config/rbac/
kubectl apply -f config/manager/

# 6. Deploy sample resources
kubectl apply -f config/samples/harness.yaml
kubectl apply -f config/samples/runtime.yaml
kubectl apply -f config/samples/agent.yaml
```

## Configuration

### Harness Configuration

Harnesses are defined as CRDs and provide external capabilities:

```yaml
apiVersion: ai.k8s.io/v1
kind: Harness
metadata:
  name: model-harness-deepseek
spec:
  type: model
  model:
    provider: deepseek
    endpoint: https://api.deepseek.com
    apiKeyRef:
      name: deepseek-api-key
      key: api-key
    defaultModel: deepseek-chat
```

### AgentRuntime Configuration

AgentRuntime defines a runtime environment:

```yaml
apiVersion: ai.k8s.io/v1
kind: AgentRuntime
metadata:
  name: adk-runtime-sample
spec:
  agentHandler:
    image: aiagent/handler:latest
  agentFramework:
    image: aiagent/adk-framework:latest
    type: adk-go
  harness:
  - name: model-harness-deepseek
  replicas: 1
```

### AIAgent Configuration

AIAgent defines an individual AI agent:

```yaml
apiVersion: ai.k8s.io/v1
kind: AIAgent
metadata:
  name: adk-agent-sample
spec:
  runtimeRef:
    name: adk-runtime-sample
    type: adk-go
  description: "Sample AI Agent using ADK-Go framework"
  volumePolicy: retain
```

## Directory Structure

```
aiagent/
├── api/v1/                    # CRD type definitions
├── cmd/
│   ├── manager/               # Controller manager entry point
│   └── handler/               # Handler process entry point
├── config/
│   ├── crd/bases/             # CRD YAML files
│   ├── rbac/                  # RBAC configuration
│   ├── manager/               # Manager deployment
│   └── samples/               # Sample YAML files
├── pkg/
│   ├── agent/                 # Core agent abstraction
│   ├── controller/            # Kubernetes controllers
│   ├── handler/               # Handler interface & registry
│   ├── harness/               # Harness integration
│   ├── runtime/               # Runtime utilities
│   └── scheduler/             # Agent scheduling
├── scripts/
│   ├── kind-config.yaml       # Kind cluster configuration
│   └ kind-deploy.sh           # Deployment script
├── test/
│   ├── integration/           # Integration tests
│   ├── unit/                  # Unit tests
│   └ e2e/                     # End-to-end tests
├── Dockerfile.manager         # Controller manager Dockerfile
├── Dockerfile.handler         # Handler process Dockerfile
├── Makefile                   # Build and deployment targets
└── go.mod                     # Go module definition
```

## Supported Frameworks

### ADK-Go
- Native Go implementation
- Supports multi-agent systems
- LLM agents, workflow agents

### OpenClaw
- TypeScript-based (requires Node.js bridge)
- Multi-channel messaging support
- Configuration-driven agents

## Make Targets

```bash
make build          # Build binaries
make test           # Run all tests
make docker-build   # Build Docker images
make kind-create    # Create kind cluster
make kind-deploy    # Deploy to kind
make clean          # Clean build artifacts
```

## Testing

```bash
# Run unit tests
make test-unit

# Run integration tests (requires envtest)
export CRD_DIR=config/crd/bases
export KUBEBUILDER_ASSETS=bin
make test-integration
```

## License

Apache 2.0