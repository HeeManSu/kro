package main

import (
	"context"

	"github.com/kro-run/kro/tools/lsp/server/handlers"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Router handles routing of LSP requests to appropriate handlers
// It acts as a central dispatch point for all LSP method calls
type Router struct {
	server          *KroServer
	documentHandler *handlers.DocumentHandlerImpl
}

// NewRouter creates a new router instance
func NewRouter(server *KroServer) *Router {
	// Create document handler
	documentHandler := handlers.NewDocumentHandler(server.logger, server.documentManager)
	documentHandler.SetValidationManager(server.validationManager)

	return &Router{
		server:          server,
		documentHandler: documentHandler,
	}
}

// CreateHandler creates the LSP protocol handler with all method mappings
func (r *Router) CreateHandler(server *KroServer) *protocol.Handler {
	// Create diagnostic publisher
	diagnosticPublisher := &DiagnosticPublisherImpl{
		context: nil, // Will be set after handler creation
	}

	handler := &protocol.Handler{
		// Lifecycle methods
		Initialize: server.Initialize,
		Initialized: func(glspCtx *glsp.Context, params *protocol.InitializedParams) error {
			// Set the context for diagnostic publishing
			diagnosticPublisher.SetServerDispatcher(glspCtx)
			return server.Initialized(glspCtx, params)
		},
		Shutdown: server.Shutdown,

		// Document synchronization methods - delegate to document handler
		TextDocumentDidOpen:   r.documentHandler.DidOpen,
		TextDocumentDidChange: r.documentHandler.DidChange,
		TextDocumentDidClose:  r.documentHandler.DidClose,
		TextDocumentDidSave:   r.documentHandler.DidSave,

		// Workspace methods
		WorkspaceDidChangeWatchedFiles: r.handleWorkspaceDidChangeWatchedFiles,

		// Optional notifications
		SetTrace: r.handleSetTrace,

		// Language feature methods
		// These will be added as we implement each feature
		// TextDocumentCompletion:          r.completionHandler.Completion,
		// TextDocumentHover:               r.hoverHandler.Hover,
		// TextDocumentDefinition:          r.definitionHandler.Definition,
		// TextDocumentCodeAction:          r.codeActionHandler.CodeAction,
		// TextDocumentFormatting:          r.formattingHandler.Format,
		// TextDocumentRangeFormatting:     r.formattingHandler.RangeFormat,
	}

	// Set up diagnostic publishing
	r.server.validationManager.SetDiagnosticPublisher(diagnosticPublisher)

	return handler
}

// Workspace methods
func (r *Router) handleWorkspaceDidChangeWatchedFiles(glspContext *glsp.Context, params *protocol.DidChangeWatchedFilesParams) error {
	r.server.logger.Debugf("Workspace files changed: %d changes", len(params.Changes))

	// Handle file changes that might affect CRDs or validation
	for _, change := range params.Changes {
		r.server.logger.Debugf("File change: %s (type: %d)", change.URI, change.Type)

		// If CRD files changed, refresh CRDs
		if r.isCRDFile(change.URI) {
			go func() {
				ctx := context.Background()
				if err := r.server.RefreshCRDs(ctx); err != nil {
					r.server.logger.Errorf("Failed to refresh CRDs after file change: %v", err)
				}
			}()
		}
	}

	return nil
}

// isCRDFile checks if a file URI represents a CRD file
func (r *Router) isCRDFile(uri string) bool {
	// Simple check for CRD files based on path
	return false // TODO: Implement proper CRD file detection
}

// handleSetTrace handles trace notifications
func (r *Router) handleSetTrace(glspContext *glsp.Context, params *protocol.SetTraceParams) error {
	r.server.logger.Debugf("Trace set to: %s", params.Value)
	return nil
}

// DiagnosticPublisherImpl implements the DiagnosticPublisher interface
type DiagnosticPublisherImpl struct {
	context *glsp.Context
}

// PublishDiagnostics publishes diagnostics to the LSP client
func (dp *DiagnosticPublisherImpl) PublishDiagnostics(uri string, diagnostics []protocol.Diagnostic) {
	if dp.context != nil {
		// Ensure diagnostics is never nil
		if diagnostics == nil {
			diagnostics = []protocol.Diagnostic{}
		}

		dp.context.Notify(protocol.ServerTextDocumentPublishDiagnostics, &protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diagnostics,
		})
	}
}

// SetServerDispatcher sets the server context for publishing diagnostics
func (dp *DiagnosticPublisherImpl) SetServerDispatcher(context *glsp.Context) {
	dp.context = context
}
