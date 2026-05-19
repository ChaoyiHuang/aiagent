#!/bin/bash
# OpenClaw Discord E2E Test - Complete Setup and Deployment Script
# Creates Kind cluster, installs all dependencies, and deploys OpenClaw Discord bot
#
# Version Configuration:
#   - Kind: v0.31.0
#   - Kubernetes: v1.35.0
#   - ImageVolume: enabled via feature gate in K8s 1.35
#
# Environment Variables (required):
#   DISCORD_BOT_TOKEN    - Discord bot token from Developer Portal
#   DISCORD_USER_IDS     - Comma-separated list of Discord user IDs (whitelist)
#   DEEPSEEK_API_KEY     - DeepSeek API key for LLM
#   DISCORD_GUILD_ID     - (optional) Discord server ID to restrict bot
#   DISCORD_COMMAND_PREFIX - (optional) Bot command prefix, default: !
#
# Usage:
#   ./run-e2e-test-discord.sh          # Full setup + Discord deployment
#   ./run-e2e-test-discord.sh deploy   # Only deploy (assuming setup done)
#   ./run-e2e-test-discord.sh status   # Show deployment status
#   ./run-e2e-test-discord.sh cleanup  # Cleanup cluster

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$(dirname "$SCRIPT_DIR")")")"
KIND_CLUSTER_NAME="aiagent-discord-test"
K8S_VERSION="v1.35.0"
KIND_VERSION="v0.31.0"
NS="aiagent-system"

echo "=================================================="
echo "OpenClaw Discord E2E Test"
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

    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
    sh /tmp/get-docker.sh
    systemctl start docker
    systemctl enable docker
    docker --version
    echo "    Docker installed successfully"
}

install_kind() {
    echo ">>> [2/4] Installing Kind v${KIND_VERSION}..."

    if command -v kind >/dev/null 2>&1; then
        KIND_INSTALLED=$(kind version 2>/dev/null | head -1)
        echo "    Kind already installed: ${KIND_INSTALLED}"
        if [[ "$KIND_INSTALLED" == *"${KIND_VERSION}"* ]]; then
            return 0
        fi
        echo "    Updating Kind to ${KIND_VERSION}..."
    fi

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

    cd "${PROJECT_ROOT}"

    # Build Manager image
    echo ">>> Building aiagent/manager:test..."
    docker build -t aiagent/manager:test \
        -f Dockerfile.manager \
        . || { echo "ERROR: Failed to build manager"; return 1; }

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
    docker images | grep "aiagent/"
}

