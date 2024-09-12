# Variables
BINARY_NAME=tod-p2m
GO=go
GOBUILD=$(GO) build
GOCLEAN=$(GO) clean
GOTEST=$(GO) test
GOGET=$(GO) get
GOMOD=$(GO) mod
GOFMT=$(GO) fmt
GOLINT=golangci-lint

# Module name (change this to your module name)
MODULE_NAME=tod-p2m

# Directories
CMD_DIR=./cmd/server
INTERNAL_DIR=./internal
PKG_DIR=./pkg
TEMP_DIR=./tmp

# Build flags
LDFLAGS=-ldflags "-w -s"

# Determine OS for sed command
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
    SED_INPLACE := sed -i
else ifeq ($(UNAME_S),Darwin)
    SED_INPLACE := sed -i ''
else
    SED_INPLACE := sed -i
endif

# Fly.io variables
FLY_APP_NAME := $(shell fly status --json | jq -r '.Name')
FLY_VOLUME_NAME := $(shell fly volumes list --json | jq -r '.[0].Name')

.PHONY: all build clean deep-clean test coverage deps lint fmt run help init fix-imports remove-backups increase-volume increase-volume-20gb

all: init fix-imports clean deps fmt lint test build

build:
	@echo "Building..."
	@$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) $(CMD_DIR)

clean:
	@echo "Cleaning..."
	@$(GOCLEAN)
	@rm -f $(BINARY_NAME)
	@rm -f coverage.out
	@rm -rf $(TEMP_DIR)

deep-clean: clean remove-backups
	@echo "Performing deep clean..."
	@find . -name "*~" -type f -delete
	@find . -name "*.swp" -type f -delete
	@find . -name ".DS_Store" -type f -delete

test:
	@echo "Running tests..."
	@$(GOTEST) -v ./...

coverage:
	@echo "Running tests with coverage..."
	@$(GOTEST) -v -coverprofile=coverage.out ./...
	@$(GO) tool cover -html=coverage.out

deps: init
	@echo "Checking and downloading dependencies..."
	@$(GOMOD) download
	@$(GOMOD) verify

lint:
	@echo "Linting..."
	@if command -v $(GOLINT) > /dev/null; then \
		$(GOLINT) run; \
	else \
		echo "Warning: golangci-lint is not installed. Skipping linting."; \
		echo "To install, run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

fmt:
	@echo "Formatting..."
	@$(GOFMT) ./...

run: build
	@echo "Running..."
	@./$(BINARY_NAME)

init:
	@echo "Initializing and verifying Go module..."
	@mkdir -p $(TEMP_DIR)
	@if [ ! -f go.mod ]; then \
		$(GOMOD) init $(MODULE_NAME); \
		echo "Go module initialized"; \
	else \
		if ! grep -q "^module $(MODULE_NAME)" go.mod; then \
			$(SED_INPLACE) '1s#^module.*#module $(MODULE_NAME)#' go.mod; \
			echo "Module name updated in go.mod"; \
		else \
			echo "Go module already initialized and declared correctly"; \
		fi \
	fi
	@$(GOMOD) tidy

fix-imports:
	@echo "Fixing imports..."
	@mkdir -p $(TEMP_DIR)
	@find . -name '*.go' -type f -exec $(SED_INPLACE) 's#tod-p2m/#$(MODULE_NAME)/#g' {} +

remove-backups:
	@echo "Removing backup files..."
	@rm -rf $(TEMP_DIR)

increase-volume:
	@echo "Increasing volume size..."
	@if [ -z "$$FLY_VOLUME_SIZE" ]; then \
		read -p "Enter the desired volume size in GB: " FLY_VOLUME_SIZE; \
		if [ -z "$$FLY_VOLUME_SIZE" ]; then \
			echo "Error: Volume size not provided."; \
			exit 1; \
		fi; \
	fi
	@echo "Current volume size:"
	@fly volumes list
	@echo "Increasing volume size to $$FLY_VOLUME_SIZE GB..."
	@fly volumes extend $(FLY_VOLUME_NAME) --size $$FLY_VOLUME_SIZE
	@echo "New volume size:"
	@fly volumes list

increase-volume-20gb:
	@echo "Increasing volume size to 20 GB..."
	@echo "Debugging information:"
	@echo "App name: $(FLY_APP_NAME)"
	@echo "Volume name: $(FLY_VOLUME_NAME)"
	@echo "Current volumes:"
	@fly volumes list
	@if [ -z "$(FLY_VOLUME_NAME)" ]; then \
		echo "Error: Unable to determine volume name."; \
		echo "Please specify the volume name manually:"; \
		read -p "Enter the volume name: " MANUAL_VOLUME_NAME; \
		if [ -z "$$MANUAL_VOLUME_NAME" ]; then \
			echo "No volume name provided. Exiting."; \
			exit 1; \
		fi; \
		echo "Attempting to increase size of volume: $$MANUAL_VOLUME_NAME"; \
		fly volumes extend $$MANUAL_VOLUME_NAME --size 20 || { echo "Failed to extend volume. Please check the volume name and your Fly.io permissions."; exit 1; }; \
	else \
		echo "Attempting to increase size of volume: $(FLY_VOLUME_NAME)"; \
		fly volumes extend $(FLY_VOLUME_NAME) --size 20 || { echo "Failed to extend volume. Please check the volume name and your Fly.io permissions."; exit 1; }; \
	fi
	@echo "New volume size:"
	@fly volumes list

help:
	@echo "Available commands:"
	@echo "  make all          - Initialize module, fix imports, clean, get dependencies, format, lint, test, and build"
	@echo "  make build        - Build the application"
	@echo "  make clean        - Clean build files and temporary directory"
	@echo "  make deep-clean   - Perform a deep clean, including removing backup and temporary files"
	@echo "  make test         - Run tests"
	@echo "  make coverage     - Run tests with coverage"
	@echo "  make deps         - Check and download dependencies"
	@echo "  make lint         - Run linter (if installed)"
	@echo "  make fmt          - Format code"
	@echo "  make run          - Build and run the application"
	@echo "  make init         - Initialize Go module (if not already initialized)"
	@echo "  make fix-imports  - Fix import paths in all Go files"
	@echo "  make remove-backups - Remove backup files"
	@echo "  make increase-volume - Increase Fly.io volume size based on user input or FLY_VOLUME_SIZE environment variable"
	@echo "  make increase-volume-20gb - Increase Fly.io volume size to 20 GB"
	@echo "  make help         - Show this help message"

# Default target
.DEFAULT_GOAL := help