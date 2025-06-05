package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// CRDManager handles CRD schema loading, caching, and validation
type CRDManager struct {
	logger commonlog.Logger

	// CRD storage and caching
	crdCache   map[string]*CRDSchema                  // URI -> CRDSchema
	gvkIndex   map[schema.GroupVersionKind]*CRDSchema // GVK -> CRDSchema
	cacheMutex sync.RWMutex

	// CRD sources
	sources []CRDSource

	// Configuration
	config CRDManagerConfig

	// Background refresh
	refreshTicker *time.Ticker
	stopChan      chan struct{}
}

// CRDManagerConfig contains configuration for the CRD manager
type CRDManagerConfig struct {
	RefreshInterval time.Duration
	ValidationMode  ValidationMode
	EnableCluster   bool
	EnableGitHub    bool
	EnableLocal     bool
	LocalPaths      []string
	GitHubRepos     []GitHubRepo
	ClusterConfigs  []ClusterConfig
	CacheConfig     CacheConfig
}

// ValidationMode defines how strict validation should be
type ValidationMode string

const (
	ValidationModeStrict     ValidationMode = "strict"
	ValidationModePermissive ValidationMode = "permissive"
	ValidationModeOff        ValidationMode = "off"
)

// GitHubRepo represents a GitHub repository configuration
type GitHubRepo struct {
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Branch  string `json:"branch"`
	Token   string `json:"token"`
	Enabled bool   `json:"enabled"`
}

// ClusterConfig represents a Kubernetes cluster configuration
type ClusterConfig struct {
	Name       string   `json:"name"`
	Kubeconfig string   `json:"kubeconfig"`
	Context    string   `json:"context"`
	Namespaces []string `json:"namespaces"`
	Enabled    bool     `json:"enabled"`
}

// CacheConfig represents caching configuration
type CacheConfig struct {
	Enabled   bool   `json:"enabled"`
	Directory string `json:"directory"`
	TTL       int    `json:"ttl"` // seconds
	MaxSize   string `json:"maxSize"`
}

// LocalConfig represents local CRD source configuration
type LocalConfig struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Watch   bool   `json:"watch"`
	Enabled bool   `json:"enabled"`
}

// CRDSchema represents a parsed CRD with validation information
type CRDSchema struct {
	CRD           *v1.CustomResourceDefinition
	GVK           schema.GroupVersionKind
	OpenAPISchema map[string]interface{}
	CELRules      []CELRule
	Source        string
	LastUpdate    time.Time
}

// CELRule represents a CEL validation rule
type CELRule struct {
	Rule    string
	Message string
	Path    string
}

// CRDSource interface for different CRD sources
type CRDSource interface {
	Name() string
	LoadCRDs(ctx context.Context) ([]*CRDSchema, error)
	SupportsWatch() bool
	Watch(ctx context.Context, callback func([]*CRDSchema)) error
}

// NewCRDManager creates a new CRD manager with the given configuration
func NewCRDManager(logger commonlog.Logger, config CRDManagerConfig) *CRDManager {
	manager := &CRDManager{
		logger:   logger,
		crdCache: make(map[string]*CRDSchema),
		gvkIndex: make(map[schema.GroupVersionKind]*CRDSchema),
		config:   config,
		stopChan: make(chan struct{}),
	}

	// Initialize sources based on configuration
	manager.initializeSources()

	// Start background refresh if interval is set
	if config.RefreshInterval > 0 {
		manager.startBackgroundRefresh()
	}

	return manager
}

// NewCRDManagerFromVSCodeSettings creates a CRD manager from VS Code settings
func NewCRDManagerFromVSCodeSettings(logger commonlog.Logger, settingsPath string) (*CRDManager, error) {
	config, err := loadVSCodeSettings(logger, settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load VS Code settings: %w", err)
	}

	return NewCRDManager(logger, config), nil
}

