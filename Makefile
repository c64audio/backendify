# Variables
GO=go
BINARY_NAME=backendify
BUILD_DIR=.
GO_BUILD_FLAGS=-o $(BUILD_DIR)/$(BINARY_NAME)
DOCKER_IMAGE_NAME=backendify
DOCKER_TAG=latest

# Default target
.PHONY: all
all: test build

# Build the application
.PHONY: build
build:
	$(GO) build $(GO_BUILD_FLAGS) ./cmd

# Run the application locally with config
.PHONY: run
run: build
	./$(BINARY_NAME)

# Run tests
.PHONY: test
test:
	$(GO) test ./...

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(BINARY_NAME)
	$(GO) clean

# Build Docker image with embedded config
.PHONY: docker-build
docker-build:
	docker build -t $(DOCKER_IMAGE_NAME):$(DOCKER_TAG) .

# Run Docker container
.PHONY: docker-run
docker-run: docker-build
	docker run -p 9000:9000 $(DOCKER_IMAGE_NAME):$(DOCKER_TAG)

# Run the Docker container with shell access for debugging
.PHONY: docker-debug
docker-debug: docker-build
	docker run -it --entrypoint sh -p 9000:9000 $(DOCKER_IMAGE_NAME):$(DOCKER_TAG)

# Build and run using the same steps as CI
.PHONY: ci-local
ci-local:
	$(GO) mod download
	$(GO) test ./...
	$(GO) build $(GO_BUILD_FLAGS) ./cmd
	docker build -t $(DOCKER_IMAGE_NAME):$(DOCKER_TAG) .

# Format Go code
.PHONY: fmt
fmt:
	$(GO) fmt ./...

# Check for lint issues (if golangci-lint is installed)
.PHONY: lint
lint:
	which golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"

# Download dependencies
.PHONY: deps
deps:
	$(GO) mod download
	$(GO) mod tidy

# Check for outdated dependencies
.PHONY: deps-check
deps-check:
	$(GO) list -u -m all

# Help target
.PHONY: help
help:
	@echo "Backendify Makefile targets:"
	@echo "  all          - Run tests and build the application (default)"
	@echo "  build        - Build the application"
	@echo "  run          - Build and run the application locally"
	@echo "  test         - Run tests"
	@echo "  clean        - Remove build artifacts"
	@echo "  docker-build - Build Docker image with embedded config"
	@echo "  docker-run   - Build and run Docker container"
	@echo "  docker-debug - Build and run container with shell for debugging"
	@echo "  ci-local     - Run the same build steps as the CI pipeline locally"
	@echo "  fmt          - Format Go code"
	@echo "  lint         - Check for lint issues"
	@echo "  deps         - Download and tidy dependencies"
	@echo "  deps-check   - Check for outdated dependencies"
	@echo "  help         - Show this help message"