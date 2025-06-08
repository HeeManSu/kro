package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tliron/commonlog"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// - **Local**: Filesystem paths (`./crds`, `~/.kube/crds`)
// - **Cluster**: Kubernetes API (requires kubeconfig)
// - **GitHub**: Public repositories with CRD files

// LocalCRDSource loads CRDs from local filesystem
type LocalCRDSource struct {
	logger commonlog.Logger
	paths  []string
}

// NewLocalCRDSource creates a new local CRD source
func NewLocalCRDSource(logger commonlog.Logger, paths []string) *LocalCRDSource {
	return &LocalCRDSource{
		logger: logger,
		paths:  paths,
	}
}

// Name returns the name of this source
func (s *LocalCRDSource) Name() string {
	return "local"
}

// LoadCRDs loads CRDs from local filesystem paths
func (s *LocalCRDSource) LoadCRDs(ctx context.Context) ([]*CRDSchema, error) {
	var schemas []*CRDSchema

	for _, path := range s.paths {
		// Expand home directory
		expandedPath := strings.Replace(path, "~", os.Getenv("HOME"), 1)

		err := filepath.WalkDir(expandedPath, func(filePath string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip non-YAML files
			if !isYAMLFile(filePath) {
				return nil
			}

			// Read and parse file
			data, err := os.ReadFile(filePath)
			if err != nil {
				s.logger.Warningf("Failed to read file %s: %v", filePath, err)
				return nil
			}

			// Parse as CRD
			crd, err := s.parseCRD(data)
			if err != nil {
				s.logger.Debugf("File %s is not a valid CRD: %v", filePath, err)
				return nil
			}

			// Create schema
			schema := &CRDSchema{
				CRD: crd,
				GVK: schema.GroupVersionKind{
					Group:   crd.Spec.Group,
					Version: getLatestVersion(crd),
					Kind:    crd.Spec.Names.Kind,
				},
				Source:     fmt.Sprintf("local:%s", filePath),
				LastUpdate: time.Now(),
			}

			// Extract OpenAPI schema and CEL rules if available
			s.extractValidationInfo(crd, schema)

			schemas = append(schemas, schema)
			s.logger.Debugf("Loaded CRD %s from %s", crd.Name, filePath)

			return nil
		})

		if err != nil {
			s.logger.Warningf("Failed to walk directory %s: %v", expandedPath, err)
		}
	}

	return schemas, nil
}

// extractValidationInfo extracts OpenAPI schema and CEL rules from CRD
func (s *LocalCRDSource) extractValidationInfo(crd *v1.CustomResourceDefinition, schema *CRDSchema) {
	if crd.Spec.Versions != nil && len(crd.Spec.Versions) > 0 {
		for _, version := range crd.Spec.Versions {
			if version.Schema != nil && version.Schema.OpenAPIV3Schema != nil {
				schema.OpenAPISchema = convertOpenAPISchema(version.Schema.OpenAPIV3Schema)
				schema.CELRules = extractCELRules(version.Schema.OpenAPIV3Schema, "")
				break
			}
		}
	}
}

// SupportsWatch returns whether this source supports watching for changes
func (s *LocalCRDSource) SupportsWatch() bool {
	return false // TODO: Implement file system watching
}

// Watch watches for changes in local CRDs
func (s *LocalCRDSource) Watch(ctx context.Context, callback func([]*CRDSchema)) error {
	return fmt.Errorf("watch not implemented for local source")
}

// parseCRD parses YAML data into a CRD
func (s *LocalCRDSource) parseCRD(data []byte) (*v1.CustomResourceDefinition, error) {
	var crd v1.CustomResourceDefinition

	err := yaml.Unmarshal(data, &crd)
	if err != nil {
		return nil, err
	}

	// Validate that this is actually a CRD
	if crd.Kind != "CustomResourceDefinition" {
		return nil, fmt.Errorf("not a CustomResourceDefinition, got %s", crd.Kind)
	}

	return &crd, nil
}