// loadVSCodeSettings loads CRD configuration from VS Code settings.json
func loadVSCodeSettings(logger commonlog.Logger, settingsPath string) (CRDManagerConfig, error) {
	config := CRDManagerConfig{
		RefreshInterval: 5 * time.Minute, // Default refresh interval
		ValidationMode:  ValidationModePermissive,
	}

	// Log the CWD
	if cwd, err := os.Getwd(); err == nil {
		logger.Infof("CRDManager: Current working directory: %s", cwd)
	} else {
		logger.Warningf("CRDManager: Failed to get current working directory: %v", err)
	}

	// Try multiple possible locations for VS Code settings
	possiblePaths := []string{}

	// Only add settingsPath if it's not empty and not already a default path
	if settingsPath != "" && settingsPath != ".vscode/settings.json" {
		possiblePaths = append(possiblePaths, settingsPath)
	}

	// Add common locations
	possiblePaths = append(possiblePaths,
		".vscode/settings.json",
		"../.vscode/settings.json",
		"../../.vscode/settings.json",
		"../../../.vscode/settings.json",
		// Add paths for sibling directories (common when working directory is kro-example)
		"../kro/.vscode/settings.json",
		"../kro/tools/lsp/client/.vscode/settings.json",
		"../kro/tools/lsp/.vscode/settings.json",
		// Add paths for deeper nesting
		"../../kro/.vscode/settings.json",
		"../../kro/tools/lsp/client/.vscode/settings.json",
		"../../../kro/.vscode/settings.json",
		"../../../kro/tools/lsp/client/.vscode/settings.json",
	)

	var data []byte
	var err error
	var usedPath string

	// Try each path until we find one that works
	for _, path := range possiblePaths {
		logger.Debugf("Trying to read settings from: %s", path)
		data, err = os.ReadFile(path)
		if err == nil {
			usedPath = path
			logger.Infof("Found VS Code settings at: %s", path)
			break
		}
		logger.Debugf("Failed to read %s: %v", path, err)
	}

	if err != nil {
		logger.Debugf("Could not find VS Code settings in any of these locations: %v", possiblePaths)
		return config, fmt.Errorf("failed to read settings file from any location: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return config, fmt.Errorf("failed to parse settings JSON from %s: %w", usedPath, err)
	}

	// Parse CRD sources configuration
	if sources, ok := settings["kro.lsp.crd.sources"].(map[string]interface{}); ok {
		// Parse cluster sources
		if clusters, ok := sources["clusters"].([]interface{}); ok {
			for _, cluster := range clusters {
				if clusterMap, ok := cluster.(map[string]interface{}); ok {
					clusterConfig := ClusterConfig{}
					if name, ok := clusterMap["name"].(string); ok {
						clusterConfig.Name = name
					}
					if kubeconfig, ok := clusterMap["kubeconfig"].(string); ok {
						clusterConfig.Kubeconfig = kubeconfig
					}
					if context, ok := clusterMap["context"].(string); ok {
						clusterConfig.Context = context
					}
					if namespaces, ok := clusterMap["namespaces"].([]interface{}); ok {
						for _, ns := range namespaces {
							if nsStr, ok := ns.(string); ok {
								clusterConfig.Namespaces = append(clusterConfig.Namespaces, nsStr)
							}
						}
					}
					if enabled, ok := clusterMap["enabled"].(bool); ok {
						clusterConfig.Enabled = enabled
					}
					config.ClusterConfigs = append(config.ClusterConfigs, clusterConfig)
					if clusterConfig.Enabled {
						config.EnableCluster = true
					}
				}
			}
		}

		// Parse GitHub sources
		if github, ok := sources["github"].([]interface{}); ok {
			for _, gh := range github {
				if ghMap, ok := gh.(map[string]interface{}); ok {
					repo := GitHubRepo{}
					if name, ok := ghMap["name"].(string); ok {
						repo.Name = name
					}
					if owner, ok := ghMap["owner"].(string); ok {
						repo.Owner = owner
					}
					if repoName, ok := ghMap["repo"].(string); ok {
						repo.Repo = repoName
					}
					if path, ok := ghMap["path"].(string); ok {
						repo.Path = path
					}
					if branch, ok := ghMap["branch"].(string); ok {
						repo.Branch = branch
					} else {
						repo.Branch = "main" // Default branch
					}
					if token, ok := ghMap["token"].(string); ok {
						repo.Token = token
					}
					if enabled, ok := ghMap["enabled"].(bool); ok {
						repo.Enabled = enabled
					}
					config.GitHubRepos = append(config.GitHubRepos, repo)
					if repo.Enabled {
						config.EnableGitHub = true
					}
				}
			}
		}

		// Parse local sources
		if local, ok := sources["local"].([]interface{}); ok {
			for _, loc := range local {
				if locMap, ok := loc.(map[string]interface{}); ok {
					var localPath string
					if path, ok := locMap["path"].(string); ok {
						localPath = path
					}
					if enabled, ok := locMap["enabled"].(bool); ok && enabled {
						config.LocalPaths = append(config.LocalPaths, localPath)
						config.EnableLocal = true
					}
				}
			}
		}
	}

	// Parse cache configuration
	if cache, ok := settings["kro.lsp.crd.cache"].(map[string]interface{}); ok {
		if enabled, ok := cache["enabled"].(bool); ok {
			config.CacheConfig.Enabled = enabled
		}
		if directory, ok := cache["directory"].(string); ok {
			config.CacheConfig.Directory = directory
		}
		if ttl, ok := cache["ttl"].(float64); ok {
			config.CacheConfig.TTL = int(ttl)
		}
		if maxSize, ok := cache["maxSize"].(string); ok {
			config.CacheConfig.MaxSize = maxSize
		}
	}

	// Parse refresh configuration
	if refresh, ok := settings["kro.lsp.crd.refresh"].(map[string]interface{}); ok {
		if interval, ok := refresh["interval"].(float64); ok {
			config.RefreshInterval = time.Duration(interval) * time.Second
		}
	}

	// Parse validation configuration
	if validation, ok := settings["kro.lsp.validation"].(map[string]interface{}); ok {
		if strictMode, ok := validation["strictMode"].(bool); ok {
			if strictMode {
				config.ValidationMode = ValidationModeStrict
			} else {
				config.ValidationMode = ValidationModePermissive
			}
		}
		if enabled, ok := validation["enabled"].(bool); ok && !enabled {
			config.ValidationMode = ValidationModeOff
		}
	}

	return config, nil
}

// initializeSources initializes CRD sources based on configuration
func (m *CRDManager) initializeSources() {
	if m.config.EnableLocal && len(m.config.LocalPaths) > 0 {
		localSource := NewLocalCRDSource(m.logger, m.config.LocalPaths)
		m.sources = append(m.sources, localSource)
		m.logger.Infof("Added local CRD source with %d paths", len(m.config.LocalPaths))
	}

	if m.config.EnableCluster && len(m.config.ClusterConfigs) > 0 {
		for _, clusterConfig := range m.config.ClusterConfigs {
			if clusterConfig.Enabled {
				clusterSource := NewClusterCRDSource(m.logger, clusterConfig.Kubeconfig, clusterConfig.Context, clusterConfig.Namespaces)
				m.sources = append(m.sources, clusterSource)
				m.logger.Infof("Added cluster CRD source: %s", clusterConfig.Name)
			}
		}
	}

	if m.config.EnableGitHub && len(m.config.GitHubRepos) > 0 {
		for _, repo := range m.config.GitHubRepos {
			if repo.Enabled {
				githubSource := NewGitHubCRDSource(m.logger, repo)
				m.sources = append(m.sources, githubSource)
				m.logger.Infof("Added GitHub CRD source: %s/%s", repo.Owner, repo.Repo)
			}
		}
	}

	m.logger.Infof("Initialized %d CRD sources", len(m.sources))
}

// LoadCRDs loads CRDs from all configured sources
func (m *CRDManager) LoadCRDs(ctx context.Context) error {
	m.logger.Info("Loading CRDs from all sources")

	var allSchemas []*CRDSchema

	// Load from all sources
	for _, source := range m.sources {
		schemas, err := source.LoadCRDs(ctx)
		if err != nil {
			m.logger.Errorf("Failed to load CRDs from source %s: %v", source.Name(), err)
			continue
		}

		m.logger.Infof("Loaded %d CRDs from source %s", len(schemas), source.Name())
		allSchemas = append(allSchemas, schemas...)
	}

	// Update cache
	m.updateCache(allSchemas)

	m.logger.Infof("Total CRDs loaded: %d", len(allSchemas))
	return nil
}

// updateCache updates the internal cache with new CRD schemas
func (m *CRDManager) updateCache(schemas []*CRDSchema) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	// Clear existing cache
	m.crdCache = make(map[string]*CRDSchema)
	m.gvkIndex = make(map[schema.GroupVersionKind]*CRDSchema)

	// Populate cache
	for _, schema := range schemas {
		// Use source as key for cache
		key := fmt.Sprintf("%s/%s", schema.Source, schema.CRD.Name)
		m.crdCache[key] = schema

		// Index by GVK for fast lookup
		m.gvkIndex[schema.GVK] = schema
	}

	m.logger.Debugf("Updated cache with %d CRD schemas", len(schemas))
}

