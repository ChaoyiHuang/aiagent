# Makefile for AI Agent Kubernetes Controller

# Detect OS
OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Container images
CONTROLLER_MANAGER_IMAGE ?= aiagent/controller-manager:latest
ADK_HANDLER_IMAGE ?= aiagent/adk-handler:latest
OPENCLAW_HANDLER_IMAGE ?= aiagent/openclaw-handler:latest
ADK_FRAMEWORK_IMAGE ?= aiagent/adk-framework:latest
INIT_CONTAINER_IMAGE ?= aiagent/init-container:latest

# Go settings
GO_VERSION ?= 1.23
GOLANGCI_LINT_VERSION ?= 1.54.2

# Kubernetes settings
KUBEBUILDER_ASSETS ?= $(shell pwd)/bin
CRD_DIR ?= config/crd/bases

# Kind settings
KIND_CLUSTER_NAME ?= aiagent-cluster
KIND_NODE_IMAGE ?= kindest/node:v1.29.2

# Output directories
BIN_DIR := bin
OUT_DIR := _out

.PHONY: all build test clean manifests generate docker-build docker-push kind-create kind-delete kind-load kind-deploy

all: build test

# =======================
# Build
# =======================

build: generate
	go build -o $(BIN_DIR)/manager ./cmd/manager
	go build -o $(BIN_DIR)/adk-handler ./cmd/adk-handler
	go build -o $(BIN_DIR)/openclaw-handler ./cmd/openclaw-handler
	go build -o $(BIN_DIR)/adk-framework ./cmd/adk-framework

build-binaries:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/manager ./cmd/manager
	go build -o $(BIN_DIR)/adk-handler ./cmd/adk-handler
	go build -o $(BIN_DIR)/openclaw-handler ./cmd/openclaw-handler
	go build -o $(BIN_DIR)/adk-framework ./cmd/adk-framework

# =======================
# Generate
# =======================

generate:
	go generate ./...

manifests:
	@echo "CRD manifests are already generated in $(CRD_DIR)"
	@echo "To regenerate using controller-gen, run: controller-gen crd paths=./api/v1 output:crd:dir=$(CRD_DIR)"

# =======================
# Testing
# =======================

test:
	go test ./... -coverprofile=$(OUT_DIR)/coverage.out

test-unit:
	go test ./pkg/... ./api/... -coverprofile=$(OUT_DIR)/coverage-unit.out

test-integration:
	@echo "Running integration tests (requires envtest)"
	go test ./test/integration/... -coverprofile=$(OUT_DIR)/coverage-integration.out

test-e2e:
	@echo "Running e2e tests (requires running cluster)"
	go test ./test/e2e/... -coverprofile=$(OUT_DIR)/coverage-e2e.out

# =======================
# Docker
# =======================

docker-build-manager:
	docker build -t $(CONTROLLER_MANAGER_IMAGE) -f Dockerfile.manager .

docker-build-adk-handler:
	docker build -t $(ADK_HANDLER_IMAGE) -f Dockerfile.adk-handler .

docker-build-openclaw-handler:
	docker build -t $(OPENCLAW_HANDLER_IMAGE) -f Dockerfile.openclaw-handler .

docker-build-adk-framework:
	docker build -t $(ADK_FRAMEWORK_IMAGE) -f Dockerfile.adk-framework .

docker-build-init:
	docker build -t $(INIT_CONTAINER_IMAGE) -f Dockerfile.init-container .

docker-build: docker-build-manager docker-build-adk-handler docker-build-openclaw-handler docker-build-adk-framework docker-build-init
	@echo "All images built successfully"

docker-push:
	docker push $(CONTROLLER_MANAGER_IMAGE)
	docker push $(ADK_HANDLER_IMAGE)
	docker push $(OPENCLAW_HANDLER_IMAGE)
	docker push $(ADK_FRAMEWORK_IMAGE)
	docker push $(INIT_CONTAINER_IMAGE)

docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(CONTROLLER_MANAGER_IMAGE) -f Dockerfile.manager .
	docker buildx build --platform linux/amd64,linux/arm64 -t $(ADK_HANDLER_IMAGE) -f Dockerfile.adk-handler .
	docker buildx build --platform linux/amd64,linux/arm64 -t $(OPENCLAW_HANDLER_IMAGE) -f Dockerfile.openclaw-handler .
	docker buildx build --platform linux/amd64,linux/arm64 -t $(ADK_FRAMEWORK_IMAGE) -f Dockerfile.adk-framework .
	docker buildx build --platform linux/amd64,linux/arm64 -t $(INIT_CONTAINER_IMAGE) -f Dockerfile.init-container .

