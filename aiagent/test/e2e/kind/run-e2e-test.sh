#!/bin/bash
# AIAgent E2E Test - Complete Setup and Automation Script
# Installs all dependencies (Docker, Kind, Kubectl, jq) and runs E2E tests
#
# Version Configuration:
#   - Kind: v0.31.0
#   - Kubernetes: v1.35.0
#   - ImageVolume: enabled via feature gate in K8s 1.35
#
# Usage:
#   ./run-e2e-test.sh          # Full setup + test
#   ./run-e2e-test.sh test     # Only run test (assuming setup done)
#   ./run-e2e-test.sh cleanup  # Cleanup cluster

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$(dirname "$SCRIPT_DIR")")")"
KIND_CLUSTER_NAME="aiagent-test"
K8S_VERSION="v1.35.0"
KIND_VERSION="v0.31.0"

echo "=================================================="
echo "AIAgent E2E Test Automation"
echo "=================================================="
echo "Project Root: ${PROJECT_ROOT}"
echo "Kind Cluster: ${KIND_CLUSTER_NAME}"
echo "Kind Version: ${KIND_VERSION}"
echo "Kubernetes Version: ${K8S_VERSION}"
echo "=================================================="

# ============================================================
# Step 1: Install Dependencies
# ============================================================

install_docker() {
    echo ">>> [1/4] Installing Docker..."

    if command -v docker >/dev/null 2>&1; then
        echo "    Docker already installed: $(docker --version)"
        return 0
    fi

    # Install Docker using official script
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    sh /tmp/get-docker.sh

    # Start Docker service
    systemctl start docker
    systemctl enable docker

    # Verify installation
    docker --version
    echo "    Docker installed successfully"
}

install_kind() {
    echo ">>> [2/4] Installing Kind v${KIND_VERSION}..."

    if command -v kind >/dev/null 2>&1; then
        KIND_INSTALLED=$(kind version 2>/dev/null | head -1)
        echo "    Kind already installed: ${KIND_INSTALLED}"
        # Check if version matches
        if [[ "$KIND_INSTALLED" == *"${KIND_VERSION}"* ]]; then
            return 0
        fi
        echo "    Updating Kind to ${KIND_VERSION}..."
    fi

    # Install Kind
    curl -Lo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-amd64"
    chmod +x /usr/local/bin/kind

    kind version
    echo "    Kind installed successfully"
}

install_kubectl() {
    echo ">>> [3/4] Installing Kubectl..."

    if command -v kubectl >/dev/null 2>&1; then
        echo "    Kubectl already installed: $(kubectl version --client --short 2>/dev/null || kubectl version --client)"
        return 0
    fi

    # Install Kubectl matching K8s version
    curl -Lo /usr/local/bin/kubectl "https://dl.k8s.io/release/${K8S_VERSION}/bin/linux/amd64/kubectl"
    chmod +x /usr/local/bin/kubectl

    kubectl version --client
    echo "    Kubectl installed successfully"
}

install_jq() {
    echo ">>> [4/4] Installing jq..."

    if command -v jq >/dev/null 2>&1; then
        echo "    jq already installed: $(jq --version)"
        return 0
    fi

    apt-get update -qq
    apt-get install -y -qq jq

    jq --version
    echo "    jq installed successfully"
}

install_dependencies() {
    echo ""
    echo "=================================================="
    echo "Installing Dependencies"
    echo "=================================================="

    install_docker
    install_kind
    install_kubectl
    install_jq

    echo ""
    echo ">>> All dependencies installed successfully!"
}

# ============================================================
# Step 2: Build and Load Docker Images
# ============================================================

