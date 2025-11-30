.PHONY: build install clean test lint run

BINARY := muxctl
BUILD_DIR := bin
CMD_PATH := ./cmd/muxctl

# Build the binary
build:
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)

# Install to $GOPATH/bin
install:
	go install $(CMD_PATH)

# Run without building
run:
	go run $(CMD_PATH) $(ARGS)

# Run tests
test:
	go test -v ./...

# Run tests with coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Tidy dependencies
tidy:
	go mod tidy

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Lint (requires golangci-lint)
lint:
	golangci-lint run

# Format code
fmt:
	go fmt ./...

# Build for multiple platforms
release:
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(CMD_PATH)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(CMD_PATH)
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(CMD_PATH)