// GetCRDByGVK retrieves a CRD schema by GroupVersionKind
func (m *CRDManager) GetCRDByGVK(gvk schema.GroupVersionKind) (*CRDSchema, bool) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	schema, exists := m.gvkIndex[gvk]
	return schema, exists
}

// GetAllCRDs returns all cached CRD schemas
func (m *CRDManager) GetAllCRDs() []*CRDSchema {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	var schemas []*CRDSchema
	for _, schema := range m.crdCache {
		schemas = append(schemas, schema)
	}

	return schemas
}

// ValidateAgainstCRD validates a document against a CRD schema
func (m *CRDManager) ValidateAgainstCRD(gvk schema.GroupVersionKind, document map[string]interface{}) ([]protocol.Diagnostic, error) {
	if m.config.ValidationMode == ValidationModeOff {
		return nil, nil
	}

	schema, exists := m.GetCRDByGVK(gvk)
	if !exists {
		m.logger.Debugf("No CRD schema found for GVK: %s", gvk.String())
		return nil, nil
	}

	var diagnostics []protocol.Diagnostic

	// Validate against OpenAPI schema
	if schema.OpenAPISchema != nil {
		schemaDiagnostics, err := m.validateOpenAPISchema(document, schema.OpenAPISchema)
		if err != nil {
			return nil, fmt.Errorf("OpenAPI schema validation failed: %w", err)
		}
		diagnostics = append(diagnostics, schemaDiagnostics...)
	}

	// Validate against CEL rules
	if len(schema.CELRules) > 0 {
		celDiagnostics, err := m.validateCELRules(document, schema.CELRules)
		if err != nil {
			return nil, fmt.Errorf("CEL validation failed: %w", err)
		}
		diagnostics = append(diagnostics, celDiagnostics...)
	}

	return diagnostics, nil
}

