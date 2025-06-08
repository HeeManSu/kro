package handlers

import (
	"fmt"
	"strings"

	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/kro-run/kro/tools/lsp/server/document"
)

// HoverHandler handles hover requests for RGD files
type HoverHandler struct {
	logger          commonlog.Logger
	documentManager *document.Manager
}

// NewHoverHandler creates a new hover handler
func NewHoverHandler(logger commonlog.Logger, documentManager *document.Manager) *HoverHandler {
	return &HoverHandler{
		logger:          logger,
		documentManager: documentManager,
	}
}

// Hover provides hover information for RGD files
func (h *HoverHandler) Hover(glspContext *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	h.logger.Debugf("Hover requested for %s at position %d:%d",
		params.TextDocument.URI, params.Position.Line, params.Position.Character)

	// Get document content
	doc, exists := h.documentManager.GetDocument(params.TextDocument.URI)
	if !exists {
		h.logger.Debugf("Document not found: %s", params.TextDocument.URI)
		return nil, nil
	}

	// Check if this is an RGD file
	docType := h.documentManager.GetDocumentType(params.TextDocument.URI)
	if docType != document.DocumentTypeRGD {
		h.logger.Debugf("Document is not an RGD file: %s", params.TextDocument.URI)
		return nil, nil
	}

	// Basic hover content for RGD files
	content := h.getHoverContent(doc, params.Position)
	if content == "" {
		return nil, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content,
		},
		Range: &protocol.Range{
			Start: params.Position,
			End: protocol.Position{
				Line:      params.Position.Line,
				Character: params.Position.Character + 10, // Approximate range
			},
		},
	}, nil
}

// getHoverContent generates hover content based on the cursor position
func (h *HoverHandler) getHoverContent(doc *document.Document, pos protocol.Position) string {
	// Get the word at cursor position (basic implementation)
	line := int(pos.Line)
	char := int(pos.Character)

	// Split content into lines
	lines := strings.Split(doc.Content, "\n")
	if line >= len(lines) {
		return ""
	}

	currentLine := lines[line]
	if char >= len(currentLine) {
		return ""
	}

	// Simple hover content based on common RGD fields
	switch {
	case containsWord(currentLine, "apiVersion"):
		return "**apiVersion**: Specifies the API version for this ResourceGraphDefinition\n\nShould be `kro.run/v1alpha1` for RGD files"

	case containsWord(currentLine, "kind"):
		return "**kind**: Specifies the resource type\n\nShould be `ResourceGraphDefinition` for RGD files"

	case containsWord(currentLine, "metadata"):
		return "**metadata**: Standard Kubernetes metadata\n\nContains name, namespace, labels, annotations, etc."

	case containsWord(currentLine, "schema"):
		return "**schema**: Defines the custom resource schema\n\nIncludes kind, apiVersion, spec, and status definitions"

	case containsWord(currentLine, "resources"):
		return "**resources**: List of Kubernetes resources to create\n\nEach resource can have templates, external references, and conditions"

	case containsWord(currentLine, "template"):
		return "**template**: Kubernetes resource template\n\nDefines the resource manifest to be created"

	case containsWord(currentLine, "externalRef"):
		return "**externalRef**: Reference to an external resource\n\nPoints to existing Kubernetes resources"

	case containsWord(currentLine, "readyWhen"):
		return "**readyWhen**: Conditions that determine when this resource is ready\n\nUses CEL expressions to evaluate resource state"

	case containsWord(currentLine, "includeWhen"):
		return "**includeWhen**: Conditions that determine when to include this resource\n\nUses CEL expressions for conditional resource creation"

	default:
		return "**Kro ResourceGraphDefinition**\n\nDefines a custom resource and its associated Kubernetes resources"
	}
}

// containsWord checks if a line contains a specific word
func containsWord(line, word string) bool {
	return len(line) > 0 && (line[0:minInt(len(word), len(line))] == word ||
		fmt.Sprintf("%s:", word) == line[0:minInt(len(word)+1, len(line))])
}

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
