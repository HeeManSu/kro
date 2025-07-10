package validation

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/tliron/commonlog"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type CRDManager struct {
	logger        commonlog.Logger
	sources       []CRDSource
	cache         map[string]*CRDSchema // GVK -> Schema
	lastRefresh   time.Time
	refreshPeriod time.Duration
	mu            sync.RWMutex
	enabled       bool
	autoRefresh   bool
}

// CRD manager configuration
type CRDConfig struct {
	Enabled     bool           `json:"enabled"`
	AutoRefresh bool           `json:"autoRefresh"`
	GitHubRepos []GitHubConfig `json:"githubRepos"`
}

// parsed CRD with validation info
type CRDSchema struct {
	CRD        *v1.CustomResourceDefinition
	GVK        schema.GroupVersionKind
	Schema     *v1.JSONSchemaProps
	CELRules   []CELValidationRule
	LastUpdate time.Time
}

// CEL validation rule
type CELValidationRule struct {
	Rule        string
	Message     string
	MessagePath string
	FieldPath   string
}

func NewCRDManager(logger commonlog.Logger, config CRDConfig) *CRDManager {
	manager := &CRDManager{
		logger:        logger,
		cache:         make(map[string]*CRDSchema),
		refreshPeriod: 5 * time.Minute,
		enabled:       config.Enabled,
		autoRefresh:   config.AutoRefresh,
	}

	// Initialize sources
	manager.initSources(config)

	return manager
}

func (m *CRDManager) initSources(config CRDConfig) {
	m.sources = []CRDSource{}

	// Add GitHub sources
	for _, githubConfig := range config.GitHubRepos {
		if githubConfig.Owner != "" && githubConfig.Repo != "" {
			m.sources = append(m.sources, NewGitHubCRDSource(m.logger, githubConfig))
		}
	}

	m.logger.Infof("Initialized %d GitHub CRD sources", len(m.sources))
}

// returns whether CRD validation is enabled
func (m *CRDManager) IsEnabled() bool {
	return m.enabled
}

// loads CRDs from all GitHub sources
func (m *CRDManager) LoadCRDs(ctx context.Context) error {
	if !m.enabled {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cache = make(map[string]*CRDSchema)

	totalLoaded := 0
	for _, source := range m.sources {
		schemas, err := source.LoadCRDs(ctx)
		if err != nil {
			m.logger.Warningf("Failed to load CRDs from source %s: %v", source.Name(), err)
			continue
		}

		for _, schema := range schemas {
			key := schema.GVK.String()
			m.cache[key] = schema
			totalLoaded++
		}
	}

	m.lastRefresh = time.Now()
	m.logger.Infof("Loaded %d CRDs from %d GitHub sources", len(m.cache), len(m.sources))

	return nil
}

func (m *CRDManager) GetCRDSchema(gvk schema.GroupVersionKind) *CRDSchema {
	if !m.enabled {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	lookupKey := gvk.String()

	result := m.cache[lookupKey]
	if result == nil {
		return nil
	}

	return result
}

// refreshes CRDs if auto-refresh is enabled and period has passed
// func (m *CRDManager) RefreshIfNeeded(ctx context.Context) error {
// 	if !m.enabled || !m.autoRefresh {
// 		return nil
// 	}

// 	m.mu.RLock()
// 	needsRefresh := time.Since(m.lastRefresh) > m.refreshPeriod
// 	m.mu.RUnlock()

// 	if needsRefresh {
// 		return m.LoadCRDs(ctx)
// 	}

// 	return nil
// }

// updates the CRD manager configuration without recreating the instance
func (m *CRDManager) updateConfig(config CRDConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update configuration
	m.enabled = config.Enabled
	m.autoRefresh = config.AutoRefresh

	// Reinitialize sources with new config
	m.initSources(config)

}

// returns statistics about loaded CRDs
// func (m *CRDManager) GetStats() map[string]interface{} {
// 	m.mu.RLock()
// 	defer m.mu.RUnlock()

// 	stats := map[string]interface{}{
// 		"enabled":     m.enabled,
// 		"autoRefresh": m.autoRefresh,
// 		"totalCRDs":   len(m.cache),
// 		"lastRefresh": m.lastRefresh.Format(time.RFC3339),
// 		"sources":     len(m.sources),
// 	}

// 	sourceStats := make([]string, len(m.sources))
// 	for i, source := range m.sources {
// 		sourceStats[i] = source.Name()
// 	}
// 	stats["sourceNames"] = sourceStats

// 	return stats
// }

func isYAMLFile(filename string) bool {
	name := strings.ToLower(filename)
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// func getLatestVersion(crd *v1.CustomResourceDefinition) string {
// 	if len(crd.Spec.Versions) == 0 {
// 		return "v1"
// 	}

// 	// Return the first served version, or the first version
// 	for _, version := range crd.Spec.Versions {
// 		if version.Served {
// 			return version.Name
// 		}
// 	}

// 	return crd.Spec.Versions[0].Name
// }
