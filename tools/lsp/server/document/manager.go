package document

import (
	"sync"

	"github.com/kro-run/kro/tools/lsp/server/parser"
	"github.com/tliron/commonlog"
)

// - Manages document lifecycle (open, update, close)
// - Parses YAML content and maintains document models
// - Detects document types (RGD vs generic YAML)

// Manager coordinates document lifecycle and parsed models
type Manager struct {
	logger        commonlog.Logger
	documentStore *DocumentStore
	yamlParser    *parser.YAMLParser

	// Store parsed models separately from raw documents
	modelStore map[string]*parser.DocumentModel
	modelMu    sync.RWMutex
}

// NewManager creates a new document manager
func NewManager(logger commonlog.Logger) *Manager {
	return &Manager{
		logger:        logger,
		documentStore: NewDocumentStore(),
		yamlParser:    parser.NewYAMLParser(),
		modelStore:    make(map[string]*parser.DocumentModel),
	}
}

// OpenDocument handles when a document is opened
func (m *Manager) OpenDocument(uri string, version int32, content string) error {
	m.logger.Infof("Opening document: %s", uri)

	// Store the raw document
	m.documentStore.Open(uri, version, content)

	// Parse and store the model
	return m.parseAndStoreModel(uri, content)
}

// UpdateDocument handles when a document is changed
func (m *Manager) UpdateDocument(uri string, version int32, content string) error {
	m.logger.Debugf("Updating document: %s (version %d)", uri, version)

	// Update the raw document
	if !m.documentStore.Update(uri, version, content) {
		m.logger.Warningf("Attempted to update unknown document: %s", uri)
		return nil
	}

	// Re-parse and store the model
	return m.parseAndStoreModel(uri, content)
}

// CloseDocument handles when a document is closed
func (m *Manager) CloseDocument(uri string) error {
	m.logger.Infof("Closing document: %s", uri)

	// Remove from document store
	m.documentStore.Close(uri)

	// Remove from model store
	m.modelMu.Lock()
	delete(m.modelStore, uri)
	m.modelMu.Unlock()

	return nil
}

// GetDocument retrieves a document from the store
func (m *Manager) GetDocument(uri string) (*Document, bool) {
	return m.documentStore.Get(uri)
}

// GetDocumentModel retrieves the parsed model for a document
func (m *Manager) GetDocumentModel(uri string) (*parser.DocumentModel, bool) {
	m.modelMu.RLock()
	defer m.modelMu.RUnlock()

	model, exists := m.modelStore[uri]
	return model, exists
}

// FindNodeAtPosition finds the most specific node at a given position
func (m *Manager) FindNodeAtPosition(uri string, pos parser.Position) (*parser.Node, error) {
	model, exists := m.GetDocumentModel(uri)
	if !exists {
		return nil, nil
	}

	return model.FindNodeAtPosition(pos), nil
}

// GetDocumentType determines if this is an RGD or other YAML document
func (m *Manager) GetDocumentType(uri string) DocumentType {
	model, exists := m.GetDocumentModel(uri)
	if !exists {
		return DocumentTypeUnknown
	}

	// Check if this is a ResourceGraphDefinition
	if apiVersion, hasAPIVersion := model.RootMap["apiVersion"]; hasAPIVersion {
		if kind, hasKind := model.RootMap["kind"]; hasKind {
			apiVersionStr, isAPIVersionString := apiVersion.Value.(string)
			kindStr, isKindString := kind.Value.(string)
			if isAPIVersionString && isKindString {
				if apiVersionStr == "kro.run/v1alpha1" && kindStr == "ResourceGraphDefinition" {
					return DocumentTypeRGD
				}
			}
		}
	}

	return DocumentTypeYAML
}

// parseAndStoreModel parses content and stores the resulting model
func (m *Manager) parseAndStoreModel(uri string, content string) error {
	model, err := m.yamlParser.Parse(content)
	if err != nil {
		m.logger.Warningf("Failed to parse document %s: %v", uri, err)
		// Store a nil model to indicate parsing failed
		m.modelMu.Lock()
		m.modelStore[uri] = nil
		m.modelMu.Unlock()
		return err
	}

	// Store the successfully parsed model
	m.modelMu.Lock()
	m.modelStore[uri] = model
	m.modelMu.Unlock()

	m.logger.Debugf("Successfully parsed and stored model for %s", uri)
	return nil
}

// GetAllDocuments returns all tracked documents
func (m *Manager) GetAllDocuments() []string {
	m.modelMu.RLock()
	defer m.modelMu.RUnlock()

	var uris []string
	for uri := range m.modelStore {
		uris = append(uris, uri)
	}
	return uris
}
