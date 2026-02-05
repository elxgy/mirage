BINARY_NAME=mirage
VERSION=1.0.0
BUILD_DIR=build
LDFLAGS=-ldflags "-s -w -X main.appVersion=$(VERSION)"

.DEFAULT_GOAL := build

.PHONY: build
build:
	@mkdir -p $(BUILD_DIR)
	@echo "Building $(BINARY_NAME)..."
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build completed: $(BUILD_DIR)/$(BINARY_NAME)"

.PHONY: install
install: build
	@echo "Installing $(BINARY_NAME)..."
	@go install $(LDFLAGS) .

.PHONY: test
test:
	@go test -v ./...

.PHONY: fmt
fmt:
	@gofmt -s -w .

.PHONY: vet
vet:
	@go vet ./...

.PHONY: clean
clean:
	@rm -rf $(BUILD_DIR)
	@go clean

.PHONY: run
run: build
	@./$(BUILD_DIR)/$(BINARY_NAME) -v ls -la

.PHONY: run-standard
run-standard: build
	@./$(BUILD_DIR)/$(BINARY_NAME) --standard -v ls -la

.PHONY: run-deep
run-deep: build
	@./$(BUILD_DIR)/$(BINARY_NAME) --deep -v ls -la

.PHONY: preload
preload:
	@$(MAKE) -C internal/instrument/preload
	@mkdir -p $(BUILD_DIR)
	@cp internal/instrument/preload/preload_shim.so $(BUILD_DIR)/
	@echo "Preload shim built and copied to $(BUILD_DIR)/preload_shim.so"

.PHONY: help
help:
	@echo "Mirage v$(VERSION)"
	@echo ""
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  install      - Install to GOPATH/bin"
	@echo "  test         - Run tests"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  clean        - Remove build artifacts"
	@echo "  run          - Build and run example"
	@echo "  run-standard - Build and run with --standard mode"
	@echo "  run-deep     - Build and run with --deep mode"
	@echo "  preload      - Build LD_PRELOAD shim and copy to build dir"
	@echo "  help         - Show this help"
