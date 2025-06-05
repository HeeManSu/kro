package main

import (
	"context"
	"time"

	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/kro-run/kro/tools/lsp/server/document"
	"github.com/kro-run/kro/tools/lsp/server/validation"
)

// KroServer represents the core language server instance
// It coordinates all components and manages server lifecycle
type KroServer struct {
	name    string
	version string
	router  *Router
	logger  commonlog.Logger

	// Managers
	documentManager   *document.Manager
	validationManager *validation.Manager

	// Configuration
	crdValidationEnabled bool

	// CRD management
	crdManager    *validation.CRDManager
	isInitialized bool
}

// ServerConfig contains configuration for the Kro LSP server
type ServerConfig struct {
	CRDValidationEnabled bool
	CRDConfig            validation.CRDManagerConfig
}

// NewKroServer creates a new Kro LSP server instance
func NewKroServer(logger commonlog.Logger) *KroServer {
	// Initialize document manager
	documentManager := document.NewManager(logger)

	// Initialize CRD manager from VS Code settings
	crdManager, err := validation.NewCRDManagerFromVSCodeSettings(logger, ".vscode/settings.json")
	if err != nil {
		logger.Warningf("Failed to initialize CRD manager from VS Code settings: %v", err)
		// Fallback to default configuration
		config := validation.CRDManagerConfig{
			RefreshInterval: 5 * time.Minute,
			ValidationMode:  validation.ValidationModePermissive,
			EnableLocal:     true,
			LocalPaths:      []string{"./crds"},
		}
		crdManager = validation.NewCRDManager(logger, config)
	}

	// Initialize validation manager with CRD support
	validationManager := validation.NewManagerWithCRD(logger, documentManager, crdManager)

	server := &KroServer{
		logger:               logger,
		documentManager:      documentManager,
		crdManager:           crdManager,
		validationManager:    validationManager,
		isInitialized:        false,
		crdValidationEnabled: true, // Enable CRD validation by default
	}

	// Create router
	server.router = NewRouter(server)

	// Load CRDs on startup
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if err := crdManager.LoadCRDs(ctx); err != nil {
			logger.Errorf("Failed to load CRDs on startup: %v", err)
		} else {
			logger.Info("Successfully loaded CRDs on startup")
		}
	}()

	return server
}

// Initialize handles the LSP initialize request
func (s *KroServer) Initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Info("Initializing Kro Language Server")

	// Log configuration
	if s.crdValidationEnabled {
		s.logger.Info("CRD validation is enabled")
	} else {
		s.logger.Info("CRD validation is disabled")
	}

	// Define server capabilities
	capabilities := s.createServerCapabilities()

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    s.name,
			Version: &s.version,
		},
	}, nil
}

// Initialized handles the initialized notification
func (s *KroServer) Initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	s.logger.Info("Server initialized")

	// Log CRD information if available
	if s.crdManager != nil {
		s.logger.Info("CRD validation system is active")
	}

	return nil
}

// Shutdown handles the shutdown request
func (s *KroServer) Shutdown(context *glsp.Context) error {
	s.logger.Info("Shutting down server")

	// Stop CRD manager if it exists
	if s.crdManager != nil {
		s.crdManager.Stop()
	}

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
		HoverProvider: boolPtr(true),
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{".", ":", "-", " "},
		},

		// Advanced features (will be implemented later)
		// DefinitionProvider: boolPtr(true),
		// CodeActionProvider: boolPtr(true),
		// DocumentFormattingProvider: boolPtr(true),
	}

	// Add additional capabilities if CRD validation is enabled
	if s.crdValidationEnabled {
		// Enhanced validation capabilities could be advertised here
		s.logger.Debug("Enhanced CRD validation capabilities enabled")
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

// RefreshCRDs manually refreshes CRDs (can be called via custom LSP command)
func (s *KroServer) RefreshCRDs(ctx context.Context) error {
	if s.crdManager == nil {
		return nil // CRD validation not enabled
	}

	return s.crdManager.RefreshCRDs(ctx)
}

// GetCRDInfo returns information about loaded CRDs
func (s *KroServer) GetCRDInfo() map[string]interface{} {
	if s.crdManager == nil {
		return map[string]interface{}{
			"enabled": false,
			"message": "CRD validation is disabled",
		}
	}

	return map[string]interface{}{
		"enabled": true,
		"message": "CRD validation is active",
	}
}

// Helper function to create bool pointers
func boolPtr(b bool) *bool {
	return &b
}
