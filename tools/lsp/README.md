# Kro Language Server Protocol (LSP) Implementation

This directory contains the implementation of a Language Server Protocol (LSP) for Kro YAML configuration files, specifically focusing on ResourceGraphDefinition (RGD) files.

> **Note**: This LSP implementation uses the main Kro project's `go.mod` file and is built as part of the unified module `github.com/kro-run/kro`. There are no separate `go.mod` files in the LSP directories.

## Architecture

The LSP implementation follows a layered architecture:

### Server Side (`server/`)

- **Protocol Layer**: Handles JSON-RPC communication using the `tliron/glsp` library
- **Router**: Central dispatcher for LSP method calls
- **Document Manager**: Tracks open documents and their parsed models
- **Validation Manager**: Coordinates validation operations
- **YAML Parser**: AST-based parser using `goccy/go-yaml` for precise position tracking
- **Validators**:
  - Document Validator: Basic YAML and Kubernetes resource validation
  - Kro Adapter: Integrates Kro-specific validation logic
  - CRD Validator: Validates against Custom Resource Definitions (placeholder)

### Client Side (`client/`)

- **VS Code Extension**: TypeScript extension that connects to the language server
- **Language Configuration**: YAML language support and syntax highlighting

## Features Implemented

### ✅ Core Infrastructure

- [x] LSP server setup with `tliron/glsp`
- [x] Document synchronization (open, change, close, save)
- [x] AST-based YAML parsing with position tracking
- [x] Document and validation managers
- [x] VS Code client extension

### ✅ Validation

- [x] Basic YAML syntax validation
- [x] Kubernetes resource structure validation
- [x] RGD-specific validation (schema fields, resources structure)
- [x] CEL expression syntax checking (basic)
- [x] Diagnostic publishing to client

### 🚧 In Progress

- [ ] Advanced CEL expression validation using `google/cel-go`
- [ ] CRD-based validation
- [ ] Integration with existing Kro validation logic

### 📋 Planned Features

- [ ] Auto-completion for RGD fields
- [ ] Hover information
- [ ] Go-to-definition for resource references
- [ ] Code actions and quick fixes
- [ ] Document formatting

## Building and Running

### Prerequisites

- Go 1.24+
- Node.js 16+ (for VS Code extension)
- VS Code (for testing)

### Using the Makefile (Recommended)

The project includes a comprehensive Makefile inspired by [Alex Edwards' time-saving Makefile patterns](https://www.alexedwards.net/blog/a-time-saving-makefile-for-your-go-projects).

#### Quick Start

```bash
# Install dependencies and build everything
make start

# Or step by step:
make install    # Install all dependencies
make build      # Build both server and client
```

#### Common Development Tasks

```bash
# Get help with available targets
make help

# Development with hot reload
make dev

# Run quality checks
make audit

# Run tests
make test

# Clean up build artifacts
make clean

# Complete cleanup (including dependencies)
make clean/all
```

#### Building Individual Components

```bash
# Build only the server
make build/server

# Build only the client
make build/client

# Install only server dependencies
make install/server

# Install only client dependencies
make install/client
```

### Manual Building (Alternative)

#### Build the Server

```bash
# From the root of the Kro project
go build -o tools/lsp/server/kro-lsp ./tools/lsp/server

# Or from the tools/lsp directory
cd ../../  # Go to root
go build -o tools/lsp/server/kro-lsp ./tools/lsp/server
```

#### Build the Client Extension

```bash
cd client
npm install
npm run compile
```

### Testing the LSP

1. **Start the server manually** (for debugging):

   ```bash
   # Using Makefile
   make debug/server

   # Or manually
   cd server
   ./kro-lsp
   ```

2. **Test with VS Code**:

   ```bash
   # Package and install the extension locally
   make deploy/local
   ```

   Then:

   - Open VS Code in the `tools/lsp` directory
   - The extension should be installed and active
   - Open the `test-rgd.yaml` file
   - You should see validation diagnostics for any errors

### Configuration

The VS Code extension can be configured via settings:

```json
{
  "kroLanguageServer.serverPath": "/path/to/kro-lsp",
  "kroLanguageServer.trace.server": "verbose"
}
```

## File Structure

```
tools/lsp/
├── server/                     # Go LSP server
│   ├── main.go                # Server entry point
│   ├── server.go              # Core server implementation
│   ├── router.go              # Request routing
│   ├── document/              # Document management
│   │   ├── manager.go         # Document lifecycle
│   │   └── types.go           # Document type definitions
│   ├── validation/            # Validation framework
│   │   ├── manager.go         # Validation coordination
│   │   ├── document_validator.go  # Basic validation
│   │   ├── kro_adapter.go     # Kro-specific validation
│   │   └── crd_validator.go   # CRD validation (placeholder)
│   ├── parser/                # YAML parsing
│   │   ├── yaml.go            # AST-based YAML parser
│   │   └── position.go        # Position tracking utilities
│   ├── protocol/              # LSP protocol types
│   │   ├── types.go           # Common types
│   │   └── document_sync.go   # Document synchronization
│   └── handlers/              # Request handlers (legacy)
├── client/                    # VS Code extension
│   ├── src/
│   │   └── extension.ts       # Main extension code
│   ├── package.json           # Extension manifest
│   ├── tsconfig.json          # TypeScript configuration
│   └── language-configuration.json  # YAML language config
├── test-rgd.yaml             # Test RGD file
└── README.md                 # This file
```

## Validation Examples

The LSP provides real-time validation for:

### Basic YAML Issues

```yaml
# Missing required fields
apiVersion: kro.run/v1alpha1
# kind: ResourceGraphDefinition  # ❌ Missing required field
```

### RGD Structure Issues

```yaml
apiVersion: kro.run/v1alpha1
kind: ResourceGraphDefinition
spec:
  resources:
    - template: # ❌ Missing required 'id' field
        apiVersion: v1
        kind: Service
```

### Schema Field Issues

```yaml
spec:
  schema:
    spec:
      name: invalid_type # ❌ Invalid field type
```

## Development

### Adding New Validators

1. Create a new validator in `server/validation/`
2. Implement the validation interface
3. Register it in the validation manager
4. Add tests

### Extending Language Features

1. Add new handlers in `server/router.go`
2. Implement the feature logic
3. Update the server capabilities
4. Test with the VS Code client

## Contributing

This LSP implementation is part of the Kro project. When contributing:

1. Follow Go best practices for the server
2. Use TypeScript best practices for the client
3. Add tests for new features
4. Update documentation

## Troubleshooting

### Server Not Starting

- Check the server path configuration
- Verify the binary is executable
- Check the VS Code output panel for errors

### No Diagnostics Appearing

- Ensure the file is recognized as YAML
- Check the language server trace output
- Verify the document is being parsed correctly

### Performance Issues

- The AST parser may be slow on very large files
- Consider implementing incremental parsing for better performance

## Future Enhancements

1. **Advanced CEL Support**: Full CEL expression validation and completion
2. **CRD Integration**: Load and validate against actual CRDs
3. **Kro Integration**: Deep integration with Kro's validation logic
4. **Multi-file Support**: Cross-file reference validation
5. **Performance Optimization**: Incremental parsing and caching
6. **Testing Framework**: Comprehensive test suite for LSP features
