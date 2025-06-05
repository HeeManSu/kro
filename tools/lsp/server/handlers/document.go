package handlers

import (
	"context"

	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/kro-run/kro/tools/lsp/server/document"
)

// DocumentHandlerImpl implements the DocumentHandler interface
type DocumentHandlerImpl struct {
	logger            commonlog.Logger
	documentManager   *document.Manager
	validationManager ValidationManagerInterface
}

// ValidationManagerInterface defines the interface for validation managers
type ValidationManagerInterface interface {
	ValidateDocument(ctx context.Context, uri string) error
	ClearDiagnostics(uri string)
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(logger commonlog.Logger, docManager *document.Manager) *DocumentHandlerImpl {
	return &DocumentHandlerImpl{
		logger:          logger,
		documentManager: docManager,
	}
}

// SetValidationManager sets the validation manager for this handler
func (h *DocumentHandlerImpl) SetValidationManager(vm ValidationManagerInterface) {
	h.validationManager = vm
}

// DidOpen handles when a document is opened
func (h *DocumentHandlerImpl) DidOpen(glspContext *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := params.TextDocument.URI
	version := params.TextDocument.Version
	content := params.TextDocument.Text

	h.logger.Infof("Document opened: %s", uri)

	// Store and parse the document using document manager
	if err := h.documentManager.OpenDocument(uri, version, content); err != nil {
		h.logger.Errorf("Failed to open document %s: %v", uri, err)
		return err
	}

	// Check if this is an RGD file and trigger validation
	if h.isRGDFile(content) {
		h.logger.Debugf("Detected RGD file: %s", uri)
		if h.validationManager != nil {
			go func() {
				ctx := context.Background()
				if err := h.validationManager.ValidateDocument(ctx, uri); err != nil {
					h.logger.Errorf("Validation failed for %s: %v", uri, err)
				}
			}()
		}
	}

	return nil
}

// DidChange handles when a document is changed
func (h *DocumentHandlerImpl) DidChange(glspContext *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	uri := params.TextDocument.URI
	version := params.TextDocument.Version

	h.logger.Debugf("Document changed: %s (version %d)", uri, version)

	// Get the current document to calculate new content
	doc, exists := h.documentManager.GetDocument(uri)
	if !exists {
		h.logger.Warningf("Received change for unknown document: %s", uri)
		return nil
	}

	// Apply changes to get new content
	var contentChanges []protocol.TextDocumentContentChangeEvent
	for _, change := range params.ContentChanges {
		if changeEvent, ok := change.(protocol.TextDocumentContentChangeEvent); ok {
			contentChanges = append(contentChanges, changeEvent)
		}
	}

	// For incremental changes, we need to apply them to the current content
	newContent := doc.Content
	for _, change := range contentChanges {
		if change.Range == nil {
			// Full document change
			newContent = change.Text
		} else {
			// Incremental change - for simplicity, we'll treat it as full document change
			// In a production implementation, you'd want to properly apply incremental changes
			newContent = change.Text
		}
	}

	// Update the document using document manager
	if err := h.documentManager.UpdateDocument(uri, version, newContent); err != nil {
		h.logger.Errorf("Failed to update document %s: %v", uri, err)
		return err
	}

	// Trigger validation if this is an RGD file
	if h.isRGDFile(newContent) && h.validationManager != nil {
		go func() {
			ctx := context.Background()
			if err := h.validationManager.ValidateDocument(ctx, uri); err != nil {
				h.logger.Errorf("Validation failed for %s: %v", uri, err)
			}
		}()
	}

	return nil
}

// DidClose handles when a document is closed
func (h *DocumentHandlerImpl) DidClose(glspContext *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI

	h.logger.Infof("Document closed: %s", uri)

	// Clear diagnostics for this document
	if h.validationManager != nil {
		h.validationManager.ClearDiagnostics(uri)
	}

	// Remove the document using document manager
	if err := h.documentManager.CloseDocument(uri); err != nil {
		h.logger.Errorf("Failed to close document %s: %v", uri, err)
		return err
	}

	return nil
}

// DidSave handles when a document is saved
func (h *DocumentHandlerImpl) DidSave(glspContext *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	uri := params.TextDocument.URI

	h.logger.Debugf("Document saved: %s", uri)

	// If the save includes text, update our document
	if params.Text != nil {
		doc, exists := h.documentManager.GetDocument(uri)
		if exists {
			// Update with saved content
			if err := h.documentManager.UpdateDocument(uri, doc.Version, *params.Text); err != nil {
				h.logger.Errorf("Failed to update document %s on save: %v", uri, err)
				return err
			}

			// Trigger validation on save for RGD files
			if h.isRGDFile(*params.Text) && h.validationManager != nil {
				go func() {
					ctx := context.Background()
					if err := h.validationManager.ValidateDocument(ctx, uri); err != nil {
						h.logger.Errorf("Validation failed for %s: %v", uri, err)
					}
				}()
			}
		}
	}

	return nil
}

// isRGDFile checks if the content represents an RGD file
func (h *DocumentHandlerImpl) isRGDFile(content string) bool {
	// Simple check for RGD files by examining content
	return len(content) > 0 && containsRGDMarkers(content)
}

// containsRGDMarkers checks for RGD-specific markers in the content
func containsRGDMarkers(content string) bool {
	lines := []string{}
	currentLine := ""
	for _, r := range content {
		if r == '\n' {
			lines = append(lines, currentLine)
			currentLine = ""
			if len(lines) >= 5 { // Check first few lines only
				break
			}
		} else {
			currentLine += string(r)
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	hasKroAPIVersion := false
	hasRGDKind := false

	for _, line := range lines {
		if len(line) > 10 {
			if line[:10] == "apiVersion" && contains(line, "kro.run") {
				hasKroAPIVersion = true
			}
			if line[:4] == "kind" && contains(line, "ResourceGraphDefinition") {
				hasRGDKind = true
			}
		}
	}

	return hasKroAPIVersion && hasRGDKind
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
