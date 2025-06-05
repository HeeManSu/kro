package validation

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kro-run/kro/tools/lsp/server/parser"
)

// - Main validation engine for RGD files
// - Parses YAML content into structured RGD representation
// - Validates structural requirements and schema consistency
// - Integrates with CRD manager for schema validation

// RGDValidator provides comprehensive validation for ResourceGraphDefinition files
type RGDValidator struct {
	logger     commonlog.Logger
	crdManager *CRDManager

	// Cache for parsed RGD structures
	rgdCache   map[string]*ParsedRGD
	cacheMutex sync.RWMutex
}

// ParsedRGD represents a parsed and validated RGD structure
type ParsedRGD struct {
	APIVersion string
	Kind       string
	Schema     *RGDSchema
	Resources  []*RGDResource
	Errors     []ValidationError
}

// RGDSchema represents the schema definition within an RGD
type RGDSchema struct {
	Kind       string
	APIVersion string
	Group      string
	Spec       map[string]interface{}
	Status     map[string]interface{}
	Types      map[string]interface{}
}

// RGDResource represents a resource definition within an RGD
type RGDResource struct {
	ID          string
	Template    map[string]interface{}
	ExternalRef *ExternalRef
	ReadyWhen   []string
	IncludeWhen []string

	// Resolved GVK from template
	GVK *schema.GroupVersionKind
}

// ExternalRef represents an external resource reference
type ExternalRef struct {
	APIVersion string
	Kind       string
	Metadata   map[string]interface{}
}

// ValidationError represents a validation error with position information
type ValidationError struct {
	Message  string
	Line     int
	Column   int
	Severity ValidationSeverity
	Rule     string
}

// ValidationSeverity represents the severity of a validation error
type ValidationSeverity int

const (
	SeverityError ValidationSeverity = iota
	SeverityWarning
	SeverityInfo
)

// NewRGDValidator creates a new RGD validator
func NewRGDValidator(logger commonlog.Logger, crdManager *CRDManager) *RGDValidator {
	return &RGDValidator{
		logger:     logger,
		crdManager: crdManager,
		rgdCache:   make(map[string]*ParsedRGD),
	}
}

// ValidateRGD validates a ResourceGraphDefinition document against CRDs
func (v *RGDValidator) ValidateRGD(ctx context.Context, model *parser.DocumentModel, content string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Parse the RGD structure
	parsedRGD, err := v.parseRGD(content, model)
	if err != nil {
		// Create diagnostic for parsing error
		diagnostic := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 10},
			},
			Severity: func() *protocol.DiagnosticSeverity {
				severity := protocol.DiagnosticSeverityError
				return &severity
			}(),
			Message: fmt.Sprintf("Failed to parse RGD: %v", err),
			Source:  stringPtr("kro-lsp"),
		}
		diagnostics = append(diagnostics, diagnostic)
		return diagnostics
	}

	// Validate RGD structure
	structuralErrors := v.validateRGDStructure(parsedRGD)
	for _, err := range structuralErrors {
		diagnostics = append(diagnostics, v.createDiagnostic(err))
	}

	// Validate resources against CRDs
	crdErrors := v.validateResourcesAgainstCRDs(ctx, parsedRGD)
	for _, err := range crdErrors {
		diagnostics = append(diagnostics, v.createDiagnostic(err))
	}

	// Validate schema consistency
	schemaErrors := v.validateSchemaConsistency(parsedRGD)
	for _, err := range schemaErrors {
		diagnostics = append(diagnostics, v.createDiagnostic(err))
	}

	return diagnostics
}

// parseRGD parses the YAML content into a structured RGD representation
func (v *RGDValidator) parseRGD(content string, model *parser.DocumentModel) (*ParsedRGD, error) {
	var rawRGD map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &rawRGD); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	rgd := &ParsedRGD{}

	// Extract basic fields
	if apiVersion, ok := rawRGD["apiVersion"].(string); ok {
		rgd.APIVersion = apiVersion
	}
	if kind, ok := rawRGD["kind"].(string); ok {
		rgd.Kind = kind
	}

	// Validate this is actually an RGD
	if rgd.APIVersion != "kro.run/v1alpha1" || rgd.Kind != "ResourceGraphDefinition" {
		return nil, fmt.Errorf("not a ResourceGraphDefinition: apiVersion=%s, kind=%s", rgd.APIVersion, rgd.Kind)
	}

	// Parse spec section
	if spec, ok := rawRGD["spec"].(map[string]interface{}); ok {
		if err := v.parseRGDSpec(rgd, spec); err != nil {
			return nil, fmt.Errorf("failed to parse spec: %w", err)
		}
	}

	return rgd, nil
}