// ClusterCRDSource loads CRDs from a Kubernetes cluster
type ClusterCRDSource struct {
	logger     commonlog.Logger
	kubeconfig string
	context    string
	namespaces []string
	// TODO: Add proper Kubernetes client for CRD access
}

// NewClusterCRDSource creates a new cluster CRD source
func NewClusterCRDSource(logger commonlog.Logger, kubeconfig, context string, namespaces []string) *ClusterCRDSource {
	return &ClusterCRDSource{
		logger:     logger,
		kubeconfig: kubeconfig,
		context:    context,
		namespaces: namespaces,
	}
}

// Name returns the name of this source
func (s *ClusterCRDSource) Name() string {
	if s.context != "" {
		return fmt.Sprintf("cluster:%s", s.context)
	}
	return "cluster:default"
}

// LoadCRDs loads CRDs from Kubernetes cluster
func (s *ClusterCRDSource) LoadCRDs(ctx context.Context) ([]*CRDSchema, error) {
	// TODO: Implement cluster CRD loading when apiextensions client is available
	s.logger.Debug("Cluster CRD loading requires apiextensions client - returning empty for now")
	return []*CRDSchema{}, nil
}

// SupportsWatch returns whether this source supports watching for changes
func (s *ClusterCRDSource) SupportsWatch() bool {
	return true // Kubernetes supports watching
}

// Watch watches for changes in cluster CRDs
func (s *ClusterCRDSource) Watch(ctx context.Context, callback func([]*CRDSchema)) error {
	// TODO: Implement cluster CRD watching when client is available
	return fmt.Errorf("cluster watch not implemented - requires apiextensions client")
}

// GitHubCRDSource loads CRDs from GitHub repositories
type GitHubCRDSource struct {
	logger commonlog.Logger
	repo   GitHubRepo
}

// NewGitHubCRDSource creates a new GitHub CRD source
func NewGitHubCRDSource(logger commonlog.Logger, repo GitHubRepo) *GitHubCRDSource {
	return &GitHubCRDSource{
		logger: logger,
		repo:   repo,
	}
}

// Name returns the name of this source
func (s *GitHubCRDSource) Name() string {
	return fmt.Sprintf("github:%s/%s", s.repo.Owner, s.repo.Repo)
}

