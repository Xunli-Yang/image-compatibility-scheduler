# Makefile for custom-scheduler

# Variables
REGISTRY ?= docker.io
IMAGE_NAME ?= custom-scheduler
IMAGE_TAG ?= latest
ARCH ?= amd64
IMAGE ?= $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
BINARY_NAME ?= custom-scheduler
GO_VERSION ?= 1.25
BUILD_DIR ?= bin

# Build flags
LDFLAGS ?= -s -w
GO_BUILD_FLAGS ?= -ldflags="$(LDFLAGS)"

.PHONY: all build clean test docker-build docker-push docker-build-multi docker-push-multi deploy help

all: build

# Build the scheduler binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

# Build Docker image for specified architecture
docker-build:
	@echo "Building Docker image $(IMAGE) for $(ARCH)..."
	@DOCKER_BUILDKIT=0 docker build \
		--build-arg TARGETARCH=$(ARCH) \
		-t $(IMAGE) \
		-f Dockerfile .

# Push Docker image for specified architecture
docker-push: docker-build
	@echo "Pushing Docker image $(IMAGE)..."
	@docker push $(IMAGE)

# Build both amd64 and arm64 images
docker-build-multi:
	@echo "Building both amd64 and arm64 images..."
	@$(MAKE) docker-build ARCH=amd64
	@$(MAKE) docker-build ARCH=arm64

# Push both amd64 and arm64 images
docker-push-multi:
	@echo "Pushing both amd64 and arm64 images..."
	@$(MAKE) docker-push ARCH=amd64
	@$(MAKE) docker-push ARCH=arm64

# Deploy to Kubernetes
deploy: docker-push
	@echo "Deploying scheduler..."
	@kubectl apply -f deploy/

# Undeploy from Kubernetes
undeploy:
	@echo "Undeploying scheduler..."
	@kubectl delete -f deploy/ --ignore-not-found=true

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Run vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Help target
help:
	@echo "Available targets:"
	@echo "  build                - Build the scheduler binary"
	@echo "  test                 - Run tests"
	@echo "  clean                - Clean build artifacts"
	@echo "  docker-build         - Build Docker image for specified architecture (default: amd64)"
	@echo "  docker-push          - Push Docker image for specified architecture"
	@echo "  docker-build-multi   - Build both amd64 and arm64 images"
	@echo "  docker-push-multi    - Push both amd64 and arm64 images"
	@echo "  deploy               - Deploy scheduler to Kubernetes"
	@echo "  undeploy             - Remove scheduler from Kubernetes"
	@echo "  fmt                  - Format code"
	@echo "  lint                 - Run linter"
	@echo "  vet                  - Run go vet"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY             - Docker registry (default: docker.io)"
	@echo "  IMAGE_NAME           - Image name (default: custom-scheduler)"
	@echo "  IMAGE_TAG            - Image tag (default: latest)"
	@echo "  ARCH                 - Target architecture: amd64 or arm64 (default: amd64)"
	@echo "  BINARY_NAME          - Binary name (default: custom-scheduler)"
	@echo ""
	@echo "Examples:"
	@echo "  make docker-build ARCH=amd64          # Build amd64 image"
	@echo "  make docker-build ARCH=arm64          # Build arm64 image"
	@echo "  make docker-build-multi               # Build both architectures"
	@echo "  make docker-push ARCH=arm64           # Push arm64 image"
