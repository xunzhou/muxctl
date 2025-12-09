.PHONY: build clean run install test deps help

BINARY_NAME=muxctl
GO=go

build:
	@echo "Building $(BINARY_NAME)..."
	$(GO) build -o $(BINARY_NAME) .

clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	$(GO) clean

install: build
	@echo "Installing $(BINARY_NAME) to ~/bin..."
	mkdir -p ~/bin
	cp $(BINARY_NAME) ~/bin/
	@echo "Done! Make sure ~/bin is in your PATH."

run: build
	@echo "Starting $(BINARY_NAME)..."
	./$(BINARY_NAME)

test:
	@echo "Running tests..."
	$(GO) test -v ./...

deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

help:
	@echo "Available targets:"
	@echo "  build    - Build the binary"
	@echo "  clean    - Remove binary and clean"
	@echo "  install  - Install to ~/bin"
	@echo "  run      - Build and run muxctl"
	@echo "  deps     - Download dependencies"
	@echo "  test     - Run tests"
	@echo "  help     - Show this help"