# =======================
# Kind Cluster
# =======================

kind-create:
	@echo "Creating kind cluster: $(KIND_CLUSTER_NAME)"
	kind create cluster --name $(KIND_CLUSTER_NAME) --image $(KIND_NODE_IMAGE) --config config/kind/kind-config.yaml

kind-delete:
	@echo "Deleting kind cluster: $(KIND_CLUSTER_NAME)"
	kind delete cluster --name $(KIND_CLUSTER_NAME)

kind-load: docker-build
	@echo "Loading images into kind cluster"
	kind load docker-image $(CONTROLLER_MANAGER_IMAGE) --name $(KIND_CLUSTER_NAME)
	kind load docker-image $(ADK_HANDLER_IMAGE) --name $(KIND_CLUSTER_NAME)
	kind load docker-image $(OPENCLAW_HANDLER_IMAGE) --name $(KIND_CLUSTER_NAME)
	kind load docker-image $(ADK_FRAMEWORK_IMAGE) --name $(KIND_CLUSTER_NAME)
	kind load docker-image $(INIT_CONTAINER_IMAGE) --name $(KIND_CLUSTER_NAME)

kind-deploy: kind-load
	@echo "Deploying AI Agent components to kind cluster"
	kubectl apply -f config/crd/bases/
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/
	@echo "Waiting for controller manager to be ready..."
	kubectl wait --for=condition=available --timeout=120s deployment/aiagent-controller-manager -n aiagent-system || true

kind-deploy-samples:
	@echo "Deploying sample Harnesses and AgentRuntimes"
	kubectl apply -f config/samples/harness.yaml
	kubectl apply -f config/samples/runtime.yaml
	@echo "Waiting for AgentRuntimes..."
	sleep 5
	kubectl get agentruntimes -n default
	kubectl apply -f config/samples/agent.yaml

# Full deployment flow
kind-full-deploy: kind-create kind-deploy kind-deploy-samples
	@echo "Full deployment complete"
	kubectl get all -n default
	kubectl get all -n aiagent-system

# =======================
# Install dependencies
# =======================

install-controller-gen:
	@echo "Installing controller-gen"
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

install-envtest:
	@echo "Installing envtest"
	@mkdir -p $(KUBEBUILDER_ASSETS)
	$(BIN_DIR)/setup-envtest use 1.29.2 --bin-dir $(KUBEBUILDER_ASSETS)

install-golangci-lint:
	@echo "Installing golangci-lint"
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(BIN_DIR) v$(GOLANGCI_LINT_VERSION)

install-kubebuilder:
	@echo "Installing kubebuilder assets"
	@mkdir -p $(KUBEBUILDER_ASSETS)
	curl -sSfL https://github.com/kubernetes-sigs/kubebuilder/releases/download/v3.14.1/kubebuilder_$(OS)_$(ARCH).tar.gz | tar -xz -C $(KUBEBUILDER_ASSETS)

# =======================
# Clean
# =======================

clean:
	rm -rf $(BIN_DIR)
	rm -rf $(OUT_DIR)
	go clean

# =======================
# Lint
# =======================

lint:
	golangci-lint run --timeout=5m

# =======================
# Help
# =======================

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build                Build all binaries"
	@echo "  build-binaries       Build binaries locally"
	@echo "  test                 Run all tests"
	@echo "  test-unit            Run unit tests"
	@echo "  docker-build         Build all Docker images"
	@echo "  docker-build-manager Build controller manager image"
	@echo "  docker-build-adk-*   Build ADK handler/framework images"
	@echo "  docker-build-openclaw-* Build OpenClaw handler image"
	@echo "  kind-create          Create kind cluster"
	@echo "  kind-delete          Delete kind cluster"
	@echo "  kind-load            Load Docker images into kind"
	@echo "  kind-deploy          Deploy controller to kind"
	@echo "  kind-deploy-samples  Deploy sample CRs"
	@echo "  kind-full-deploy     Full deployment (create + deploy)"
	@echo "  clean                Clean build artifacts"
	@echo "  lint                 Run linter"
	@echo "  help                 Show this help"