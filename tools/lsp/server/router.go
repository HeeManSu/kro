package main

import (
	"github.com/kro-run/kro/tools/lsp/server/handlers"
	"github.com/kro-run/kro/tools/lsp/server/validation"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Router struct {
	server          *kroServer
	documentHandler *handlers.DocumentHandler
}

func NewRouter(server *kroServer) *Router {
	// Create a minimal temporary validation manager for initialization
	// This will be replaced with the real one in Initialize()
	tempValidationManager := validation.NewValidationManager(server.logger, "TEMP_WORKSPACE_ROOT")
	documentHandler := handlers.NewDocumentHandler(server.logger, tempValidationManager)

	return &Router{
		server:          server,
		documentHandler: documentHandler,
	}
}

// UpdateValidationManager updates the document handler with the real validation manager
func (r *Router) UpdateValidationManager(validationManager *validation.ValidationManager) {
	// Recreate document handler with the real validation manager
	r.documentHandler = handlers.NewDocumentHandler(r.server.logger, validationManager)
	r.server.logger.Info("📝 Document handler updated with real ValidationManager")
}

// Dynamic method wrappers that always use the current document handler
func (r *Router) didOpen(context *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	return r.documentHandler.DidOpen(context, params)
}

func (r *Router) didChange(context *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	return r.documentHandler.DidChange(context, params)
}

func (r *Router) didClose(context *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	return r.documentHandler.DidClose(context, params)
}

func (r *Router) didSave(context *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	return r.documentHandler.DidSave(context, params)
}

func (r *Router) createHandler() *protocol.Handler {

	handler := &protocol.Handler{

		// Lifecycle methods
		Initialize:  r.server.Initialize,
		Initialized: r.server.Initialized,
		Shutdown:    r.server.Shutdown,

		// Document synchronization methods - use dynamic wrappers
		TextDocumentDidOpen:   r.didOpen,
		TextDocumentDidChange: r.didChange,
		TextDocumentDidClose:  r.didClose,
		TextDocumentDidSave:   r.didSave,

		// Workspace methods
		WorkspaceDidChangeWatchedFiles: r.server.WorkspaceDidChangeWatchedFiles,

		// Optional notifications
		SetTrace: r.server.SetTrace,

		// Language feature methods
	}

	return handler
}
