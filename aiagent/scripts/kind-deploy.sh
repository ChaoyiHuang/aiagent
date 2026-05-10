#!/bin/bash
# Kind deployment script for AI Agent system
# This script deploys the AI Agent controller to a kind cluster

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Configuration
CLUSTER_NAME="${CLUSTER_NAME:-aiagent-cluster}"
CONTROLLER_IMAGE="${CONTROLLER_IMAGE:-aiagent/controller-manager:latest}"
HANDLER_IMAGE="${HANDLER_IMAGE:-aiagent/handler:latest}"

echo "=========================================="
echo "AI Agent Kind Deployment Script"
echo "=========================================="
echo "Cluster: $CLUSTER_NAME"
echo "Controller Image: $CONTROLLER_IMAGE"
echo "Handler Image: $HANDLER_IMAGE"
echo ""

# Check if kind is installed
if ! command -v kind &> /dev/null; then
    echo "ERROR: kind is not installed"
    echo "Please install kind: https://kind.sigs.k8s.io/docs/user/quick-start/"
    exit 1
fi

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo "ERROR: kubectl is not installed"
    echo "Please install kubectl: https://kubernetes.io/docs/tasks/tools/"
    exit 1
fi

# Check if docker is installed
if ! command -v docker &> /dev/null; then
    echo "ERROR: docker is not installed"
    echo "Please install docker: https://docs.docker.com/get-docker/"
    exit 1
fi

# Function to create cluster
create_cluster() {
    echo "Step 1: Creating kind cluster..."
    if kind get clusters | grep -q "$CLUSTER_NAME"; then
        echo "Cluster $CLUSTER_NAME already exists"
        read -p "Delete and recreate? (y/n): " confirm
        if [[ "$confirm" == "y" ]]; then
            kind delete cluster --name "$CLUSTER_NAME"
            kind create cluster --name "$CLUSTER_NAME" --config "$SCRIPT_DIR/kind-config.yaml"
        fi
    else
        kind create cluster --name "$CLUSTER_NAME" --config "$SCRIPT_DIR/kind-config.yaml"
    fi
    echo "Cluster created successfully"
}

# Function to build images
build_images() {
    echo "Step 2: Building Docker images..."
    cd "$PROJECT_DIR"

    # Build controller manager image
    echo "Building controller manager image..."
    docker build -t "$CONTROLLER_IMAGE" -f Dockerfile.manager .

    # Build handler image
    echo "Building handler image..."
    docker build -t "$HANDLER_IMAGE" -f Dockerfile.handler .

    echo "Images built successfully"
}

# Function to load images into kind
load_images() {
    echo "Step 3: Loading images into kind cluster..."
    kind load docker-image "$CONTROLLER_IMAGE" --name "$CLUSTER_NAME"
    kind load docker-image "$HANDLER_IMAGE" --name "$CLUSTER_NAME"
    echo "Images loaded successfully"
}

# Function to install CRDs
install_crds() {
    echo "Step 4: Installing CRDs..."
    kubectl apply -f "$PROJECT_DIR/config/crd/bases/"
    echo "Waiting for CRDs to be established..."
    kubectl wait --for=condition=established --timeout=60s crd/harnesses.agent.ai
    kubectl wait --for=condition=established --timeout=60s crd/agentruntimes.agent.ai
    kubectl wait --for=condition=established --timeout=60s crd/aigents.agent.ai
    echo "CRDs installed successfully"
}

# Function to deploy controller
deploy_controller() {
    echo "Step 5: Deploying controller manager..."
    kubectl apply -f "$PROJECT_DIR/config/rbac/"
    kubectl apply -f "$PROJECT_DIR/config/manager/"

    echo "Waiting for controller manager deployment..."
    kubectl wait --for=condition=available --timeout=120s deployment/aiagent-controller-manager -n aiagent-system

    echo "Controller manager deployed successfully"
}

# Function to deploy sample resources
deploy_samples() {
    echo "Step 6: Deploying sample Harnesses..."
    kubectl apply -f "$PROJECT_DIR/config/samples/harness.yaml"

    # Wait for harnesses to be available
    sleep 5
    kubectl get harnesses -n default

    echo "Step 7: Deploying sample AgentRuntimes..."
    kubectl apply -f "$PROJECT_DIR/config/samples/runtime.yaml"

    # Wait for runtimes to be running
    echo "Waiting for AgentRuntimes to initialize..."
    sleep 30
    kubectl get agentruntimes -n default

    echo "Step 8: Deploying sample AIAgents..."
    kubectl apply -f "$PROJECT_DIR/config/samples/agent.yaml"

    # Wait for agents
    sleep 10
    kubectl get aigents -n default
}

# Function to show status
show_status() {
    echo ""
    echo "=========================================="
    echo "Deployment Status"
    echo "=========================================="

    echo ""
    echo "Controller Manager:"
    kubectl get pods -n aiagent-system -l control-plane=controller-manager

    echo ""
    echo "Harnesses:"
    kubectl get harnesses -n default

    echo ""
    echo "AgentRuntimes:"
    kubectl get agentruntimes -n default

    echo ""
    echo "AIAgents:"
    kubectl get aigents -n default

    echo ""
    echo "=========================================="
    echo "Next steps:"
    echo "1. Configure API keys in secrets"
    echo "2. Monitor agent status: kubectl get aigents -n default -w"
    echo "3. Check logs: kubectl logs -n aiagent-system deployment/aiagent-controller-manager"
    echo "=========================================="
}

# Main execution
case "${1:-full}" in
    "create")
        create_cluster
        ;;
    "build")
        build_images
        ;;
    "load")
        load_images
        ;;
    "crds")
        install_crds
        ;;
    "deploy")
        deploy_controller
        ;;
    "samples")
        deploy_samples
        ;;
    "status")
        show_status
        ;;
    "full")
        create_cluster
        build_images
        load_images
        install_crds
        deploy_controller
        deploy_samples
        show_status
        ;;
    "cleanup")
        echo "Cleaning up..."
        kind delete cluster --name "$CLUSTER_NAME"
        echo "Cluster deleted"
        ;;
    *)
        echo "Usage: $0 {create|build|load|crds|deploy|samples|status|full|cleanup}"
        echo ""
        echo "Commands:"
        echo "  create   - Create kind cluster"
        echo "  build    - Build Docker images"
        echo "  load     - Load images into kind"
        echo "  crds     - Install CRDs"
        echo "  deploy   - Deploy controller manager"
        echo "  samples  - Deploy sample resources"
        echo "  status   - Show deployment status"
        echo "  full     - Full deployment (all steps)"
        echo "  cleanup  - Delete cluster"
        exit 1
        ;;
esac