package document

import "sync"

// It stores all the documents (Yaml Files) that are opened, changed, closed or being worked with in vs code.

type Document struct {
	URI     string
	Version int32
	Content string
}

type DocumentStore struct {
	documents map[string]*Document
	mu        sync.RWMutex
}

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

// thread-Safe Storage
// Uses sync.RWMutex for concurrent access
// Multiple requests can read simultaneously
// Only one can write at a time

// the DocumentStore is like a central repository that stores ALL YAML documents that are opened, changed, or worked with in VS Code.

// User opens file.yaml in VS Code
//     ↓
// VS Code sends "textDocument/didOpen" event
//     ↓
// LSP Server receives the event
//     ↓
// DocumentStore.Open(uri, version, content) stores the document

// When you have 3 files open in VS Code:
// documents = {
//     "file:///project/rgd-webapp.yaml": &Document{
//         URI: "file:///project/rgd-webapp.yaml",
//         Version: 5,  // Updated 5 times
//         Content: "apiVersion: kro.run/v1alpha1\nkind: ResourceGraphDefinition\n..."
//     },
//     "file:///project/config.yaml": &Document{
//         URI: "file:///project/config.yaml",
//         Version: 2,
//         Content: "database:\n  host: localhost\n..."
//     },
//     "file:///project/deployment.yaml": &Document{
//         URI: "file:///project/deployment.yaml",
//         Version: 1,
//         Content: "apiVersion: apps/v1\nkind: Deployment\n..."
//     }
// }

// Why sync.RWMutex is used not the normal mutex?
// With a regular sync.Mutex, ONLY ONE operation can happen at a time - whether it's reading OR writing.

// The LSP Server Reality
// In VS Code with LSP, you have MANY concurrent read operations happening simultaneously:

// User types in file1.yaml
//     ↓
// VS Code sends validation request for file1.yaml
//     ↓
// LSP needs to read file1.yaml content

// SIMULTANEOUSLY:
// - Hover request for file2.yaml (needs to read file2.yaml)
// - Auto-complete for file3.yaml (needs to read file3.yaml)
// - Diagnostic update for file4.yaml (needs to read file4.yaml)
// - Syntax highlighting for file5.yaml (needs to read file5.yaml)

// Regular Mutex = Performance Bottleneck
// With sync.Mutex - TERRIBLE performance
// Thread 1: Get(file1.yaml) → LOCK → read → UNLOCK
// Thread 2: Get(file2.yaml) → WAIT... WAIT... WAIT... → LOCK → read → UNLOCK
// Thread 3: Get(file3.yaml) → WAIT... WAIT... WAIT... WAIT... → LOCK → read → UNLOCK
// Thread 4: Get(file4.yaml) → WAIT... WAIT... WAIT... WAIT... WAIT... → LOCK → read → UNLOCK

// Result: Everything is SEQUENTIAL even though reads don't conflict!

// RWMutex = Smart Concurrency
// With sync.RWMutex - EXCELLENT performance
// Thread 1: Get(file1.yaml) → RLock() → read → RUnlock()
// Thread 2: Get(file2.yaml) → RLock() → read → RUnlock()  // PARALLEL!
// Thread 3: Get(file3.yaml) → RLock() → read → RUnlock()  // PARALLEL!
// Thread 4: Get(file4.yaml) → RLock() → read → RUnlock()  // PARALLEL!

// All reads happen SIMULTANEOUSLY!

// How RWMutex works ?
// WRITE operations (modify the map) - Exclusive lock
// func (ds *DocumentStore) Open(uri string, version int32, content string) {
//     ds.mu.Lock()        // EXCLUSIVE - blocks ALL other operations
//     defer ds.mu.Unlock()

//     ds.documents[uri] = &Document{...}  // MODIFYING the map
// }

// READ operations (just read the map) - Shared lock
// func (ds *DocumentStore) Get(uri string) (*Document, bool) {
//     ds.mu.RLock()       // SHARED - allows OTHER reads simultaneously
//     defer ds.mu.RUnlock()

//     doc, exists := ds.documents[uri]    // ONLY READING the map
//     return doc, exists
// }

// Two Types of Locks
// 1. Write Lock (Lock()) - Exclusive
// Used for: Open(), Update(), Close()
// Behavior: BLOCKS EVERYTHING - no other reads or writes
// Reason: Modifying the map structure is dangerous with concurrent access

// 2. Read Lock (RLock()) - Shared
// Used for: Get()
// Behavior: ALLOWS MULTIPLE READERS simultaneously
// Reason: Reading doesn't modify anything, so it's safe to do in parallel

// Real Performance Difference
// Let's say you have 100 concurrent read requests:
// With sync.Mutex:
// Time: 0ms → Read 1 starts
// Time: 1ms → Read 1 finishes, Read 2 starts
// Time: 2ms → Read 2 finishes, Read 3 starts
// ...
// Time: 100ms → Read 100 finishes

// Total time: 100ms (sequential)
// With sync.RWMutex:
// Time: 0ms → ALL 100 reads start simultaneously
// Time: 1ms → ALL 100 reads finish simultaneously

// Total time: 1ms (parallel)