build_images() {
    echo ""
    echo "=================================================="
    echo "Building Docker Images"
    echo "=================================================="

    # Build from aiagent directory (adk-go cloned from GitHub in Dockerfile)
    cd "${PROJECT_ROOT}"

    # Build Manager image
    echo ">>> Building aiagent/manager:test..."
    docker build -t aiagent/manager:test \
        -f Dockerfile.manager \
        . || { echo "ERROR: Failed to build manager"; return 1; }

    # Build ADK Framework image (DUMMY container)
    echo ">>> Building aiagent/adk-framework:test..."
    docker build -t aiagent/adk-framework:test \
        -f Dockerfile.adk-framework \
        . || { echo "ERROR: Failed to build adk-framework"; return 1; }

    # Build ADK Handler image
    echo ">>> Building aiagent/adk-handler:test..."
    docker build -t aiagent/adk-handler:test \
        -f Dockerfile.adk-handler \
        . || { echo "ERROR: Failed to build adk-handler"; return 1; }

    # Build OpenClaw Framework image (DUMMY container)
    echo ">>> Building aiagent/openclaw-framework:test..."
    docker build -t aiagent/openclaw-framework:test \
        -f Dockerfile.openclaw-framework \
        . || { echo "ERROR: Failed to build openclaw-framework"; return 1; }

    # Build OpenClaw Handler image
    echo ">>> Building aiagent/openclaw-handler:test..."
    docker build -t aiagent/openclaw-handler:test \
        -f Dockerfile.openclaw-handler \
        . || { echo "ERROR: Failed to build openclaw-handler"; return 1; }

    # Build Config Daemon image
    echo ">>> Building aiagent/config-daemon:test..."
    docker build -t aiagent/config-daemon:test \
        -f Dockerfile.config-daemon \
        . || { echo "ERROR: Failed to build config-daemon"; return 1; }

    echo ""
    echo ">>> All images built successfully!"

    # List built images
    docker images | grep "aiagent/"
}

load_images() {
    echo ""
    echo "=================================================="
    echo "Loading Images into Kind Cluster"
    echo "=================================================="

    kind load docker-image aiagent/manager:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load manager"; return 1; }

    kind load docker-image aiagent/adk-framework:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load adk-framework"; return 1; }

    kind load docker-image aiagent/adk-handler:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load adk-handler"; return 1; }

    kind load docker-image aiagent/openclaw-framework:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load openclaw-framework"; return 1; }

    kind load docker-image aiagent/openclaw-handler:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load openclaw-handler"; return 1; }

    kind load docker-image aiagent/config-daemon:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load config-daemon"; return 1; }

    echo ">>> All images loaded into Kind cluster!"
}

# ============================================================
# Step 3: Create Kind Cluster
# ============================================================

create_kind_cluster() {
    echo ""
    echo "=================================================="
    echo "Creating Kind Cluster (K8s ${K8S_VERSION})"
    echo "=================================================="

    # Check if cluster already exists
    if kind get clusters 2>/dev/null | grep -q "${KIND_CLUSTER_NAME}"; then
        echo "    Cluster '${KIND_CLUSTER_NAME}' already exists"
        read -p "    Delete and recreate? [y/N]: " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            kind delete cluster --name "${KIND_CLUSTER_NAME}"
        else
            echo "    Using existing cluster"
            return 0
        fi
    fi

    # Create Kind cluster config
    # K8s 1.35: ImageVolume requires feature gate
    cat > /tmp/kind-config-aiagent.yaml <<EOF
# Kind Cluster Configuration for AIAgent E2E Tests
# Kind v${KIND_VERSION} + K8s ${K8S_VERSION}
# ImageVolume feature gate enabled
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ${KIND_CLUSTER_NAME}
featureGates:
  ImageVolume: true
nodes:
- role: control-plane
  image: kindest/node:${K8S_VERSION}
  kubeadmConfigPatches:
  - |
    {
      "kind": "ClusterConfiguration",
      "apiVersion": "kubeadm.k8s.io/v1beta4",
      "featureGates": {
        "ImageVolume": true
      }
    }
  - |
    {
      "kind": "InitConfiguration",
      "apiVersion": "kubeadm.k8s.io/v1beta4",
      "featureGates": {
        "ImageVolume": true
      }
    }
- role: worker
  image: kindest/node:${K8S_VERSION}
  kubeadmConfigPatches:
  - |
    {
      "kind": "JoinConfiguration",
      "apiVersion": "kubeadm.k8s.io/v1beta4",
      "featureGates": {
        "ImageVolume": true
      }
    }
- role: worker
  image: kindest/node:${K8S_VERSION}
  kubeadmConfigPatches:
  - |
    {
      "kind": "JoinConfiguration",
      "apiVersion": "kubeadm.k8s.io/v1beta4",
      "featureGates": {
        "ImageVolume": true
      }
    }
EOF

    echo ">>> Creating Kind cluster..."
    kind create cluster \
        --name "${KIND_CLUSTER_NAME}" \
        --config /tmp/kind-config-aiagent.yaml \
        --wait 180s

    echo ">>> Kind cluster created successfully!"

    # Verify cluster
    kubectl cluster-info
    kubectl get nodes
}