// validateOpenAPISchema validates a document against an OpenAPI schema
func (m *CRDManager) validateOpenAPISchema(document map[string]interface{}, schema map[string]interface{}) ([]protocol.Diagnostic, error) {
	// TODO: Implement OpenAPI schema validation
	// This would use a JSON schema validation library to validate the document
	return nil, nil
}

// validateCELRules validates a document against CEL rules
func (m *CRDManager) validateCELRules(document map[string]interface{}, rules []CELRule) ([]protocol.Diagnostic, error) {
	// TODO: Implement CEL rule validation
	// This would use the CEL library to evaluate validation rules
	return nil, nil
}

// startBackgroundRefresh starts a background goroutine to periodically refresh CRDs
func (m *CRDManager) startBackgroundRefresh() {
	m.refreshTicker = time.NewTicker(m.config.RefreshInterval)

	go func() {
		for {
			select {
			case <-m.refreshTicker.C:
				m.logger.Debug("Background CRD refresh triggered")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if err := m.RefreshCRDs(ctx); err != nil {
					m.logger.Errorf("Background CRD refresh failed: %v", err)
				}
				cancel()
			case <-m.stopChan:
				m.refreshTicker.Stop()
				return
			}
		}
	}()

	m.logger.Infof("Started background CRD refresh with interval: %v", m.config.RefreshInterval)
}

// Stop stops the CRD manager and cleans up resources
func (m *CRDManager) Stop() {
	if m.refreshTicker != nil {
		close(m.stopChan)
		m.refreshTicker.Stop()
	}
	m.logger.Info("CRD manager stopped")
}

// RefreshCRDs refreshes CRDs from all sources
func (m *CRDManager) RefreshCRDs(ctx context.Context) error {
	return m.LoadCRDs(ctx)
}
