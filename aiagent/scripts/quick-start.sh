#!/bin/bash
# Quick start script for AI Agent deployment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "AI Agent Quick Start"
echo "===================="

# Check prerequisites
check_prerequisites() {
    echo "Checking prerequisites..."

    missing=""
    for cmd in go docker kind kubectl; do
        if ! command -v $cmd &> /dev/null; then
            missing="$missing $cmd"
        fi
    done

    if [ -n "$missing" ]; then
        echo "ERROR: Missing tools:$missing"
        echo ""
        echo "Please install:"
        echo "  - Go: https://go.dev/doc/install"
        echo "  - Docker: https://docs.docker.com/get-docker/"
        echo "  - Kind: https://kind.sigs.k8s.io/docs/user/quick-start/"
        echo "  - Kubectl: https://kubernetes.io/docs/tasks/tools/"
        exit 1
    fi

    echo "All prerequisites satisfied"
}

# Build everything
build() {
    echo ""
    echo "Building..."
    cd "$PROJECT_DIR"

    # Build Go binaries
    echo "  Building Go binaries..."
    go build -o bin/manager ./cmd/manager
    go build -o bin/handler ./cmd/handler

    # Build Docker images
    echo "  Building Docker images..."
    docker build -t aiagent/controller-manager:latest -f Dockerfile.manager .
    docker build -t aiagent/handler:latest -f Dockerfile.handler .

    echo "Build complete"
}

# Deploy to kind
deploy() {
    echo ""
    echo "Deploying to kind cluster..."

    # Create cluster if not exists
    if ! kind get clusters | grep -q "aiagent-cluster"; then
        echo "  Creating kind cluster..."
        kind create cluster --name aiagent-cluster --config "$SCRIPT_DIR/kind-config.yaml"
    else
        echo "  Cluster already exists"
    fi

    # Load images
    echo "  Loading Docker images..."
    kind load docker-image aiagent/controller-manager:latest --name aiagent-cluster
    kind load docker-image aiagent/handler:latest --name aiagent-cluster

    # Install CRDs
    echo "  Installing CRDs..."
    kubectl apply -f "$PROJECT_DIR/config/crd/bases/"

    # Deploy controller
    echo "  Deploying controller..."
    kubectl apply -f "$PROJECT_DIR/config/rbac/"
    kubectl apply -f "$PROJECT_DIR/config/manager/"

    # Wait for controller
    echo "  Waiting for controller to be ready..."
    kubectl wait --for=condition=available --timeout=120s deployment/aiagent-controller-manager -n aiagent-system

    echo "Deployment complete"
}

# Deploy samples
samples() {
    echo ""
    echo "Deploying sample resources..."

    # Deploy harnesses
    echo "  Creating Harnesses..."
    kubectl apply -f "$PROJECT_DIR/config/samples/harness.yaml"

    # Deploy runtimes
    echo "  Creating AgentRuntimes..."
    kubectl apply -f "$PROJECT_DIR/config/samples/runtime.yaml"

    # Deploy agents
    echo "  Creating AIAgents..."
    kubectl apply -f "$PROJECT_DIR/config/samples/agent.yaml"

    echo "Samples deployed"
}

# Show status
status() {
    echo ""
    echo "Cluster Status"
    echo "=============="

    echo ""
    echo "Controller:"
    kubectl get pods -n aiagent-system

    echo ""
    echo "Harnesses:"
    kubectl get harnesses -n default

    echo ""
    echo "AgentRuntimes:"
    kubectl get agentruntimes -n default

    echo ""
    echo "AIAgents:"
    kubectl get aigents -n default
}

# Cleanup
cleanup() {
    echo ""
    echo "Cleaning up..."
    kind delete cluster --name aiagent-cluster
    echo "Cluster deleted"
}

# Main
case "${1:-all}" in
    "check")
        check_prerequisites
        ;;
    "build")
        check_prerequisites
        build
        ;;
    "deploy")
        check_prerequisites
        deploy
        ;;
    "samples")
        samples
        ;;
    "status")
        status
        ;;
    "cleanup")
        cleanup
        ;;
    "all")
        check_prerequisites
        build
        deploy
        samples
        status
        ;;
    *)
        echo "Usage: $0 {check|build|deploy|samples|status|cleanup|all}"
        exit 1
        ;;
esac