// parseRGDSpec parses the spec section of an RGD
func (v *RGDValidator) parseRGDSpec(rgd *ParsedRGD, spec map[string]interface{}) error {
	// Parse schema
	if schemaData, ok := spec["schema"].(map[string]interface{}); ok {
		schema := &RGDSchema{}

		if kind, ok := schemaData["kind"].(string); ok {
			schema.Kind = kind
		}
		if apiVersion, ok := schemaData["apiVersion"].(string); ok {
			schema.APIVersion = apiVersion
		}
		if group, ok := schemaData["group"].(string); ok {
			schema.Group = group
		}
		if specData, ok := schemaData["spec"].(map[string]interface{}); ok {
			schema.Spec = specData
		}
		if statusData, ok := schemaData["status"].(map[string]interface{}); ok {
			schema.Status = statusData
		}
		if typesData, ok := schemaData["types"].(map[string]interface{}); ok {
			schema.Types = typesData
		}

		rgd.Schema = schema
	}

	// Parse resources
	if resourcesData, ok := spec["resources"].([]interface{}); ok {
		for _, resourceItem := range resourcesData {
			if resourceMap, ok := resourceItem.(map[string]interface{}); ok {
				resource, err := v.parseRGDResource(resourceMap)
				if err != nil {
					return fmt.Errorf("failed to parse resource: %w", err)
				}
				rgd.Resources = append(rgd.Resources, resource)
			}
		}
	}

	return nil
}

// parseRGDResource parses a single resource definition
func (v *RGDValidator) parseRGDResource(resourceMap map[string]interface{}) (*RGDResource, error) {
	resource := &RGDResource{}

	if id, ok := resourceMap["id"].(string); ok {
		resource.ID = id
	}

	// Parse template
	if template, ok := resourceMap["template"].(map[string]interface{}); ok {
		resource.Template = template

		// Extract GVK from template
		if apiVersion, hasAPIVersion := template["apiVersion"].(string); hasAPIVersion {
			if kind, hasKind := template["kind"].(string); hasKind {
				gvk, err := parseGVK(apiVersion, kind)
				if err == nil {
					resource.GVK = &gvk
				}
			}
		}
	}

	// Parse external ref
	if externalRefData, ok := resourceMap["externalRef"].(map[string]interface{}); ok {
		extRef := &ExternalRef{}
		if apiVersion, ok := externalRefData["apiVersion"].(string); ok {
			extRef.APIVersion = apiVersion
		}
		if kind, ok := externalRefData["kind"].(string); ok {
			extRef.Kind = kind
		}
		if metadata, ok := externalRefData["metadata"].(map[string]interface{}); ok {
			extRef.Metadata = metadata
		}
		resource.ExternalRef = extRef
	}

	// Parse readyWhen and includeWhen
	if readyWhenData, ok := resourceMap["readyWhen"].([]interface{}); ok {
		for _, item := range readyWhenData {
			if str, ok := item.(string); ok {
				resource.ReadyWhen = append(resource.ReadyWhen, str)
			}
		}
	}

	if includeWhenData, ok := resourceMap["includeWhen"].([]interface{}); ok {
		for _, item := range includeWhenData {
			if str, ok := item.(string); ok {
				resource.IncludeWhen = append(resource.IncludeWhen, str)
			}
		}
	}

	return resource, nil
}

// validateRGDStructure validates the basic structure of an RGD
func (v *RGDValidator) validateRGDStructure(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	// Validate required fields
	if rgd.APIVersion == "" {
		errors = append(errors, ValidationError{
			Message:  "apiVersion is required",
			Severity: SeverityError,
			Rule:     "required-field",
		})
	}

	if rgd.Kind == "" {
		errors = append(errors, ValidationError{
			Message:  "kind is required",
			Severity: SeverityError,
			Rule:     "required-field",
		})
	}

	// Validate schema if present
	if rgd.Schema != nil {
		if rgd.Schema.Kind == "" {
			errors = append(errors, ValidationError{
				Message:  "schema.kind is required",
				Severity: SeverityError,
				Rule:     "required-field",
			})
		}
		if rgd.Schema.APIVersion == "" {
			errors = append(errors, ValidationError{
				Message:  "schema.apiVersion is required",
				Severity: SeverityError,
				Rule:     "required-field",
			})
		}
	}

	// Validate resources
	for i, resource := range rgd.Resources {
		if resource.ID == "" {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d].id is required", i),
				Severity: SeverityError,
				Rule:     "required-field",
			})
		}

		// Must have either template or externalRef
		if resource.Template == nil && resource.ExternalRef == nil {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d] must have either template or externalRef", i),
				Severity: SeverityError,
				Rule:     "mutually-exclusive",
			})
		}

		if resource.Template != nil && resource.ExternalRef != nil {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d] cannot have both template and externalRef", i),
				Severity: SeverityError,
				Rule:     "mutually-exclusive",
			})
		}
	}

	return errors
}

