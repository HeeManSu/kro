package validation

import (
	"context"
	"sync"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/kro-run/kro/tools/lsp/server/document"
)

// Manager coordinates all validation operations
type Manager struct {
	logger          commonlog.Logger
	documentManager *document.Manager

	// Main RGD validator
	rgdValidator *RGDValidator

	// CRD Manager for schema validation
	crdManager *CRDManager

	// Diagnostic publishing
	diagnosticPublisher DiagnosticPublisher

	mu sync.RWMutex
}

// DiagnosticPublisher interface for publishing diagnostics to the client
type DiagnosticPublisher interface {
	PublishDiagnostics(uri string, diagnostics []protocol.Diagnostic)
}

// NewManager creates a new validation manager
func NewManager(logger commonlog.Logger, docManager *document.Manager) *Manager {
	return &Manager{
		logger:          logger,
		documentManager: docManager,
	}
}

// NewManagerWithCRD creates a new validation manager with CRD support
func NewManagerWithCRD(logger commonlog.Logger, docManager *document.Manager, crdManager *CRDManager) *Manager {
	vm := &Manager{
		logger:          logger,
		documentManager: docManager,
		crdManager:      crdManager,
	}

	// Initialize RGD validator with CRD support
	vm.rgdValidator = NewRGDValidator(logger, crdManager)

	return vm
}

// SetDiagnosticPublisher sets the diagnostic publisher for sending results to client
func (vm *Manager) SetDiagnosticPublisher(publisher DiagnosticPublisher) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.diagnosticPublisher = publisher
}

// ValidateDocument performs comprehensive validation on a document
func (vm *Manager) ValidateDocument(ctx context.Context, uri string) error {
	vm.logger.Debugf("Validating document: %s", uri)

	var allDiagnostics []protocol.Diagnostic

	// Get document and model
	doc, exists := vm.documentManager.GetDocument(uri)
	if !exists {
		vm.logger.Warningf("Document not found for validation: %s", uri)
		return nil
	}

	model, exists := vm.documentManager.GetDocumentModel(uri)
	if !exists || model == nil {
		// Document parsing failed, create a syntax error diagnostic
		errorLevel := protocol.DiagnosticSeverityError
		syntaxDiag := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 10},
			},
			Severity: &errorLevel,
			Message:  "Failed to parse YAML document",
		}
		allDiagnostics = append(allDiagnostics, syntaxDiag)
		vm.publishDiagnostics(uri, allDiagnostics)
		return nil
	}

	// Determine document type and apply specific validation
	docType := vm.documentManager.GetDocumentType(uri)

	switch docType {
	case document.DocumentTypeRGD:
		vm.logger.Debugf("Validating RGD document: %s", uri)

		// Use RGD validator if available
		if vm.rgdValidator != nil {
			rgdDiagnostics := vm.rgdValidator.ValidateRGD(ctx, model, doc.Content)
			allDiagnostics = append(allDiagnostics, rgdDiagnostics...)
		} else {
			vm.logger.Warningf("No RGD validator available for %s", uri)
		}

	case document.DocumentTypeYAML:
		// Generic YAML - no specific validation
		vm.logger.Debugf("Document %s is generic YAML, no specific validation", uri)

	default:
		vm.logger.Debugf("Unknown document type for %s", uri)
	}

	// Publish all diagnostics
	vm.publishDiagnostics(uri, allDiagnostics)

	vm.logger.Debugf("Validation completed for %s with %d diagnostics", uri, len(allDiagnostics))
	return nil
}

// ValidateAllDocuments validates all open documents
func (vm *Manager) ValidateAllDocuments(ctx context.Context) error {
	uris := vm.documentManager.GetAllDocuments()

	for _, uri := range uris {
		if err := vm.ValidateDocument(ctx, uri); err != nil {
			vm.logger.Errorf("Error validating document %s: %v", uri, err)
		}
	}

	return nil
}

// ClearDiagnostics clears all diagnostics for a document
func (vm *Manager) ClearDiagnostics(uri string) {
	vm.publishDiagnostics(uri, []protocol.Diagnostic{})
}

// publishDiagnostics publishes diagnostics to the client
func (vm *Manager) publishDiagnostics(uri string, diagnostics []protocol.Diagnostic) {
	vm.mu.RLock()
	publisher := vm.diagnosticPublisher
	vm.mu.RUnlock()

	if publisher != nil {
		publisher.PublishDiagnostics(uri, diagnostics)
	} else {
		vm.logger.Warningf("No diagnostic publisher set, cannot publish %d diagnostics for %s", len(diagnostics), uri)
	}
}
