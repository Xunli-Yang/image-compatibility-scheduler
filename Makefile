# Makefile for custom-scheduler

# Variables
REGISTRY ?= docker.io
IMAGE_NAME ?= custom-scheduler
IMAGE_TAG ?= latest
IMAGE ?= $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
BINARY_NAME ?= custom-scheduler
GO_VERSION ?= 1.25
BUILD_DIR ?= bin

# Build flags
LDFLAGS ?= -s -w
GO_BUILD_FLAGS ?= -ldflags="$(LDFLAGS)"

.PHONY: all build clean test docker-build docker-push deploy help

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

# Build Docker image
docker-build: build
	@echo "Building Docker image $(IMAGE)..."
	@DOCKER_BUILDKIT=1 docker build -t $(IMAGE) -f Dockerfile .

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image $(IMAGE)..."
	@docker push $(IMAGE)

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
	@echo "  build         - Build the scheduler binary"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  deploy        - Deploy scheduler to Kubernetes"
	@echo "  undeploy      - Remove scheduler from Kubernetes"
	@echo "  fmt           - Format code"
	@echo "  lint          - Run linter"
	@echo "  vet           - Run go vet"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY      - Docker registry (default: docker.io)"
	@echo "  IMAGE_NAME    - Image name (default: custom-scheduler)"
	@echo "  IMAGE_TAG     - Image tag (default: latest)"
	@echo "  BINARY_NAME   - Binary name (default: custom-scheduler)"