# ============================================================
# Step 4: Install CRDs and Deploy Manager
# ============================================================

install_crds() {
    echo ""
    echo "=================================================="
    echo "Installing CRDs"
    echo "=================================================="

    cd "${PROJECT_ROOT}"

    # Check if CRD files exist
    CRD_DIR="${PROJECT_ROOT}/config/crd/bases"
    if [ ! -d "${CRD_DIR}" ]; then
        echo ">>> Generating CRDs with controller-gen..."
        # Install controller-gen if needed
        if ! command -v controller-gen >/dev/null 2>&1; then
            go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
        fi
        controller-gen rbac:roleName=manager-role crd webhook paths="./api/..." output:crd:artifacts:config="${CRD_DIR}"
    fi

    # Apply CRDs
    echo ">>> Applying CRDs..."
    kubectl apply -f "${CRD_DIR}/" --wait=true

    # Wait for CRDs to be established
    kubectl wait --for condition=established \
        --timeout=60s \
        crd/agentruntimes.agent.ai || true
    kubectl wait --for condition=established \
        --timeout=60s \
        crd/aiagents.agent.ai || true
    kubectl wait --for condition=established \
        --timeout=60s \
        crd/harnesses.agent.ai || true

    echo ">>> CRDs installed successfully!"

    kubectl get crd | grep agent
}

deploy_manager() {
    echo ""
    echo "=================================================="
    echo "Deploying AIAgent Manager"
    echo "=================================================="

    # Create namespace
    kubectl create namespace aiagent-system --dry-run=client -o yaml | kubectl apply -f -

    # Deploy manager
    kubectl apply -f "${SCRIPT_DIR}/manifests/manager-deployment.yaml"

    # Wait for manager to be ready
    kubectl wait --for condition=available \
        --timeout=120s \
        deployment/aiagent-manager \
        -n aiagent-system

    echo ">>> Manager deployed successfully!"

    kubectl get pods -n aiagent-system
}

