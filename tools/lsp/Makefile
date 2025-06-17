SERVER_BINARY_NAME = kro-lsp
SERVER_PACKAGE_PATH = ./tools/lsp/server
CLIENT_DIR = client
SERVER_DIR = server
ROOT_DIR = ../..

## build: build both server and client
.PHONY: build
build: build/server build/client

## build/server: build the language server
.PHONY: build/server
build/server:
	@echo "Building language server with debug symbols..."
	cd $(ROOT_DIR) && go build -o tools/lsp/server/$(SERVER_BINARY_NAME) $(SERVER_PACKAGE_PATH)

## build/client: build the VS Code extension
.PHONY: build/client
build/client:
	@echo "Building VS Code extension..."
	cd $(CLIENT_DIR) && npm install && npm run compile

## run/server: run the language server
.PHONY: run/server
run/server: build/server
	@echo "Starting language server..."
	cd $(SERVER_DIR) && ./$(SERVER_BINARY_NAME)


## clean: clean both server and client build artifacts and dependencies
.PHONY: clean
clean: clean/server clean/client

## clean/server: clean server binary and dependencies
.PHONY: clean/server
clean/server:
	@echo "Cleaning server build artifacts and dependencies..."
	rm -f $(SERVER_DIR)/$(SERVER_BINARY_NAME)
	cd $(ROOT_DIR) && go clean -modcache

## clean/client: clean client build artifacts and dependencies
.PHONY: clean/client
clean/client:
	@echo "Cleaning client build artifacts and dependencies..."
	cd $(CLIENT_DIR) && rm -rf out node_modules package-lock.json