// validateResourcesAgainstCRDs validates each resource template against its corresponding CRD
func (v *RGDValidator) validateResourcesAgainstCRDs(ctx context.Context, rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	if v.crdManager == nil {
		return errors // Skip CRD validation if no CRD manager
	}

	for i, resource := range rgd.Resources {
		if resource.GVK == nil {
			continue // Skip resources without GVK
		}

		// Get CRD for this resource
		crdSchema, found := v.crdManager.GetCRDByGVK(*resource.GVK)
		if !found {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d]: No CRD found for %s", i, resource.GVK.String()),
				Severity: SeverityWarning,
				Rule:     "crd-not-found",
			})
			continue
		}

		v.logger.Debugf("Validating resource %s against CRD: %s", resource.ID, crdSchema.GVK.String())

		// Validate resource template against CRD schema
		diagnostics, err := v.crdManager.ValidateAgainstCRD(*resource.GVK, resource.Template)
		if err != nil {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d]: Validation error: %v", i, err),
				Severity: SeverityError,
				Rule:     "crd-validation-error",
			})
		}

		// Convert protocol diagnostics to validation errors
		for _, diag := range diagnostics {
			severity := SeverityInfo
			if diag.Severity != nil {
				switch *diag.Severity {
				case protocol.DiagnosticSeverityError:
					severity = SeverityError
				case protocol.DiagnosticSeverityWarning:
					severity = SeverityWarning
				case protocol.DiagnosticSeverityInformation:
					severity = SeverityInfo
				}
			}

			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("resource[%d] (%s): %s", i, resource.ID, diag.Message),
				Severity: severity,
				Rule:     "crd-validation",
				Line:     int(diag.Range.Start.Line),
				Column:   int(diag.Range.Start.Character),
			})
		}
	}

	return errors
}

// validateSchemaConsistency validates that the RGD schema is consistent
func (v *RGDValidator) validateSchemaConsistency(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	if rgd.Schema == nil {
		return errors
	}

	// Validate schema format
	if rgd.Schema.APIVersion != "" {
		if !strings.HasPrefix(rgd.Schema.APIVersion, "v") {
			errors = append(errors, ValidationError{
				Message:  "schema.apiVersion must start with 'v' (e.g., 'v1', 'v1alpha1')",
				Severity: SeverityError,
				Rule:     "schema-format",
			})
		}
	}

	// Validate kind naming convention
	if rgd.Schema.Kind != "" {
		if !isValidKubernetesName(rgd.Schema.Kind) {
			errors = append(errors, ValidationError{
				Message:  "schema.kind must follow Kubernetes naming conventions (PascalCase)",
				Severity: SeverityError,
				Rule:     "naming-convention",
			})
		}
	}

	return errors
}

// createDiagnostic converts a ValidationError to a protocol.Diagnostic
func (v *RGDValidator) createDiagnostic(err ValidationError) protocol.Diagnostic {
	var severity protocol.DiagnosticSeverity
	switch err.Severity {
	case SeverityError:
		severity = protocol.DiagnosticSeverityError
	case SeverityWarning:
		severity = protocol.DiagnosticSeverityWarning
	case SeverityInfo:
		severity = protocol.DiagnosticSeverityInformation
	default:
		severity = protocol.DiagnosticSeverityError
	}

	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{
				Line:      uint32(err.Line),
				Character: uint32(err.Column),
			},
			End: protocol.Position{
				Line:      uint32(err.Line),
				Character: uint32(err.Column + 10), // Approximate end
			},
		},
		Severity: &severity,
		Message:  err.Message,
		Source:   stringPtr("kro-lsp"),
	}
}

// Helper functions

// parseGVK parses a GroupVersionKind from apiVersion and kind
func parseGVK(apiVersion, kind string) (schema.GroupVersionKind, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}, nil
}

// isValidKubernetesName checks if a name follows Kubernetes naming conventions
func isValidKubernetesName(name string) bool {
	if len(name) == 0 {
		return false
	}

	// Must start with uppercase letter
	if name[0] < 'A' || name[0] > 'Z' {
		return false
	}

	// Only alphanumeric characters allowed
	for _, r := range name {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}

	return true
}

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}
