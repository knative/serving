# Default registry base URL
REGISTRY_BASE ?= registry.dev-aws.us-east-1.cerebrium.ai

# Ko configuration
KO_DOCKER_REPO ?= $(REGISTRY_BASE)

# Components to build
COMPONENTS = activator autoscaler queue webhook

# Default target
.PHONY: all
all: build

# Build all components
.PHONY: build
build: $(COMPONENTS)

# Build individual components
.PHONY: activator
activator:
	@echo "Building activator..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --platform=linux/amd64 --tags=dev ./cmd/activator

.PHONY: autoscaler
autoscaler:
	@echo "Building autoscaler..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --platform=linux/amd64 --tags=dev ./cmd/autoscaler

.PHONY: queue
queue:
	@echo "Building queue-proxy..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --platform=linux/amd64 --tags=dev ./cmd/queue

.PHONY: webhook
webhook:
	@echo "Building webhook..."
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build --platform=linux/amd64 --tags=dev ./cmd/webhook


# Check if ko is installed
.PHONY: check-ko
check-ko:
	@command -v ko >/dev/null 2>&1 || { echo "Error: ko is not installed. Please install from https://github.com/ko-build/ko"; exit 1; }

# Login to registry
.PHONY: login
login:
	@echo "Logging in to $(REGISTRY_BASE)..."
	@docker login $(REGISTRY_BASE)

# Clean local images
.PHONY: clean
clean:
	@echo "Cleaning local ko cache..."
	@rm -rf .ko

# Help
.PHONY: help
help:
	@echo "Knative Serving Build Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all/build      - Build all components with ko"
	@echo "  activator      - Build activator component"
	@echo "  autoscaler     - Build autoscaler component"
	@echo "  queue          - Build queue-proxy component"
	@echo "  webhook        - Build webhook component"
	@echo ""
	@echo "  login          - Login to container registry"
	@echo "  clean          - Clean local ko cache"
	@echo "  help           - Show this help message"
	@echo ""
	@echo "Environment Variables:"
	@echo "  REGISTRY_BASE  - Container registry base URL (default: registry.dev-aws.us-east-1.cerebrium.ai)"
	@echo "  KO_DOCKER_REPO - Ko docker repository (defaults to REGISTRY_BASE)"
	@echo ""
	@echo "Examples:"
	@echo "  make build                                    # Build all components"
	@echo "  make activator                                # Build only activator"
	@echo "  REGISTRY_BASE=my.registry.com make build      # Build with custom registry"
