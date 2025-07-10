package handlers

import (
	"github.com/kro-run/kro/tools/lsp/server/document"
	"github.com/kro-run/kro/tools/lsp/server/validation"
	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type DocumentHandler struct {
	logger          commonlog.Logger
	documentManager *document.Manager
	context         *glsp.Context
}

func NewDocumentHandler(logger commonlog.Logger, validationManager *validation.ValidationManager) *DocumentHandler {
	return &DocumentHandler{
		logger:          logger,
		documentManager: document.NewManager(logger, validationManager),
	}
}

func (h *DocumentHandler) SetContext(context *glsp.Context) {
	h.context = context
	h.documentManager.SetDiagnosticPublisher(h)
}

func (h *DocumentHandler) PublishDiagnostics(uri string, diagnostics []protocol.Diagnostic) {

	if h.context != nil {
		h.context.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diagnostics,
		})
	} else {
		h.logger.Warningf("No LSP context available, cannot publish diagnostics for %s", uri)
	}
}

func (h *DocumentHandler) DidOpen(glspContext *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := params.TextDocument.URI
	version := params.TextDocument.Version
	content := params.TextDocument.Text

	if h.context == nil {
		h.SetContext(glspContext)
	}

	if err := h.documentManager.OpenDocument(uri, version, content); err != nil {
		return err
	}

	return nil
}

func (h *DocumentHandler) DidChange(glspContext *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	uri := params.TextDocument.URI
	version := params.TextDocument.Version

	if h.context == nil {
		h.SetContext(glspContext)
	}

	if _, exists := h.documentManager.GetDocument(uri); !exists {
		return nil
	}

	if len(params.ContentChanges) == 0 {
		return nil
	}

	change := params.ContentChanges[0]

	var newContent string
	var found bool

	// Find better solution for this
	// TextDocumentContentChangeEventWhole type (most common for full sync)
	if changeEvent, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
		newContent = changeEvent.Text
		found = true
	}

	if !found {
		if changeEvent, ok := change.(protocol.TextDocumentContentChangeEvent); ok {
			newContent = changeEvent.Text
			found = true
		}
	}

	if !found {
		if changeMap, ok := change.(map[string]interface{}); ok {
			if text, textOk := changeMap["text"].(string); textOk {
				newContent = text
				found = true
			}
		}
	}

	if !found {
		return nil
	}

	if err := h.documentManager.UpdateDocument(uri, version, newContent); err != nil {
		return err
	}

	return nil
}

func (h *DocumentHandler) DidClose(glspContext *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI

	h.PublishDiagnostics(uri, []protocol.Diagnostic{})

	if err := h.documentManager.CloseDocument(uri); err != nil {
		return err
	}

	return nil
}

func (h *DocumentHandler) DidSave(glspContext *glsp.Context, params *protocol.DidSaveTextDocumentParams) error {
	uri := params.TextDocument.URI

	doc, exists := h.documentManager.GetDocument(uri)
	if !exists {
		return nil
	}

	if params.Text != nil {
		if err := h.documentManager.UpdateDocument(uri, doc.Version, *params.Text); err != nil {
			return err
		}
	} else {
		if err := h.documentManager.UpdateDocument(uri, doc.Version, doc.Content); err != nil {
			return err
		}
	}

	return nil
}
