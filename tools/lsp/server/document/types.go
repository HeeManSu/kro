package document

import "sync"

// DocumentType represents the type of document being processed
type DocumentType int

const (
	// DocumentTypeUnknown represents an unrecognized document type
	DocumentTypeUnknown DocumentType = iota

	// DocumentTypeYAML represents a generic YAML document
	DocumentTypeYAML

	// DocumentTypeRGD represents a ResourceGraphDefinition document
	DocumentTypeRGD
)

// Document represents a text document
type Document struct {
	URI     string
	Version int32
	Content string
}

// DocumentStore manages document storage and retrieval
type DocumentStore struct {
	documents map[string]*Document
	mu        sync.RWMutex
}

// NewDocumentStore creates a new document store
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		documents: make(map[string]*Document),
	}
}

// Open stores a new document
func (ds *DocumentStore) Open(uri string, version int32, content string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.documents[uri] = &Document{
		URI:     uri,
		Version: version,
		Content: content,
	}
}

// Update updates an existing document
func (ds *DocumentStore) Update(uri string, version int32, content string) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if doc, exists := ds.documents[uri]; exists {
		doc.Version = version
		doc.Content = content
		return true
	}
	return false
}

// Close removes a document from the store
func (ds *DocumentStore) Close(uri string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	delete(ds.documents, uri)
}

// Get retrieves a document from the store
func (ds *DocumentStore) Get(uri string) (*Document, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	doc, exists := ds.documents[uri]
	return doc, exists
}