// LoadCRDs loads CRDs from GitHub repository
func (s *GitHubCRDSource) LoadCRDs(ctx context.Context) ([]*CRDSchema, error) {
	// GitHub API URL for repository contents
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		s.repo.Owner, s.repo.Repo, s.repo.Path, s.repo.Branch)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization header if token is available
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	// Parse response
	var files []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		DownloadURL string `json:"download_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	// Download and parse YAML files
	var schemas []*CRDSchema
	for _, file := range files {
		if file.Type == "file" && isYAMLFile(file.Name) {
			schema, err := s.downloadAndParseCRD(ctx, file.DownloadURL, file.Name)
			if err != nil {
				s.logger.Warningf("Failed to parse CRD from %s: %v", file.Name, err)
				continue
			}
			if schema != nil {
				schemas = append(schemas, schema)
				s.logger.Debugf("Loaded CRD %s from GitHub", schema.CRD.Name)
			}
		}
	}

	s.logger.Infof("Loaded %d CRDs from GitHub %s", len(schemas), s.Name())
	return schemas, nil
}

// downloadAndParseCRD downloads and parses a CRD from GitHub
func (s *GitHubCRDSource) downloadAndParseCRD(ctx context.Context, url, filename string) (*CRDSchema, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var crd v1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return nil, err
	}

	// Validate that this is actually a CRD
	if crd.Kind != "CustomResourceDefinition" {
		return nil, nil // Not a CRD, skip
	}

	// Create schema
	schema := &CRDSchema{
		CRD: &crd,
		GVK: schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: getLatestVersion(&crd),
			Kind:    crd.Spec.Names.Kind,
		},
		Source:     fmt.Sprintf("github:%s/%s/%s", s.repo.Owner, s.repo.Repo, filename),
		LastUpdate: time.Now(),
	}

	// Extract OpenAPI schema and CEL rules
	s.extractValidationInfo(&crd, schema)

	return schema, nil
}

// extractValidationInfo extracts OpenAPI schema and CEL rules from CRD
func (s *GitHubCRDSource) extractValidationInfo(crd *v1.CustomResourceDefinition, schema *CRDSchema) {
	if crd.Spec.Versions != nil && len(crd.Spec.Versions) > 0 {
		for _, version := range crd.Spec.Versions {
			if version.Schema != nil && version.Schema.OpenAPIV3Schema != nil {
				schema.OpenAPISchema = convertOpenAPISchema(version.Schema.OpenAPIV3Schema)
				schema.CELRules = extractCELRules(version.Schema.OpenAPIV3Schema, "")
				break
			}
		}
	}
}

// SupportsWatch returns whether this source supports watching for changes
func (s *GitHubCRDSource) SupportsWatch() bool {
	return false // GitHub doesn't support real-time watching
}

// Watch watches for changes in GitHub CRDs
func (s *GitHubCRDSource) Watch(ctx context.Context, callback func([]*CRDSchema)) error {
	return fmt.Errorf("GitHub watch not supported")
}

// Utility functions

func isYAMLFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".yaml" || ext == ".yml"
}

func getLatestVersion(crd *v1.CustomResourceDefinition) string {
	if crd.Spec.Versions != nil && len(crd.Spec.Versions) > 0 {
		// Find the served version or return the first one
		for _, version := range crd.Spec.Versions {
			if version.Served {
				return version.Name
			}
		}
		return crd.Spec.Versions[0].Name
	}
	return ""
}

func convertOpenAPISchema(schema *v1.JSONSchemaProps) map[string]interface{} {
	// Convert the OpenAPI schema to a map for easier manipulation
	// This is a simplified conversion - in production, you'd want more thorough conversion
	result := make(map[string]interface{})

	if schema.Type != "" {
		result["type"] = schema.Type
	}
	if schema.Format != "" {
		result["format"] = schema.Format
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if schema.Properties != nil {
		properties := make(map[string]interface{})
		for key, prop := range schema.Properties {
			properties[key] = convertOpenAPISchema(&prop)
		}
		result["properties"] = properties
	}
	if schema.Required != nil {
		result["required"] = schema.Required
	}
	if schema.Items != nil {
		// Handle JSONSchemaPropsOrArray - check if it has a JSONSchemaProps
		if schema.Items.Schema != nil {
			result["items"] = convertOpenAPISchema(schema.Items.Schema)
		}
	}

	return result
}

// extractCELRules recursively extracts CEL validation rules from OpenAPI schema
func extractCELRules(schema *v1.JSONSchemaProps, path string) []CELRule {
	var rules []CELRule

	// Check for CEL validation rules at this level
	if schema.XValidations != nil {
		for _, validation := range schema.XValidations {
			rule := CELRule{
				Rule:    validation.Rule,
				Message: validation.Message,
				Path:    path,
			}
			rules = append(rules, rule)
		}
	}

	// Recursively check properties
	if schema.Properties != nil {
		for propName, propSchema := range schema.Properties {
			propPath := path
			if propPath != "" {
				propPath += "."
			}
			propPath += propName

			propRules := extractCELRules(&propSchema, propPath)
			rules = append(rules, propRules...)
		}
	}

	// Check items for arrays - handle JSONSchemaPropsOrArray
	if schema.Items != nil && schema.Items.Schema != nil {
		itemPath := path + "[]"
		itemRules := extractCELRules(schema.Items.Schema, itemPath)
		rules = append(rules, itemRules...)
	}

	return rules
}