load_images() {
    echo ""
    echo "=================================================="
    echo "Loading Images into Kind Cluster"
    echo "=================================================="

    kind load docker-image aiagent/manager:test \
        --name "${KIND_CLUSTER_NAME}" || { echo "ERROR: Failed to load manager"; return 1; }

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

    cat > /tmp/kind-config-discord.yaml <<EOF
# Kind Cluster Configuration for OpenClaw Discord E2E Test
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
EOF

    echo ">>> Creating Kind cluster..."
    kind create cluster \
        --name "${KIND_CLUSTER_NAME}" \
        --config /tmp/kind-config-discord.yaml \
        --wait 180s

    echo ">>> Kind cluster created successfully!"
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

    CRD_DIR="${PROJECT_ROOT}/config/crd/bases"
    if [ ! -d "${CRD_DIR}" ]; then
        echo ">>> Generating CRDs with controller-gen..."
        if ! command -v controller-gen >/dev/null 2>&1; then
            go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
        fi
        controller-gen rbac:roleName=manager-role crd webhook paths="./api/..." output:crd:artifacts:config="${CRD_DIR}"
    fi

    echo ">>> Applying CRDs..."
    kubectl apply -f "${CRD_DIR}/" --wait=true

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

    kubectl create namespace aiagent-system --dry-run=client -o yaml | kubectl apply -f -
    kubectl apply -f "${SCRIPT_DIR}/manifests/manager-deployment.yaml"

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

    READY_COUNT=$(kubectl get daemonset config-daemon -n aiagent-system -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
    DESIRED_COUNT=$(kubectl get daemonset config-daemon -n aiagent-system -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")

    if [ "$READY_COUNT" == "$DESIRED_COUNT" ] && [ "$READY_COUNT" -gt 0 ]; then
        echo ">>> Config Daemon already running (${READY_COUNT}/${DESIRED_COUNT} pods ready)"
        kubectl get pods -n aiagent-system -l app=config-daemon
        return 0
    fi

    kubectl apply -f "${SCRIPT_DIR}/manifests/config-daemon-deployment.yaml"

    echo ">>> Waiting for Config Daemon DaemonSet..."
    kubectl wait --for condition=ready \
        --timeout=120s \
        daemonset/config-daemon \
        -n aiagent-system || true

    echo ">>> Config Daemon deployed successfully!"
    kubectl get pods -n aiagent-system -l app=config-daemon
}

# ============================================================
# Step 5: Collect Credentials
# ============================================================

collect_credentials() {
    echo ""
    echo "=================================================="
    echo "Collecting Discord Credentials"
    echo "=================================================="

    if [ -n "$DISCORD_BOT_TOKEN" ] && [ -n "$DISCORD_USER_IDS" ] && [ -n "$DEEPSEEK_API_KEY" ]; then
        echo ""
        echo ">>> Using environment variables (non-interactive mode)"
        DISCORD_TOKEN="$DISCORD_BOT_TOKEN"
        echo "    ✓ DISCORD_BOT_TOKEN: set"
        echo "    ✓ DISCORD_USER_IDS: ${DISCORD_USER_IDS}"
        DEEPSEEK_KEY="$DEEPSEEK_API_KEY"
        echo "    ✓ DEEPSEEK_API_KEY: set"

        if [ -n "$DISCORD_GUILD_ID" ]; then
            DISCORD_GUILD_YAML="        allowedGuilds:\n        - \"${DISCORD_GUILD_ID}\""
            echo "    ✓ DISCORD_GUILD_ID: ${DISCORD_GUILD_ID}"
        else
            DISCORD_GUILD_YAML=""
        fi

        COMMAND_PREFIX="${DISCORD_COMMAND_PREFIX:-!}"
        echo "    ✓ DISCORD_COMMAND_PREFIX: ${COMMAND_PREFIX}"
        return 0
    fi

    echo ""
    echo ">>> ERROR: Required environment variables not set"
    echo ""
    echo "    Required:"
    echo "      DISCORD_BOT_TOKEN    - Discord bot token"
    echo "      DISCORD_USER_IDS     - Comma-separated user IDs whitelist"
    echo "      DEEPSEEK_API_KEY     - DeepSeek API key"
    echo ""
    echo "    Optional:"
    echo "      DISCORD_GUILD_ID     - Server ID to restrict bot"
    echo "      DISCORD_COMMAND_PREFIX - Command prefix (default: !)"
    echo ""
    echo "    Example:"
    echo "      export DISCORD_BOT_TOKEN='your-bot-token'"
    echo "      export DISCORD_USER_IDS='123456789,987654321'"
    echo "      export DEEPSEEK_API_KEY='your-api-key'"
    echo ""
    exit 1
}

# ============================================================
# Step 6: Generate and Deploy Discord Config
# ============================================================

generate_config() {
    echo ""
    echo ">>> Generating configuration files..."

    TEMP_DIR="/tmp/discord-deploy-$(date +%s)"
    mkdir -p "$TEMP_DIR"

    # Generate Secrets
    cat > "${TEMP_DIR}/secrets.yaml" <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  namespace: aiagent-system
  name: discord-bot-token
type: Opaque
stringData:
  token: "${DISCORD_TOKEN}"

---
apiVersion: v1
kind: Secret
metadata:
  namespace: aiagent-system
  name: deepseek-api-key
type: Opaque
stringData:
  api-key: "${DEEPSEEK_KEY}"
EOF

    # Generate Harness
    cat > "${TEMP_DIR}/harness.yaml" <<EOF
---
apiVersion: agent.ai/v1
kind: Harness
metadata:
  namespace: aiagent-system
  name: discord-deepseek-model
spec:
  type: model
  model:
    provider: deepseek
    endpoint: https://api.deepseek.com/v1
    authSecretRef: deepseek-api-key
    defaultModel: deepseek-chat
    models:
    - name: deepseek-chat
      allowed: true
      contextWindow: 164000
    - name: deepseek-coder
      allowed: true
      contextWindow: 164000

---
apiVersion: agent.ai/v1
kind: Harness
metadata:
  namespace: aiagent-system
  name: discord-skills
spec:
  type: skills
  skills:
    hubType: builtin
    skills:
    - name: chat
      version: "1.0"
      allowed: true
    - name: search
      version: "1.0"
      allowed: true

---
apiVersion: agent.ai/v1
kind: Harness
metadata:
  namespace: aiagent-system
  name: discord-memory
spec:
  type: memory
  memory:
    type: inmemory
    ttl: 7200
EOF

    # Generate AgentRuntime
    cat > "${TEMP_DIR}/runtime.yaml" <<EOF
---
apiVersion: agent.ai/v1
kind: AgentRuntime
metadata:
  namespace: aiagent-system
  name: discord-runtime
spec:
  processMode: isolated
  agentHandler:
    image: aiagent/openclaw-handler:test
    env:
    - name: BASE_GATEWAY_PORT
      value: "18800"
    - name: WORK_DIR
      value: "/shared/workdir"
    - name: CONFIG_DIR
      value: "/shared/config"
  agentFramework:
    image: aiagent/openclaw-framework:test
    type: openclaw
  harness:
  - name: discord-deepseek-model
    namespace: aiagent-system
  - name: discord-skills
    namespace: aiagent-system
  - name: discord-memory
    namespace: aiagent-system
  replicas: 1
EOF

    # Generate AIAgent
    cat > "${TEMP_DIR}/agent.yaml" <<EOF
---
apiVersion: agent.ai/v1
kind: AIAgent
metadata:
  namespace: aiagent-system
  name: discord-bot-1
  labels:
    runtime: discord-runtime
spec:
  description: "Discord Bot powered by DeepSeek"
  runtimeRef:
    type: openclaw
    name: discord-runtime
  agentConfig:
    gateway:
      port: 18800
      bind: "loopback"
      auth:
        mode: "none"
    models:
      mergeModels: true
    channels:
      discord:
        enabled: true
        tokenSecretRef: discord-bot-token
        dmPolicy: "all"
        mentionRequired: false
    agents:
      list:
      - id: "discord-assistant"
        name: "Discord Assistant"
        skills: ["chat", "search"]
EOF

    echo "    ✓ Configuration files generated in ${TEMP_DIR}"
}

deploy_discord() {
    echo ""
    echo "=================================================="
    echo "Deploying Discord OpenClaw Instance"
    echo "=================================================="

    echo "    Applying secrets..."
    kubectl apply -f "${TEMP_DIR}/secrets.yaml"

    echo "    Applying harness CRDs..."
    kubectl apply -f "${TEMP_DIR}/harness.yaml"

    echo "    Applying agentruntime..."
    kubectl apply -f "${TEMP_DIR}/runtime.yaml"

    echo "    Applying aiagent..."
    kubectl apply -f "${TEMP_DIR}/agent.yaml"

    echo ""
    echo ">>> Waiting for deployment..."

    echo "    Waiting for AgentRuntime to be ready..."
    kubectl wait --for=jsonpath='{.status.phase}'=Running agentruntime/discord-runtime -n ${NS} --timeout=120s || {
        echo "    ⚠ AgentRuntime not ready after 120s"
        echo "    Check logs: kubectl logs -n ${NS} -l app=aiagent-manager"
    }

    echo "    Waiting for Pod to be running..."
    sleep 5
    kubectl wait --for=condition=Ready pod -l runtime=discord-runtime -n ${NS} --timeout=60s || {
        echo "    ⚠ Pod not ready after 60s"
        kubectl get pods -n ${NS} -l runtime=discord-runtime
    }

    echo ""
    echo ">>> Deployment Status"
    kubectl get agentruntime discord-runtime -n ${NS}
    kubectl get aiagent discord-bot-1 -n ${NS}
    kubectl get pods -n ${NS} -l runtime=discord-runtime
}

# ============================================================
# Step 7: Verify Deployment
# ============================================================

verify_discord() {
    echo ""
    echo "=================================================="
    echo "Verifying Discord Deployment"
    echo "=================================================="

    POD_NAME="discord-runtime-runtime"
    NS="aiagent-system"

    echo ""
    echo "    [Verify] Discord OpenClaw Instance..."
    echo "    ----------------------------------------"

    # Check AgentRuntime is running
    RUNTIME_STATUS=$(kubectl get agentruntime discord-runtime -n ${NS} -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "$RUNTIME_STATUS" != "Running" ]; then
        echo "    ❌ ERROR: AgentRuntime status is '$RUNTIME_STATUS', expected 'Running'"
        return 1
    fi
    echo "    ✓ AgentRuntime phase: Running"

    # Check AIAgent exists
    AGENT_NAME=$(kubectl get aiagent discord-bot-1 -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    if [ "$AGENT_NAME" != "discord-bot-1" ]; then
        echo "    ❌ ERROR: AIAgent discord-bot-1 not found"
        return 1
    fi
    echo "    ✓ AIAgent: discord-bot-1"

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

    # Check Secrets
    SECRET_TOKEN=$(kubectl get secret discord-bot-token -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    SECRET_API=$(kubectl get secret deepseek-api-key -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
    if [ "$SECRET_TOKEN" != "discord-bot-token" ]; then
        echo "    ❌ ERROR: Secret discord-bot-token not found"
        return 1
    fi
    echo "    ✓ Secret discord-bot-token: exists"
    if [ "$SECRET_API" != "deepseek-api-key" ]; then
        echo "    ❌ ERROR: Secret deepseek-api-key not found"
        return 1
    fi
    echo "    ✓ Secret deepseek-api-key: exists"

    # Check Harness CRDs
    for harness in discord-deepseek-model discord-skills discord-memory; do
        H=$(kubectl get harness ${harness} -n ${NS} -o jsonpath='{.metadata.name}' 2>/dev/null || echo "")
        if [ "$H" != "${harness}" ]; then
            echo "    ❌ ERROR: Harness ${harness} not found"
            return 1
        fi
        echo "    ✓ Harness ${harness}: exists"
    done

    echo "    ----------------------------------------"
    echo "    ✅ Discord Deployment: PASS"
    return 0
}

# ============================================================
# Step 8: Show Usage Guide
# ============================================================

show_usage() {
    echo ""
    echo "=================================================="
    echo "Discord Bot Usage Guide"
    echo "=================================================="

    echo ""
    echo ">>> Your Discord Bot is now deployed!"
    echo ""
    echo ">>> To use the bot in Discord:"
    echo "    1. Add the bot to your Discord server:"
    echo "       - Go to Discord Developer Portal"
    echo "       - OAuth2 > URL Generator"
    echo "       - Select 'bot' scope"
    echo "       - Copy and open the invite link"
    echo ""
    echo "    2. Interact with the bot:"
    echo "       - Use command prefix: ${COMMAND_PREFIX}"
    echo "       - Example: ${COMMAND_PREFIX}help"
    echo "       - Example: ${COMMAND_PREFIX}chat Hello!"
    echo ""
    echo ">>> Whitelist Settings:"
    if [ -n "$DISCORD_USER_IDS" ]; then
        echo "    - Only these users can interact with the bot:"
        for id in $(echo "$DISCORD_USER_IDS" | tr ',' ' '); do
            echo "      User ID: ${id}"
        done
    else
        echo "    - All users can interact (no whitelist)"
    fi
    if [ -n "$DISCORD_GUILD_ID" ]; then
        echo "    - Bot restricted to guild: ${DISCORD_GUILD_ID}"
    else
        echo "    - Bot works in all guilds/servers"
    fi

    echo ""
    echo ">>> Troubleshooting:"
    echo "    Check bot logs:"
    echo "      kubectl logs -n ${NS} ${POD_NAME} -c agent-handler"
    echo ""
    echo "    Check manager logs:"
    echo "      kubectl logs -n ${NS} deployment/aiagent-manager"
    echo ""
    echo "    Restart the bot:"
    echo "      kubectl rollout restart deployment/aiagent-manager -n ${NS}"
    echo ""
    echo "    Show status:"
    echo "      ${SCRIPT_DIR}/run-e2e-test-discord.sh status"
    echo ""
    echo "    Cleanup cluster:"
    echo "      ${SCRIPT_DIR}/run-e2e-test-discord.sh cleanup"
}

# ============================================================
# Step 9: Cleanup
# ============================================================

cleanup() {
    echo ""
    echo "=================================================="
    echo "Cleanup"
    echo "=================================================="

    echo ">>> Deleting Discord resources..."
    kubectl delete aiagent discord-bot-1 -n aiagent-system 2>/dev/null || true
    kubectl delete agentruntime discord-runtime -n aiagent-system 2>/dev/null || true
    kubectl delete harness discord-deepseek-model -n aiagent-system 2>/dev/null || true
    kubectl delete harness discord-skills -n aiagent-system 2>/dev/null || true
    kubectl delete harness discord-memory -n aiagent-system 2>/dev/null || true
    kubectl delete secret discord-bot-token -n aiagent-system 2>/dev/null || true
    kubectl delete secret deepseek-api-key -n aiagent-system 2>/dev/null || true

    echo ">>> Deleting Kind cluster..."
    kind delete cluster --name "${KIND_CLUSTER_NAME}" || true

    echo ">>> Cleanup complete!"
}

# ============================================================
# Status Display
# ============================================================

show_status() {
    echo ""
    echo "=================================================="
    echo "Cluster and Discord Status"
    echo "=================================================="

    echo ""
    echo ">>> Kubernetes Nodes:"
    kubectl get nodes

    echo ""
    echo ">>> CRDs:"
    kubectl get crd | grep agent || echo "    No agent CRDs found"

    echo ""
    echo ">>> Manager Pod:"
    kubectl get pods -n aiagent-system

    echo ""
    echo ">>> Discord Resources:"
    kubectl get agentruntime discord-runtime -n aiagent-system 2>/dev/null || echo "    AgentRuntime: Not deployed"
    kubectl get aiagent discord-bot-1 -n aiagent-system 2>/dev/null || echo "    AIAgent: Not deployed"
    kubectl get pods -n aiagent-system -l runtime=discord-runtime 2>/dev/null || echo "    Pods: None running"

    echo ""
    echo ">>> Secrets:"
    kubectl get secret discord-bot-token -n aiagent-system 2>/dev/null || echo "    discord-bot-token: Not created"
    kubectl get secret deepseek-api-key -n aiagent-system 2>/dev/null || echo "    deepseek-api-key: Not created"

    echo ""
    echo ">>> Harness CRDs:"
    kubectl get harness discord-deepseek-model -n aiagent-system 2>/dev/null || echo "    discord-deepseek-model: Not created"
    kubectl get harness discord-skills -n aiagent-system 2>/dev/null || echo "    discord-skills: Not created"
    kubectl get harness discord-memory -n aiagent-system 2>/dev/null || echo "    discord-memory: Not created"

    echo ""
    echo "=================================================="
}

# ============================================================
# Main Execution
# ============================================================

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
        collect_credentials
        generate_config
        deploy_discord
        verify_discord
        show_usage
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
        collect_credentials
        generate_config
        deploy_discord
        verify_discord
        show_usage
        ;;
    *)
        echo "Usage: $0 {install|cluster|build|deploy|status|cleanup|all}"
        echo ""
        echo "Commands:"
        echo "  install   - Install Docker, Kind, Kubectl, jq"
        echo "  cluster   - Create Kind cluster (K8s ${K8S_VERSION})"
        echo "  build     - Build and load Docker images"
        echo "  deploy    - Install CRDs, deploy manager, and deploy Discord bot"
        echo "  status    - Show cluster and Discord status"
        echo "  cleanup   - Delete Discord resources and Kind cluster"
        echo "  all       - Full setup and Discord deployment (default)"
        echo ""
        echo "Required Environment Variables:"
        echo "  DISCORD_BOT_TOKEN     - Discord bot token"
        echo "  DISCORD_USER_IDS      - Comma-separated user IDs whitelist"
        echo "  DEEPSEEK_API_KEY      - DeepSeek API key"
        echo ""
        echo "Optional Environment Variables:"
        echo "  DISCORD_GUILD_ID      - Server ID to restrict bot"
        echo "  DISCORD_COMMAND_PREFIX - Command prefix (default: !)"
        exit 1
        ;;
esac

echo ""
echo "=================================================="
echo "Done"
echo "=================================================="