deploy_config_daemon() {
    echo ""
    echo "=================================================="
    echo "Deploying Config Daemon"
    echo "=================================================="

    # Check if Config Daemon is already running
    READY_COUNT=$(kubectl get daemonset config-daemon -n aiagent-system -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
    DESIRED_COUNT=$(kubectl get daemonset config-daemon -n aiagent-system -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")

    if [ "$READY_COUNT" == "$DESIRED_COUNT" ] && [ "$READY_COUNT" -gt 0 ]; then
        echo ">>> Config Daemon already running (${READY_COUNT}/${DESIRED_COUNT} pods ready)"
        kubectl get pods -n aiagent-system -l app=config-daemon
        return 0
    fi

    # Deploy config daemon
    kubectl apply -f "${SCRIPT_DIR}/manifests/config-daemon-deployment.yaml"

    # Wait for config daemon to be ready
    echo ">>> Waiting for Config Daemon DaemonSet..."
    kubectl wait --for condition=ready \
        --timeout=120s \
        daemonset/config-daemon \
        -n aiagent-system || true

    echo ">>> Config Daemon deployed successfully!"

    kubectl get pods -n aiagent-system -l app=config-daemon
}

# ============================================================
# Step 5: Run E2E Tests
# ============================================================

verify_adk_shared() {
    echo ""
    echo "    [Verify] ADK Shared Mode..."
    echo "    ----------------------------------------"

    POD_NAME="adk-shared-runtime-runtime"
    NS="aiagent-system"

    # Check AgentRuntime is running
    RUNTIME_STATUS=$(kubectl get agentruntime adk-shared-runtime -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$RUNTIME_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: AgentRuntime status is '$RUNTIME_STATUS', expected 'Running'"
        return 1
    fi
    echo "    ✓ AgentRuntime phase: Running"

    # Check multiple AIAgents
    AGENT_COUNT=$(kubectl get aiagent -l runtime=adk-shared-runtime -n ${NS} -o json 2>/dev/null | jq '.items | length' || echo "0")
    if [ "$AGENT_COUNT" -lt 2 ]; then
        echo "    ❌ ERROR: Expected at least 2 agents, got ${AGENT_COUNT}"
        return 1
    fi
    echo "    ✓ AIAgent count: ${AGENT_COUNT}"

    # Check Pod structure
    POD_STATUS=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$POD_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: Pod status is '$POD_STATUS'"
        return 1
    fi
    echo "    ✓ Pod phase: Running"

    # Check ImageVolume (K8s 1.35+ format)
    IMAGE_VOLUME=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image}' 2>/dev/null)
    if [ "$IMAGE_VOLUME" == "" ]; then
        echo "    ❌ ERROR: ImageVolume not configured"
        return 1
    fi
    echo "    ✓ ImageVolume configured: ${IMAGE_VOLUME}"

    # Check ImageVolume.PullPolicy
    PULL_POLICY=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image.pullPolicy}' 2>/dev/null)
    if [ "$PULL_POLICY" != "IfNotPresent" ]; then
        echo "    ❌ ERROR: ImageVolume pullPolicy should be 'IfNotPresent', got '${PULL_POLICY}'"
        return 1
    fi
    echo "    ✓ ImageVolume pullPolicy: IfNotPresent"

    # Check DUMMY Framework container
    FW_CMD=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.containers[?(@.name=="agent-framework")].command[0]}' 2>/dev/null)
    if [ "$FW_CMD" != "sleep" ]; then
        echo "    ❌ ERROR: Framework container should have 'sleep' command (DUMMY)"
        return 1
    fi
    echo "    ✓ Framework container: DUMMY (sleep infinity)"

    # Check ShareProcessNamespace
    SHARE_PID=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.shareProcessNamespace}' 2>/dev/null)
    if [ "$SHARE_PID" != "true" ]; then
        echo "    ❌ ERROR: ShareProcessNamespace should be true"
        return 1
    fi
    echo "    ✓ ShareProcessNamespace: true"

    echo "    ----------------------------------------"
    echo "    ✅ ADK Shared Mode: PASS"
    return 0
}

verify_adk_isolated() {
    echo ""
    echo "    [Verify] ADK Isolated Mode..."
    echo "    ----------------------------------------"

    POD_NAME="adk-isolated-runtime-runtime"
    NS="aiagent-system"

    # Check AgentRuntime is running
    RUNTIME_STATUS=$(kubectl get agentruntime adk-isolated-runtime -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$RUNTIME_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: AgentRuntime status is '$RUNTIME_STATUS', expected 'Running'"
        return 1
    fi
    echo "    ✓ AgentRuntime phase: Running"

    # Check multiple AIAgents
    AGENT_COUNT=$(kubectl get aiagent -l runtime=adk-isolated-runtime -n ${NS} -o json 2>/dev/null | jq '.items | length' || echo "0")
    if [ "$AGENT_COUNT" -lt 2 ]; then
        echo "    ❌ ERROR: Expected at least 2 agents, got ${AGENT_COUNT}"
        return 1
    fi
    echo "    ✓ AIAgent count: ${AGENT_COUNT}"

    # Check Pod
    POD_STATUS=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$POD_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: Pod status is '$POD_STATUS'"
        return 1
    fi
    echo "    ✓ Pod phase: Running"

    # Check ImageVolume
    IMAGE_VOLUME=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image}' 2>/dev/null)
    if [ "$IMAGE_VOLUME" == "" ]; then
        echo "    ❌ ERROR: ImageVolume not configured"
        return 1
    fi
    echo "    ✓ ImageVolume configured"

    # Check ImageVolume.PullPolicy
    PULL_POLICY=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image.pullPolicy}' 2>/dev/null)
    if [ "$PULL_POLICY" != "IfNotPresent" ]; then
        echo "    ❌ ERROR: ImageVolume pullPolicy should be 'IfNotPresent'"
        return 1
    fi
    echo "    ✓ ImageVolume pullPolicy: IfNotPresent"

    echo "    ----------------------------------------"
    echo "    ✅ ADK Isolated Mode: PASS"
    return 0
}

