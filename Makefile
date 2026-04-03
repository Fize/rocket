# Image URL to use all building/pushing image targets
IMG ?= rocket:latest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22.1

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: docker-build docker-push

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests.
	go test ./...

.PHONY: runm
runm: ## run the manager
	go run ./cmd/manager/manager.go

.PHONY: runa
runa: ## run the agent
	go run ./cmd/agent/agent.go

.PHONY: e2e-kind-create
e2e-kind-create: ## Create a local kind cluster for e2e tests
	bash hack/e2e-kind.sh create

.PHONY: e2e-kind-test
e2e-kind-test: ## Run e2e tests against the local kind cluster
	bash hack/e2e-kind.sh test

.PHONY: e2e-kind-delete
e2e-kind-delete: ## Delete the local kind cluster
	bash hack/e2e-kind.sh delete

.PHONY: e2e-kind
e2e-kind: ## Run full e2e suite (create, test, delete)
	bash hack/e2e-kind.sh create
	bash hack/e2e-kind.sh test
	bash hack/e2e-kind.sh delete

.PHONY: e2e-kruise-create
e2e-kruise-create: ## Create multi-cluster kind environment for kruise-rollout e2e tests
	bash hack/kruise-rollout-e2e.sh create

.PHONY: e2e-kruise-test
e2e-kruise-test: ## Run kruise-rollout e2e tests
	bash hack/kruise-rollout-e2e.sh test

.PHONY: e2e-kruise-delete
e2e-kruise-delete: ## Delete kruise-rollout e2e clusters
	bash hack/kruise-rollout-e2e.sh delete

.PHONY: e2e-kruise
e2e-kruise: ## Run full kruise-rollout e2e suite (create, test, delete)
	bash hack/kruise-rollout-e2e.sh create
	bash hack/kruise-rollout-e2e.sh test
	bash hack/kruise-rollout-e2e.sh delete

##@ Build

.PHONY: build-all
build-all: fmt vet ## Build all binaries.
	go build -o bin/manager cmd/manager/manager.go
	go build -o bin/agent cmd/agent/agent.go

##@ Code Generation

.PHONY: generate
generate: ## Generate CRDs and deepcopy code using controller-gen
	./bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./pkg/apis/..."
	./bin/controller-gen crd paths="./pkg/apis/apps/...;./pkg/apis/storage/...;./pkg/apis/cluster/..." output:crd:artifacts:config=config/crd/bases

##@ Install controller-gen

.PHONY: controller-gen
controller-gen: ## Install controller-gen into ./bin/
	mkdir -p bin && GOBIN=$(PWD)/bin go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/manager cmd/manager/manager.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/agent cmd/agent/agent.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test ## Build docker images for manager and agent.
	docker build --target manager -t rocket-manager:latest .
	docker build --target agent -t rocket-agent:latest .

.PHONY: docker-push
push-push: ## Push docker image with the manager.
	docker push ${IMG}

