#!/bin/bash
# Kind deployment script for AI Agent demo
# Creates cluster, builds images, deploys controllers and sample resources

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

CLUSTER_NAME="aiagent-cluster"
KIND_NODE_IMAGE="kindest/node:v1.29.2"

echo "========================================"
echo "AI Agent Kind Deployment Script"
echo "========================================"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v kind &> /dev/null; then
        log_error "kind is not installed. Please install it first."
        exit 1
    fi

    if ! command -v docker &> /dev/null; then
        log_error "docker is not installed. Please install it first."
        exit 1
    fi

    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed. Please install it first."
        exit 1
    fi

    log_info "Prerequisites OK"
}

# Create Kind cluster
create_cluster() {
    log_info "Creating Kind cluster: $CLUSTER_NAME"

    if kind get clusters | grep -q "$CLUSTER_NAME"; then
        log_warn "Cluster $CLUSTER_NAME already exists. Deleting it first..."
        kind delete cluster --name "$CLUSTER_NAME"
    fi

    kind create cluster \
        --name "$CLUSTER_NAME" \
        --image "$KIND_NODE_IMAGE" \
        --config "$PROJECT_DIR/config/kind/kind-config.yaml"

    log_info "Cluster created successfully"
}

# Build Docker images
build_images() {
    log_info "Building Docker images..."

    cd "$PROJECT_DIR"

    # Build controller manager
    log_info "Building controller-manager..."
    docker build -t aiagent/controller-manager:latest -f Dockerfile.manager .

    # Build ADK Handler
    log_info "Building adk-handler..."
    docker build -t aiagent/adk-handler:latest -f Dockerfile.adk-handler .

    # Build OpenClaw Handler
    log_info "Building openclaw-handler..."
    docker build -t aiagent/openclaw-handler:latest -f Dockerfile.openclaw-handler .

    # Build ADK Framework
    log_info "Building adk-framework..."
    docker build -t aiagent/adk-framework:latest -f Dockerfile.adk-framework .

    # Build Init Container
    log_info "Building init-container..."
    docker build -t aiagent/init-container:latest -f Dockerfile.init-container .

    log_info "All images built successfully"
}

# Load images into Kind cluster
load_images() {
    log_info "Loading images into Kind cluster..."

    kind load docker-image aiagent/controller-manager:latest --name "$CLUSTER_NAME"
    kind load docker-image aiagent/adk-handler:latest --name "$CLUSTER_NAME"
    kind load docker-image aiagent/openclaw-handler:latest --name "$CLUSTER_NAME"
    kind load docker-image aiagent/adk-framework:latest --name "$CLUSTER_NAME"
    kind load docker-image aiagent/init-container:latest --name "$CLUSTER_NAME"

    log_info "Images loaded successfully"
}

# Deploy CRDs
deploy_crds() {
    log_info "Deploying CRDs..."

    kubectl apply -f "$PROJECT_DIR/config/crd/bases/"

    # Wait for CRDs to be established
    sleep 3

    log_info "CRDs deployed successfully"
}

# Deploy RBAC
deploy_rbac() {
    log_info "Deploying RBAC..."

    kubectl apply -f "$PROJECT_DIR/config/rbac/"

    log_info "RBAC deployed successfully"
}

# Deploy Controller Manager
deploy_controller() {
    log_info "Deploying Controller Manager..."

    kubectl apply -f "$PROJECT_DIR/config/manager/"

    # Wait for deployment to be ready
    log_info "Waiting for controller manager to be ready..."
    kubectl wait --for=condition=available --timeout=120s deployment/aiagent-controller-manager -n aiagent-system || true

    # Check deployment status
    kubectl get deployment -n aiagent-system

    log_info "Controller Manager deployed"
}

# Deploy sample resources
deploy_samples() {
    log_info "Deploying sample Harness CRs..."

    kubectl apply -f "$PROJECT_DIR/config/samples/harness.yaml"

    sleep 2

    log_info "Deploying sample AgentRuntime CRs..."
    kubectl apply -f "$PROJECT_DIR/config/samples/runtime.yaml"

    sleep 5

    log_info "Checking AgentRuntime status..."
    kubectl get agentruntimes -n default

    log_info "Deploying sample AIAgent CR..."
    kubectl apply -f "$PROJECT_DIR/config/samples/agent.yaml"

    kubectl get aigents -n default

    log_info "Sample resources deployed"
}

# Verify deployment
verify_deployment() {
    log_info "Verifying deployment..."

    echo ""
    echo "========================================"
    echo "Deployment Status"
    echo "========================================"

    echo ""
    echo "Controller Manager:"
    kubectl get pods -n aiagent-system

    echo ""
    echo "AgentRuntimes:"
    kubectl get agentruntimes -n default

    echo ""
    echo "AIAgents:"
    kubectl get aigents -n default

    echo ""
    echo "Pods created by AgentRuntimes:"
    kubectl get pods -n default -l aiagent.io/type=agent-runtime

    echo ""
    log_info "Deployment verification complete"
}

# Show usage information
show_usage() {
    echo ""
    echo "========================================"
    echo "Usage Information"
    echo "========================================"
    echo ""
    echo "To interact with the cluster:"
    echo "  kubectl get agentruntimes -n default"
    echo "  kubectl get aigents -n default"
    echo "  kubectl get pods -n default"
    echo ""
    echo "To delete the cluster:"
    echo "  kind delete cluster --name $CLUSTER_NAME"
    echo ""
}

# Full deployment
full_deploy() {
    check_prerequisites
    create_cluster
    build_images
    load_images
    deploy_crds
    deploy_rbac
    deploy_controller
    deploy_samples
    verify_deployment
    show_usage
}

# Parse arguments
case "$1" in
    create)
        create_cluster
        ;;
    build)
        build_images
        ;;
    load)
        load_images
        ;;
    deploy)
        deploy_crds
        deploy_rbac
        deploy_controller
        deploy_samples
        ;;
    verify)
        verify_deployment
        ;;
    clean)
        kind delete cluster --name "$CLUSTER_NAME"
        ;;
    all)
        full_deploy
        ;;
    *)
        echo "Usage: $0 {create|build|load|deploy|verify|clean|all}"
        echo ""
        echo "  create - Create Kind cluster"
        echo "  build  - Build Docker images"
        echo "  load   - Load images into cluster"
        echo "  deploy - Deploy CRDs, RBAC, and controller"
        echo "  verify - Verify deployment status"
        echo "  clean  - Delete the cluster"
        echo "  all    - Full deployment (create + build + load + deploy)"
        exit 1
        ;;
esac