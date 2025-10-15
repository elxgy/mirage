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

.PHONY: help
help:
	@echo "Mirage v$(VERSION)"
	@echo ""
	@echo "Available targets:"
	@echo "  build    - Build the binary"
	@echo "  install  - Install to GOPATH/bin"
	@echo "  test     - Run tests"
	@echo "  fmt      - Format code"
	@echo "  vet      - Run go vet"
	@echo "  clean    - Remove build artifacts"
	@echo "  run      - Build and run example"
	@echo "  help     - Show this help"
