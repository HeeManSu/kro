package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tliron/commonlog"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type CRDSource interface {
	Name() string
	LoadCRDs(ctx context.Context) ([]*CRDSchema, error)
}

type GitHubCRDSource struct {
	logger commonlog.Logger
	config GitHubConfig
	client *http.Client
}

type GitHubConfig struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Token  string `json:"token"`
}

func NewGitHubCRDSource(logger commonlog.Logger, config GitHubConfig) *GitHubCRDSource {
	if config.Branch == "" {
		config.Branch = "main"
	}

	return &GitHubCRDSource{
		logger: logger,
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *GitHubCRDSource) Name() string {
	return fmt.Sprintf("github:%s/%s", s.config.Owner, s.config.Repo)
}

func (s *GitHubCRDSource) LoadCRDs(ctx context.Context) ([]*CRDSchema, error) {
	var schemas []*CRDSchema

	files, err := s.listDirectoryFiles(ctx, s.config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory files: %w", err)
	}

	for _, file := range files {
		if !isYAMLFile(file.Name) {
			continue
		}

		fileSchemas, err := s.loadCRDsFromGitHub(ctx, file.DownloadURL, file.Name)
		if err != nil {
			continue
		}

		schemas = append(schemas, fileSchemas...)
	}

	return schemas, nil
}

// file in a GitHub repository
type GitHubFile struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
}

func (s *GitHubCRDSource) listDirectoryFiles(ctx context.Context, path string) ([]GitHubFile, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		s.config.Owner, s.config.Repo, path, s.config.Branch)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Add authentication if token is provided
	if s.config.Token != "" {
		req.Header.Add("Authorization", "token "+s.config.Token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %s", resp.Status)
	}

	var files []GitHubFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}

	return files, nil
}

func (s *GitHubCRDSource) loadCRDsFromGitHub(ctx context.Context, downloadURL, fileName string) ([]*CRDSchema, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}

	// Add authentication if token is provided
	if s.config.Token != "" {
		req.Header.Add("Authorization", "token "+s.config.Token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse multi-document YAML files (separated by ---)
	var schemas []*CRDSchema
	documents := s.splitYAMLDocuments(string(data))

	for _, doc := range documents {
		if strings.TrimSpace(doc) == "" {
			continue
		}

		var crd v1.CustomResourceDefinition
		if err := yaml.Unmarshal([]byte(doc), &crd); err != nil {
			continue
		}

		if crd.Kind != "CustomResourceDefinition" {
			continue
		}

		for _, version := range crd.Spec.Versions {
			if !version.Served {
				continue // Skip non-served versions
			}

			schema := &CRDSchema{
				CRD: &crd,
				GVK: schema.GroupVersionKind{
					Group:   crd.Spec.Group,
					Version: version.Name, // Use the specific version, not getLatestVersion()
					Kind:    crd.Spec.Names.Kind,
				},
				LastUpdate: time.Now(),
			}

			// Extract validation info from this specific version
			if version.Schema != nil && version.Schema.OpenAPIV3Schema != nil {
				schema.Schema = version.Schema.OpenAPIV3Schema
				schema.CELRules = s.extractCELRules(version.Schema.OpenAPIV3Schema, "")
			}

			schemas = append(schemas, schema)
		}
	}

	if len(schemas) == 0 {
		return nil, fmt.Errorf("no valid CRDs found in file %s", fileName)
	}

	return schemas, nil
}

func (s *GitHubCRDSource) extractCELRules(schema *v1.JSONSchemaProps, path string) []CELValidationRule {
	var rules []CELValidationRule

	if schema == nil {
		return rules
	}

	if schema.XValidations != nil {
		for _, validation := range schema.XValidations {
			rule := CELValidationRule{
				Rule:      validation.Rule,
				Message:   validation.Message,
				FieldPath: path,
			}
			if validation.MessageExpression != "" {
				rule.MessagePath = validation.MessageExpression
			}
			rules = append(rules, rule)
		}
	}

	if schema.Properties != nil {
		for propName, propSchema := range schema.Properties {
			propPath := path
			if propPath != "" {
				propPath += "."
			}
			propPath += propName
			rules = append(rules, s.extractCELRules(&propSchema, propPath)...)
		}
	}

	if schema.Items != nil && schema.Items.Schema != nil {
		itemPath := path + "[]"
		rules = append(rules, s.extractCELRules(schema.Items.Schema, itemPath)...)
	}

	return rules
}

// splitYAMLDocuments splits a multi-document YAML string into individual documents // istio crd is a multi-document YAML file
func (s *GitHubCRDSource) splitYAMLDocuments(yamlContent string) []string {
	// Split by YAML document separator (---)
	documents := strings.Split(yamlContent, "\n---\n")

	var result []string
	for _, doc := range documents {
		// Also handle cases where --- might be at the start or end of line
		doc = strings.TrimSpace(doc)
		if strings.HasPrefix(doc, "---") {
			doc = strings.TrimPrefix(doc, "---")
		}
		if strings.HasSuffix(doc, "---") {
			doc = strings.TrimSuffix(doc, "---")
		}
		doc = strings.TrimSpace(doc)

		if doc != "" {
			result = append(result, doc)
		}
	}

	return result
}
