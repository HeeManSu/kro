package main

import (
	"context"

	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/kro-run/kro/tools/lsp/server/document"
	"github.com/kro-run/kro/tools/lsp/server/validation"
)

// KroServer represents the core language server instance
// It coordinates all components and manages server lifecycle
type KroServer struct {
	router *Router
	logger commonlog.Logger

	// Managers
	documentManager   *document.Manager
	validationManager *validation.Manager
}

// NewKroServer creates a new Kro LSP server instance using simplified validation
func NewKroServer(logger commonlog.Logger) *KroServer {
	// Initialize document manager
	documentManager := document.NewManager(logger)

	// Initialize validation manager using Kro validation functions directly
	validationManager := validation.NewManager(logger, documentManager)

	server := &KroServer{
		logger:            logger,
		documentManager:   documentManager,
		validationManager: validationManager,
	}

	// Create router
	server.router = NewRouter(server)

	return server
}

// Initialize handles the LSP initialize request
func (s *KroServer) Initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Info("Initializing Kro Language Server using Kro validation functions")

	// Define server capabilities
	capabilities := s.createServerCapabilities()

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo:   nil,
	}, nil
}

// Initialized handles the initialized notification
func (s *KroServer) Initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	s.logger.Info("Server initialized with Kro validation")
	return nil
}

// Shutdown handles the shutdown request
func (s *KroServer) Shutdown(context *glsp.Context) error {
	s.logger.Info("Shutting down server")
	return nil
}

// createServerCapabilities defines what features this LSP server supports
func (s *KroServer) createServerCapabilities() protocol.ServerCapabilities {
	syncKind := protocol.TextDocumentSyncKindIncremental
	capabilities := protocol.ServerCapabilities{
		// Document synchronization
		TextDocumentSync: protocol.TextDocumentSyncOptions{
			OpenClose: boolPtr(true),
			Change:    &syncKind,
			Save: &protocol.SaveOptions{
				IncludeText: boolPtr(true),
			},
		},

		// Language features (basic capabilities)
		// HoverProvider: boolPtr(true),
		// CompletionProvider: &protocol.CompletionOptions{
		// 	TriggerCharacters: []string{".", ":", "-", " "},
		// },

		// Advanced features (will be implemented later)
		// DefinitionProvider: boolPtr(true),
		// CodeActionProvider: boolPtr(true),
		// DocumentFormattingProvider: boolPtr(true),
	}

	return capabilities
}

// GetValidationManager returns the validation manager
func (s *KroServer) GetValidationManager() ValidationManagerInterface {
	return s.validationManager
}

// ValidationManagerInterface defines the interface for validation managers
type ValidationManagerInterface interface {
	ValidateDocument(ctx context.Context, uri string) error
	ClearDiagnostics(uri string)
	SetDiagnosticPublisher(publisher validation.DiagnosticPublisher)
}

// GetValidationInfo returns information about validation system
func (s *KroServer) GetValidationInfo() map[string]interface{} {
	return map[string]interface{}{
		"enabled": true,
		"type":    "kro-validation",
		"message": "Using Kro validation functions",
	}
}

// Helper function to create bool pointers
func boolPtr(b bool) *bool {
	return &b
}