verify_openclaw() {
    echo ""
    echo "    [Verify] OpenClaw Multiple Gateway Mode..."
    echo "    ----------------------------------------"

    POD_NAME="openclaw-runtime-runtime"
    NS="aiagent-system"

    # Check AgentRuntime is running
    RUNTIME_STATUS=$(kubectl get agentruntime openclaw-runtime -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$RUNTIME_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: AgentRuntime status is '$RUNTIME_STATUS', expected 'Running'"
        return 1
    fi
    echo "    ✓ AgentRuntime phase: Running"

    # Check exactly 2 AIAgents
    AGENT_COUNT=$(kubectl get aiagent -l runtime=openclaw-runtime -n ${NS} -o json 2>/dev/null | jq '.items | length' || echo "0")
    if [ "$AGENT_COUNT" != 2 ]; then
        echo "    ❌ ERROR: Expected 2 AIAgent CRDs (openclaw-1, openclaw-2), got ${AGENT_COUNT}"
        return 1
    fi
    echo "    ✓ AIAgent count: ${AGENT_COUNT}"

    # Check agent names
    AGENT_1=$(kubectl get aiagent openclaw-1 -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    AGENT_2=$(kubectl get aiagent openclaw-2 -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    if [ "$AGENT_1" != "openclaw-1" ] || [ "$AGENT_2" != "openclaw-2" ]; then
        echo "    ❌ ERROR: Expected AIAgent names openclaw-1 and openclaw-2"
        return 1
    fi
    echo "    ✓ AIAgent names: openclaw-1, openclaw-2"

    # Check Pod
    POD_STATUS=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$POD_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: Pod status is '$POD_STATUS'"
        return 1
    fi
    echo "    ✓ Pod phase: Running"

    # Check ImageVolume
    IMAGE_VOLUME=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image}' 2>/dev/null)
    if [ "$IMAGE_VOLUME" == "" ]; then
        echo "    ❌ ERROR: ImageVolume not configured"
        return 1
    fi
    echo "    ✓ ImageVolume configured"

    # Check ImageVolume.PullPolicy
    PULL_POLICY=$(kubectl get pod ${POD_NAME} -n ${NS} -o jsonpath='{.spec.volumes[?(@.name=="framework-image")].image.pullPolicy}' 2>/dev/null)
    if [ "$PULL_POLICY" != "IfNotPresent" ]; then
        echo "    ❌ ERROR: ImageVolume pullPolicy should be 'IfNotPresent'"
        return 1
    fi
    echo "    ✓ ImageVolume pullPolicy: IfNotPresent"

    echo "    ----------------------------------------"
    echo "    ✅ OpenClaw Multiple Gateway Mode: PASS"
    return 0
}

run_tests() {
    echo ""
    echo "=================================================="
    echo "Running E2E Tests"
    echo "=================================================="

    TEST_PASS=0
    TEST_FAIL=0

    # Test 1: ADK Shared Mode
    echo ""
    echo ">>> Test 1: ADK Shared Process Mode"
    echo "    (Multiple AIAgent CRDs → Single Framework Process)"
    kubectl apply -f "${SCRIPT_DIR}/manifests/adk-shared-test.yaml"

    # Wait for AgentRuntime to be processed by controller
    echo "    Waiting for AgentRuntime to be ready..."
    kubectl wait --for=jsonpath='{.status.phase}'=Running agentruntime/adk-shared-runtime -n aiagent-system --timeout=120s || true
    sleep 10

    if verify_adk_shared; then
        TEST_PASS=$((TEST_PASS + 1))
    else
        TEST_FAIL=$((TEST_FAIL + 1))
    fi

    # Test 2: ADK Isolated Mode
    echo ""
    echo ">>> Test 2: ADK Isolated Process Mode"
    echo "    (Each AIAgent CRD → Separate Framework Process)"
    kubectl apply -f "${SCRIPT_DIR}/manifests/adk-isolated-test.yaml"

    # Wait for AgentRuntime to be processed by controller
    echo "    Waiting for AgentRuntime to be ready..."
    kubectl wait --for=jsonpath='{.status.phase}'=Running agentruntime/adk-isolated-runtime -n aiagent-system --timeout=120s || true
    sleep 10

    if verify_adk_isolated; then
        TEST_PASS=$((TEST_PASS + 1))
    else
        TEST_FAIL=$((TEST_FAIL + 1))
    fi

    # Test 3: OpenClaw Multiple Gateway Mode
    echo ""
    echo ">>> Test 3: OpenClaw Multiple Gateway Mode"
    echo "    (Each AIAgent CRD → Gateway Process with internal agents)"
    kubectl apply -f "${SCRIPT_DIR}/manifests/openclaw-gateway-test.yaml"

    # Wait for AgentRuntime to be processed by controller
    echo "    Waiting for AgentRuntime to be ready..."
    kubectl wait --for=jsonpath='{.status.phase}'=Running agentruntime/openclaw-runtime -n aiagent-system --timeout=120s || true
    sleep 10

    if verify_openclaw; then
        TEST_PASS=$((TEST_PASS + 1))
    else
        TEST_FAIL=$((TEST_FAIL + 1))
    fi

    # Summary
    echo ""
    echo "=================================================="
    echo "Test Summary"
    echo "=================================================="
    echo "    Passed: ${TEST_PASS}"
    echo "    Failed: ${TEST_FAIL}"
    echo "=================================================="

    if [ "$TEST_FAIL" -gt 0 ]; then
        echo "    ❌ Some tests failed"
        return 1
    else
        echo "    ✅ All tests passed!"
        return 0
    fi
}

# ============================================================
# Step 6: Cleanup
# ============================================================

cleanup() {
    echo ""
    echo "=================================================="
    echo "Cleanup"
    echo "=================================================="

    echo ">>> Deleting Kind cluster..."
    kind delete cluster --name "${KIND_CLUSTER_NAME}" || true

    echo ">>> Cleanup complete!"
}

# ============================================================
# Main Execution
# ============================================================

show_status() {
    echo ""
    echo "=================================================="
    echo "Cluster Status"
    echo "=================================================="

    echo ">>> Kubernetes Nodes:"
    kubectl get nodes

    echo ""
    echo ">>> CRDs:"
    kubectl get crd | grep agent || echo "    No agent CRDs found"

    echo ""
    echo ">>> Manager Pod:"
    kubectl get pods -n aiagent-system

    echo ""
    echo ">>> AgentRuntimes:"
    kubectl get agentruntime -n aiagent-system || echo "    No AgentRuntimes found"

    echo ""
    echo ">>> AIAgents:"
    kubectl get aiagent -n aiagent-system || echo "    No AIAgents found"

    echo ""
    echo ">>> All Pods:"
    kubectl get pods -A | grep -E "aiagent|runtime"
}

case "${1:-all}" in
    "install")
        install_dependencies
        ;;
    "cluster")
        create_kind_cluster
        ;;
    "build")
        build_images
        load_images
        ;;
    "deploy")
        install_crds
        deploy_manager
        deploy_config_daemon
        ;;
    "test")
        run_tests
        ;;
    "status")
        show_status
        ;;
    "cleanup")
        cleanup
        ;;
    "all")
        install_dependencies
        create_kind_cluster
        build_images
        load_images
        install_crds
        deploy_manager
        deploy_config_daemon
        run_tests
        show_status
        ;;
    *)
        echo "Usage: $0 {install|cluster|build|deploy|test|status|cleanup|all}"
        echo ""
        echo "Commands:"
        echo "  install   - Install Docker, Kind, Kubectl, jq"
        echo "  cluster   - Create Kind cluster (K8s ${K8S_VERSION})"
        echo "  build     - Build and load Docker images"
        echo "  deploy    - Install CRDs and deploy manager + config daemon"
        echo "  test      - Run E2E tests"
        echo "  status    - Show cluster status"
        echo "  cleanup   - Delete Kind cluster"
        echo "  all       - Full setup and test (default)"
        exit 1
        ;;
esac

echo ""
echo "=================================================="
echo "Done"
echo "=================================================="