package main

import (
	"github.com/kro-run/kro/tools/lsp/server/utils"
	"github.com/kro-run/kro/tools/lsp/server/validation"
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type kroServer struct {
	logger            commonlog.Logger
	router            *Router
	validationManager *validation.ValidationManager
}

func NewKroServer(logger commonlog.Logger) *kroServer {
	server := &kroServer{
		logger: logger,
	}

	server.router = NewRouter(server)
	return server
}

func (s *kroServer) Initialize(context *glsp.Context, params *protocol.InitializeParams) (any, error) {
	s.logger.Infof("Initializing Kro Language Server")

	// For development purposes: Examples and settings.json are in different directories
	// Initialize validation manager with workspace root
	if params.RootURI != nil {
		workspaceRoot := *params.RootURI
		s.logger.Debugf("Received RootURI: %s", workspaceRoot)
		if workspaceRoot != "" {
			// Remove file:// prefix if present
			if len(workspaceRoot) > 7 && workspaceRoot[:7] == "file://" {
				workspaceRoot = workspaceRoot[7:]
				s.logger.Debugf("After removing file:// prefix: %s", workspaceRoot)
			}

			s.logger.Infof("Initializing validation manager with workspace: %s", workspaceRoot)
			// Initialize validation manager with workspace root
			s.validationManager = validation.NewValidationManager(s.logger, workspaceRoot)
			s.logger.Info("Validation manager initialized successfully")
		} else {
			s.logger.Warningf("RootURI is empty string")
		}
	} else {
		s.logger.Warningf("RootURI is nil")
	}

	// Fallback: initialize without workspace if no root URI
	if s.validationManager == nil {
		s.logger.Warningf("No workspace root provided, initializing with current directory")
		s.validationManager = validation.NewValidationManager(s.logger, ".")
	}

	// Update document handlers with the real ValidationManager
	s.router.UpdateValidationManager(s.validationManager)

	capabilities := s.createServerCapabilities()

	s.logger.Infof("Sending server capabilities: TextDocumentSync=%+v", capabilities.TextDocumentSync)

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "Kro Language Server",
			Version: utils.StringPtr("0.0.1"),
		},
	}, nil
}

func (s *kroServer) Initialized(context *glsp.Context, params *protocol.InitializedParams) error {
	s.logger.Infof("Server initialized successfully")

	// Log CRD information
	// crdInfo := s.validationManager.GetCRDInfo()
	// s.logger.Infof("CRD validation status: %+v", crdInfo)

	return nil
}

func (s *kroServer) Shutdown(context *glsp.Context) error {
	s.logger.Info("Shutting down server")
	return nil
}

// SetTrace handles trace setting notifications
func (s *kroServer) SetTrace(context *glsp.Context, params *protocol.SetTraceParams) error {
	s.logger.Debugf("Trace set to: %s", params.Value)
	return nil
}

// WorkspaceDidChangeWatchedFiles handles file system change notifications
func (s *kroServer) WorkspaceDidChangeWatchedFiles(context *glsp.Context, params *protocol.DidChangeWatchedFilesParams) error {
	s.logger.Debugf("Workspace files changed: %d changes", len(params.Changes))
	return nil
}

func (s *kroServer) createServerCapabilities() protocol.ServerCapabilities {

	syncKind := protocol.TextDocumentSyncKindFull
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: protocol.TextDocumentSyncOptions{
			OpenClose: utils.BoolPtr(true),
			Change:    &syncKind,
			Save: &protocol.SaveOptions{
				IncludeText: utils.BoolPtr(true),
			},
		},

		// Language features (basic capabilities)
		// HoverProvider: utils.BoolPtr(true),
		// CompletionProvider: &protocol.CompletionOptions{
		// 	TriggerCharacters: []string{".", ":", "-", " "},
		// },

		// Advanced features (will be implemented later)
		// DefinitionProvider: utils.BoolPtr(true),
		// CodeActionProvider: utils.BoolPtr(true),
		// DocumentFormattingProvider: utils.BoolPtr(true),
	}

	return capabilities
}
