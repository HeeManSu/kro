package document

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/kro-run/kro/tools/lsp/server/parser"
	"github.com/kro-run/kro/tools/lsp/server/validation"
	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type DiagnosticPublisher interface {
	PublishDiagnostics(uri string, diagnostics []protocol.Diagnostic)
}

type Manager struct {
	logger              commonlog.Logger
	documentStore       *DocumentStore
	parser              *parser.YamlParser
	validationManager   *validation.ValidationManager
	diagnosticPublisher DiagnosticPublisher
}

func NewManager(logger commonlog.Logger, validationManager *validation.ValidationManager) *Manager {
	return &Manager{
		logger:            logger,
		documentStore:     NewDocumentStore(),
		parser:            parser.NewYAMLParser(logger),
		validationManager: validationManager,
	}
}

func (m *Manager) SetDiagnosticPublisher(publisher DiagnosticPublisher) {
	m.diagnosticPublisher = publisher
}

func (m *Manager) OpenDocument(uri string, version int32, content string) error {
	m.logger.Infof("Opening document: %s", uri)
	m.documentStore.Open(uri, version, content)
	m.parseAndValidate(uri)

	return nil
}

func (m *Manager) UpdateDocument(uri string, version int32, content string) error {
	m.logger.Infof("Updating document: %s (version %d, content length: %d)", uri, version, len(content))

	if !m.documentStore.Update(uri, version, content) {
		return nil
	}

	m.parseAndValidate(uri)
	return nil
}

func (m *Manager) CloseDocument(uri string) error {
	m.logger.Infof("Closing document: %s", uri)
	m.documentStore.Close(uri)
	return nil
}

func (m *Manager) GetDocument(uri string) (*Document, bool) {
	return m.documentStore.Get(uri)
}

func (m *Manager) GetDocumentModel(uri string) (*Document, bool) {
	return m.documentStore.Get(uri)
}

func (m *Manager) ParseDocument(uri string) (*parser.ParsedYAML, error) {
	doc, exists := m.documentStore.Get(uri)
	if !exists {
		return nil, fmt.Errorf("document not found: %s", uri)
	}

	parsed, err := m.parser.Parse(doc.Content, uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse document: %w", err)
	}

	return parsed, nil
}

func (m *Manager) parseAndValidate(uri string) {
	ctx := context.Background()
	var finalDiagnostics []protocol.Diagnostic
	defer func() {
		m.diagnosticPublisher.PublishDiagnostics(uri, finalDiagnostics)
	}()

	// Parse the document
	parsed, err := m.ParseDocument(uri)
	if err != nil {
		if m.isYAMLFile(uri) {
			errorMessage := fmt.Sprintf("Failed to parse YAML: %v", err)

			// Extract all position information from error message
			positions := extractAllPositionsFromYAMLError(err.Error())

			if len(positions) == 0 {
				// No positions found, default to line 1, column 1
				positions = []struct{ line, col int }{{1, 1}}
			}

			var content string
			if doc, exists := m.documentStore.Get(uri); exists {
				content = doc.Content
			}

			for i, pos := range positions {
				// Convert to 0-based indexing
				startLine := uint32(pos.line - 1)
				startCol := uint32(pos.col - 1)

				endLine, endCol := calculateErrorEndPosition(content, pos.line, pos.col)
				endLineLSP := uint32(endLine - 1)
				endColLSP := uint32(endCol - 1)

				var message string
				if len(positions) > 1 {
					if i == 0 {
						message = fmt.Sprintf("Failed to parse YAML: %v", err)
					} else {
						message = fmt.Sprintf("Related: %v", err)
					}
				} else {
					message = errorMessage
				}

				diagnostic := protocol.Diagnostic{
					Range: protocol.Range{
						Start: protocol.Position{Line: startLine, Character: startCol},
						End:   protocol.Position{Line: endLineLSP, Character: endColLSP},
					},
					Severity: func() *protocol.DiagnosticSeverity {
						severity := protocol.DiagnosticSeverityError
						return &severity
					}(),
					Message: message,
					Source:  func() *string { s := "kro-lsp"; return &s }(),
				}
				finalDiagnostics = append(finalDiagnostics, diagnostic)
			}
		} else {
			// Clear diagnostics for non-YAML files
			finalDiagnostics = []protocol.Diagnostic{}
		}
		return
	}

	rgdValidator := validation.NewRGDValidator(m.logger)
	if !rgdValidator.IsRGDFile(parsed) {
		finalDiagnostics = []protocol.Diagnostic{}
		return
	}

	result := m.validationManager.ValidateDocument(ctx, uri, parsed)

	// Convert validation errors to LSP diagnostics
	finalDiagnostics = m.convertValidationErrors(result)

	if finalDiagnostics == nil {
		finalDiagnostics = []protocol.Diagnostic{}
	}
}

func (m *Manager) isYAMLFile(uri string) bool {
	lowerURI := strings.ToLower(uri)
	return strings.HasSuffix(lowerURI, ".yaml") || strings.HasSuffix(lowerURI, ".yml")
}

func (m *Manager) convertValidationErrors(result *validation.ValidationResult) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Note: Keep only errors
	for _, err := range result.Errors {
		severity := protocol.DiagnosticSeverityError
		switch err.Severity {
		case "error":
			severity = protocol.DiagnosticSeverityError
		case "warning":
			severity = protocol.DiagnosticSeverityWarning
		case "info":
			severity = protocol.DiagnosticSeverityInformation
		}

		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(err.Range.Start.Line - 1), // LSP is 0-based
					Character: uint32(err.Range.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      uint32(err.Range.End.Line - 1),
					Character: uint32(err.Range.End.Column - 1),
				},
			},
			Severity: &severity,
			Message:  err.Message,
			Source:   &err.Source,
		}
		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

func extractAllPositionsFromYAMLError(errorMsg string) []struct{ line, col int } {
	// Pattern to match [line:column] format in error messages
	re := regexp.MustCompile(`\[(\d+):(\d+)\]`)
	matches := re.FindAllStringSubmatch(errorMsg, -1)

	var positions []struct{ line, col int }
	for _, match := range matches {
		if len(match) >= 3 {
			if l, err := strconv.Atoi(match[1]); err == nil {
				if c, err := strconv.Atoi(match[2]); err == nil {
					positions = append(positions, struct{ line, col int }{l, c})
				}
			}
		}
	}

	return positions
}

func calculateErrorEndPosition(content string, line, col int) (endLine, endCol int) {
	lines := strings.Split(content, "\n")

	// Convert to 0-based indexing for array access
	lineIndex := line - 1
	colIndex := col - 1

	if lineIndex < 0 || lineIndex >= len(lines) {
		return line, col + 10 // fallback
	}

	currentLine := lines[lineIndex]

	// Find the word/key at the position
	if colIndex >= len(currentLine) {
		return line, len(currentLine) + 1
	}

	// Find the end of the current word/key
	endIndex := colIndex
	for endIndex < len(currentLine) && currentLine[endIndex] != ' ' && currentLine[endIndex] != ':' && currentLine[endIndex] != '\t' {
		endIndex++
	}

	// If we found a word, use its end, otherwise extend by a few characters
	if endIndex > colIndex {
		return line, endIndex + 1
	}

	return line, col + 10 // fallback
}
