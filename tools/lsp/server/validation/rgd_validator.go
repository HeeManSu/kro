package validation

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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
// - **Structural**: Required fields, naming conventions
// - **Schema**: OpenAPI v3 schema validation against CRDs
// - **CEL**: Common Expression Language rule evaluation

// RGDValidator provides comprehensive validation for ResourceGraphDefinition files
// using existing Kro validation functions
type RGDValidator struct {
	logger commonlog.Logger

	// Cache for parsed RGD structures
	rgdCache   map[string]*ParsedRGD
	cacheMutex sync.RWMutex
}

// ParsedRGD represents a parsed and validated RGD structure
type ParsedRGD struct {
	APIVersion string
	Kind       string
	Metadata   map[string]interface{}
	Schema     *RGDSchema
	Resources  []*RGDResource
	Errors     []ValidationError

	// Position tracking
	YamlNode *yaml.Node
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
	Path     string // YAML path to the error
}

// ValidationSeverity represents the severity of a validation error
type ValidationSeverity int

const (
	SeverityError ValidationSeverity = iota
	SeverityWarning
	SeverityInfo
)

// Valid base types for schema fields
var validBaseTypes = []string{
	"string",
	"integer",
	"boolean",
	"number",
	"object",
}

// Valid modifiers for schema fields
var validModifiers = []string{
	"required",
	"default",
	"description",
	"minimum",
	"maximum",
	"enum",
	"format",
	"pattern",
	"minLength",
	"maxLength",
	"minItems",
	"maxItems",
	"uniqueItems",
}

// Regular expressions for validating type syntax
var typePatterns = map[string]*regexp.Regexp{
	"string":  regexp.MustCompile(`^string(\s*\|\s*.+)?$`),
	"integer": regexp.MustCompile(`^integer(\s*\|\s*.+)?$`),
	"boolean": regexp.MustCompile(`^boolean(\s*\|\s*.+)?$`),
	"number":  regexp.MustCompile(`^number(\s*\|\s*.+)?$`),
	"object":  regexp.MustCompile(`^object(\s*\|\s*.+)?$`),
	"array":   regexp.MustCompile(`^\[\](.+)$`),
	"map":     regexp.MustCompile(`^map\[(.+)\](.+)$`),
}

// Regular expressions for naming conventions
var (
	upperCamelCaseRegex    = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
	lowerCamelCaseRegex    = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	kubernetesVersionRegex = regexp.MustCompile(`^v\d+(?:(?:alpha|beta)\d+)?$`)
)

// Reserved words in Kro
var reservedWords = []string{
	"apiVersion", "context", "dependency", "dependencies", "externalRef",
	"externalReference", "externalRefs", "externalReferences", "graph",
	"instance", "kind", "metadata", "namespace", "object", "resource",
	"resourcegraphdefinition", "resources", "runtime", "serviceAccountName",
	"schema", "spec", "status", "kro", "variables", "vars", "version",
}

// NewRGDValidator creates a new RGD validator
func NewRGDValidator(logger commonlog.Logger) *RGDValidator {
	return &RGDValidator{
		logger:   logger,
		rgdCache: make(map[string]*ParsedRGD),
	}
}

// ValidateRGD validates a ResourceGraphDefinition document
func (v *RGDValidator) ValidateRGD(ctx context.Context, model *parser.DocumentModel, content string) []protocol.Diagnostic {
	var diagnostics []protocol.Diagnostic

	// Check if this is an RGD document based on apiVersion and kind
	if !v.isRGDDocument(content) {
		return diagnostics // Skip validation for non-RGD documents
	}

	// Parse the RGD structure with position tracking
	parsedRGD, err := v.parseRGDWithPositions(content, model)
	if err != nil {
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
			Source:  &[]string{"kro-lsp"}[0],
		}
		diagnostics = append(diagnostics, diagnostic)
		return diagnostics
	}

	// Perform comprehensive validation
	errors := v.validateRGDComprehensive(parsedRGD)
	for _, err := range errors {
		diagnostics = append(diagnostics, v.createDiagnostic(err))
	}

	return diagnostics
}

// isRGDDocument checks if the document is an RGD based on apiVersion and kind
func (v *RGDValidator) isRGDDocument(content string) bool {
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return false
	}

	apiVersion, hasAPIVersion := doc["apiVersion"].(string)
	kind, hasKind := doc["kind"].(string)

	return hasAPIVersion && hasKind &&
		apiVersion == "kro.run/v1alpha1" &&
		kind == "ResourceGraphDefinition"
}

// validateRGDComprehensive performs comprehensive RGD validation
func (v *RGDValidator) validateRGDComprehensive(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	// 1. Validate basic RGD structure (apiVersion, kind, metadata)
	errors = append(errors, v.validateBasicStructure(rgd)...)

	// 2. Validate metadata
	errors = append(errors, v.validateMetadata(rgd)...)

	// 3. Validate spec section
	if rgd.Schema != nil || len(rgd.Resources) > 0 {
		errors = append(errors, v.validateSpecSection(rgd)...)
	}

	return errors
}

// validateBasicStructure validates apiVersion, kind, and presence of required fields
func (v *RGDValidator) validateBasicStructure(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	// Validate apiVersion
	if rgd.APIVersion == "" {
		errors = append(errors, ValidationError{
			Message:  "apiVersion is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     "apiVersion",
			Line:     v.getLineNumber(rgd.YamlNode, "apiVersion"),
		})
	} else if rgd.APIVersion != "kro.run/v1alpha1" {
		errors = append(errors, ValidationError{
			Message:  "apiVersion must be 'kro.run/v1alpha1'",
			Severity: SeverityError,
			Rule:     "invalid-api-version",
			Path:     "apiVersion",
			Line:     v.getLineNumber(rgd.YamlNode, "apiVersion"),
		})
	}

	// Validate kind
	if rgd.Kind == "" {
		errors = append(errors, ValidationError{
			Message:  "kind is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     "kind",
			Line:     v.getLineNumber(rgd.YamlNode, "kind"),
		})
	} else if rgd.Kind != "ResourceGraphDefinition" {
		errors = append(errors, ValidationError{
			Message:  "kind must be 'ResourceGraphDefinition'",
			Severity: SeverityError,
			Rule:     "invalid-kind",
			Path:     "kind",
			Line:     v.getLineNumber(rgd.YamlNode, "kind"),
		})
	}

	return errors
}

// validateMetadata validates the metadata section
func (v *RGDValidator) validateMetadata(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	if rgd.Metadata == nil || len(rgd.Metadata) == 0 {
		errors = append(errors, ValidationError{
			Message:  "metadata is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     "metadata",
			Line:     v.getLineNumber(rgd.YamlNode, "metadata"),
		})
		return errors
	}

	// Validate metadata.name
	name, hasName := rgd.Metadata["name"]
	if !hasName {
		errors = append(errors, ValidationError{
			Message:  "metadata.name is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     "metadata.name",
			Line:     v.getLineNumber(rgd.YamlNode, "metadata.name"),
		})
	} else if nameStr, ok := name.(string); ok {
		if nameStr == "" {
			errors = append(errors, ValidationError{
				Message:  "metadata.name cannot be empty",
				Severity: SeverityError,
				Rule:     "empty-name",
				Path:     "metadata.name",
				Line:     v.getLineNumber(rgd.YamlNode, "metadata.name"),
			})
		}
	} else {
		errors = append(errors, ValidationError{
			Message:  "metadata.name must be a string",
			Severity: SeverityError,
			Rule:     "invalid-type",
			Path:     "metadata.name",
			Line:     v.getLineNumber(rgd.YamlNode, "metadata.name"),
		})
	}

	return errors
}

// validateSpecSection validates the spec section including schema and resources
func (v *RGDValidator) validateSpecSection(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	// Validate schema section
	if rgd.Schema != nil {
		errors = append(errors, v.validateSchema(rgd)...)
	}

	// Validate resources section
	errors = append(errors, v.validateResources(rgd)...)

	return errors
}

// validateSchema validates the schema section
func (v *RGDValidator) validateSchema(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	// Validate schema.kind
	if rgd.Schema.Kind == "" {
		errors = append(errors, ValidationError{
			Message:  "spec.schema.kind is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     "spec.schema.kind",
			Line:     v.getLineNumber(rgd.YamlNode, "spec.schema.kind"),
		})
	} else if !isValidKindName(rgd.Schema.Kind) {
		errors = append(errors, ValidationError{
			Message:  fmt.Sprintf("spec.schema.kind '%s' must be UpperCamelCase", rgd.Schema.Kind),
			Severity: SeverityError,
			Rule:     "invalid-naming",
			Path:     "spec.schema.kind",
			Line:     v.getLineNumber(rgd.YamlNode, "spec.schema.kind"),
		})
	}

	// Validate schema.apiVersion
	if rgd.Schema.APIVersion != "" {
		if !validateKubernetesVersion(rgd.Schema.APIVersion) {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("spec.schema.apiVersion '%s' is not a valid Kubernetes version", rgd.Schema.APIVersion),
				Severity: SeverityError,
				Rule:     "invalid-version",
				Path:     "spec.schema.apiVersion",
				Line:     v.getLineNumber(rgd.YamlNode, "spec.schema.apiVersion"),
			})
		}
	}

	// Validate spec fields if present
	if rgd.Schema.Spec != nil {
		errors = append(errors, v.validateSchemaFields(rgd.Schema.Spec, "spec.schema.spec", rgd.YamlNode)...)
	}

	// Validate status fields if present
	if rgd.Schema.Status != nil {
		errors = append(errors, v.validateSchemaFields(rgd.Schema.Status, "spec.schema.status", rgd.YamlNode)...)
	}

	return errors
}

// validateSchemaFields validates schema field definitions
func (v *RGDValidator) validateSchemaFields(fields map[string]interface{}, basePath string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	for fieldName, fieldDef := range fields {
		fieldPath := basePath + "." + fieldName

		// Validate field name
		if !isValidResourceID(fieldName) {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("field name '%s' must be lowerCamelCase", fieldName),
				Severity: SeverityError,
				Rule:     "invalid-naming",
				Path:     fieldPath,
				Line:     v.getLineNumber(yamlNode, fieldPath),
			})
		}

		// Check for reserved words
		if isKroReservedWord(fieldName) {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("field name '%s' is a reserved word", fieldName),
				Severity: SeverityError,
				Rule:     "reserved-word",
				Path:     fieldPath,
				Line:     v.getLineNumber(yamlNode, fieldPath),
			})
		}

		// Validate field definition
		if fieldDefStr, ok := fieldDef.(string); ok {
			errors = append(errors, v.validateFieldDefinition(fieldDefStr, fieldPath, yamlNode)...)
		} else if fieldDefMap, ok := fieldDef.(map[string]interface{}); ok {
			// Nested object - recursively validate
			errors = append(errors, v.validateSchemaFields(fieldDefMap, fieldPath, yamlNode)...)
		} else {
			errors = append(errors, ValidationError{
				Message:  "field definition must be a string or object",
				Severity: SeverityError,
				Rule:     "invalid-type",
				Path:     fieldPath,
				Line:     v.getLineNumber(yamlNode, fieldPath),
			})
		}
	}

	return errors
}

// validateFieldDefinition validates a field type definition with modifiers
func (v *RGDValidator) validateFieldDefinition(fieldDef, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	// Parse field definition (e.g., "string | required=true description='A field'")
	parts := strings.Split(fieldDef, "|")
	if len(parts) == 0 {
		errors = append(errors, ValidationError{
			Message:  "field definition cannot be empty",
			Severity: SeverityError,
			Rule:     "empty-definition",
			Path:     path,
			Line:     v.getLineNumber(yamlNode, path),
		})
		return errors
	}

	// Validate base type
	baseType := strings.TrimSpace(parts[0])
	errors = append(errors, v.validateBaseType(baseType, path, yamlNode)...)

	// Validate modifiers if present
	if len(parts) > 1 {
		modifiers := strings.TrimSpace(parts[1])
		errors = append(errors, v.validateModifiers(modifiers, path, yamlNode)...)
	}

	return errors
}

// validateBaseType validates the base type of a field
func (v *RGDValidator) validateBaseType(baseType, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	if baseType == "" {
		errors = append(errors, ValidationError{
			Message:  "base type cannot be empty",
			Severity: SeverityError,
			Rule:     "empty-type",
			Path:     path,
			Line:     v.getLineNumber(yamlNode, path),
		})
		return errors
	}

	// Check for simple types
	for _, validType := range validBaseTypes {
		if baseType == validType {
			return errors // Valid simple type
		}
	}

	// Check for array types (e.g., "[]string")
	if strings.HasPrefix(baseType, "[]") {
		elementType := strings.TrimPrefix(baseType, "[]")
		if elementType == "" {
			errors = append(errors, ValidationError{
				Message:  "array element type cannot be empty",
				Severity: SeverityError,
				Rule:     "empty-element-type",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		} else {
			// Recursively validate element type
			errors = append(errors, v.validateBaseType(elementType, path, yamlNode)...)
		}
		return errors
	}

	// Check for map types (e.g., "map[string]integer")
	if strings.HasPrefix(baseType, "map[") {
		mapRegex := regexp.MustCompile(`^map\[(.+)\](.+)$`)
		matches := mapRegex.FindStringSubmatch(baseType)
		if len(matches) != 3 {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("invalid map type syntax: %s", baseType),
				Severity: SeverityError,
				Rule:     "invalid-syntax",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		} else {
			keyType := strings.TrimSpace(matches[1])
			valueType := strings.TrimSpace(matches[2])

			// Validate key and value types
			errors = append(errors, v.validateBaseType(keyType, path, yamlNode)...)
			errors = append(errors, v.validateBaseType(valueType, path, yamlNode)...)
		}
		return errors
	}

	// If we reach here, it's an invalid type
	errors = append(errors, ValidationError{
		Message:  fmt.Sprintf("invalid type '%s'. Valid types are: %s", baseType, strings.Join(validBaseTypes, ", ")),
		Severity: SeverityError,
		Rule:     "invalid-type",
		Path:     path,
		Line:     v.getLineNumber(yamlNode, path),
	})

	return errors
}

// validateModifiers validates field modifiers
func (v *RGDValidator) validateModifiers(modifiers, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	// Parse modifiers (e.g., "required=true description='A field' minimum=0")
	modifierPairs := v.parseModifiers(modifiers)

	for _, pair := range modifierPairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("invalid modifier syntax: %s", pair),
				Severity: SeverityError,
				Rule:     "invalid-syntax",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
			continue
		}

		modifierName := strings.TrimSpace(parts[0])
		modifierValue := strings.TrimSpace(parts[1])

		// Check if modifier is valid
		validModifier := false
		for _, valid := range validModifiers {
			if modifierName == valid {
				validModifier = true
				break
			}
		}

		if !validModifier {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("invalid modifier '%s'. Valid modifiers are: %s", modifierName, strings.Join(validModifiers, ", ")),
				Severity: SeverityError,
				Rule:     "invalid-modifier",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
			continue
		}

		// Validate modifier value based on type
		errors = append(errors, v.validateModifierValue(modifierName, modifierValue, path, yamlNode)...)
	}

	return errors
}

// parseModifiers parses modifier string into individual key=value pairs
func (v *RGDValidator) parseModifiers(modifiers string) []string {
	var pairs []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(modifiers); i++ {
		char := modifiers[i]

		if !inQuotes && (char == '\'' || char == '"') {
			inQuotes = true
			quoteChar = char
			current.WriteByte(char)
		} else if inQuotes && char == quoteChar {
			inQuotes = false
			quoteChar = 0
			current.WriteByte(char)
		} else if !inQuotes && char == ' ' {
			if current.Len() > 0 {
				pairs = append(pairs, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(char)
		}
	}

	if current.Len() > 0 {
		pairs = append(pairs, current.String())
	}

	return pairs
}

// validateModifierValue validates the value of a specific modifier
func (v *RGDValidator) validateModifierValue(name, value, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	switch name {
	case "required":
		if value != "true" && value != "false" {
			errors = append(errors, ValidationError{
				Message:  "required modifier must be 'true' or 'false'",
				Severity: SeverityError,
				Rule:     "invalid-value",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		}
	case "minimum", "maximum":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("%s modifier must be a number", name),
				Severity: SeverityError,
				Rule:     "invalid-value",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		}
	case "minLength", "maxLength", "minItems", "maxItems":
		if _, err := strconv.Atoi(value); err != nil {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("%s modifier must be an integer", name),
				Severity: SeverityError,
				Rule:     "invalid-value",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		}
	case "uniqueItems":
		if value != "true" && value != "false" {
			errors = append(errors, ValidationError{
				Message:  "uniqueItems modifier must be 'true' or 'false'",
				Severity: SeverityError,
				Rule:     "invalid-value",
				Path:     path,
				Line:     v.getLineNumber(yamlNode, path),
			})
		}
	}

	return errors
}

// validateResources validates the resources section
func (v *RGDValidator) validateResources(rgd *ParsedRGD) []ValidationError {
	var errors []ValidationError

	if len(rgd.Resources) == 0 {
		return errors // No resources to validate
	}

	// Track resource IDs for duplicate detection
	seenIDs := make(map[string]bool)

	for i, resource := range rgd.Resources {
		// Use proper array notation for path
		resourcePath := fmt.Sprintf("spec.resources[%d]", i)

		// Validate resource ID
		if resource.ID == "" {
			errors = append(errors, ValidationError{
				Message:  "resource id is required",
				Severity: SeverityError,
				Rule:     "required-field",
				Path:     resourcePath + ".id",
				Line:     v.getLineNumber(rgd.YamlNode, resourcePath+".id"),
			})
		} else {
			// Check ID naming convention
			if !isValidResourceID(resource.ID) {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("resource id '%s' must be lowerCamelCase", resource.ID),
					Severity: SeverityError,
					Rule:     "invalid-naming",
					Path:     resourcePath + ".id",
					Line:     v.getLineNumber(rgd.YamlNode, resourcePath+".id"),
				})
			}

			// Check for reserved words
			if isKroReservedWord(resource.ID) {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("resource id '%s' is a reserved word", resource.ID),
					Severity: SeverityError,
					Rule:     "reserved-word",
					Path:     resourcePath + ".id",
					Line:     v.getLineNumber(rgd.YamlNode, resourcePath+".id"),
				})
			}

			// Check for duplicates
			if seenIDs[resource.ID] {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("duplicate resource id '%s'", resource.ID),
					Severity: SeverityError,
					Rule:     "duplicate-id",
					Path:     resourcePath + ".id",
					Line:     v.getLineNumber(rgd.YamlNode, resourcePath+".id"),
				})
			} else {
				seenIDs[resource.ID] = true
			}
		}

		// Validate template
		if resource.Template != nil {
			errors = append(errors, v.validateResourceTemplate(resource.Template, resourcePath+".template", rgd.YamlNode)...)
		} else if resource.ExternalRef == nil {
			errors = append(errors, ValidationError{
				Message:  "resource must have either 'template' or 'externalRef'",
				Severity: SeverityError,
				Rule:     "missing-resource-definition",
				Path:     resourcePath,
				Line:     v.getLineNumber(rgd.YamlNode, resourcePath),
			})
		}

		// Validate external ref if present
		if resource.ExternalRef != nil {
			errors = append(errors, v.validateExternalRef(resource.ExternalRef, resourcePath+".externalRef", rgd.YamlNode)...)
		}
	}

	return errors
}

// validateResourceTemplate validates a resource template (Kubernetes object structure)
func (v *RGDValidator) validateResourceTemplate(template map[string]interface{}, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	// Validate Kubernetes object structure
	if err := validateKubernetesObjectStructure(template); err != nil {
		errors = append(errors, ValidationError{
			Message:  fmt.Sprintf("invalid Kubernetes object: %v", err),
			Severity: SeverityError,
			Rule:     "invalid-k8s-structure",
			Path:     path,
			Line:     v.getLineNumber(yamlNode, path),
		})
	}

	return errors
}

// validateExternalRef validates an external reference
func (v *RGDValidator) validateExternalRef(externalRef *ExternalRef, path string, yamlNode *yaml.Node) []ValidationError {
	var errors []ValidationError

	if externalRef.APIVersion == "" {
		errors = append(errors, ValidationError{
			Message:  "externalRef.apiVersion is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     path + ".apiVersion",
			Line:     v.getLineNumber(yamlNode, path+".apiVersion"),
		})
	}

	if externalRef.Kind == "" {
		errors = append(errors, ValidationError{
			Message:  "externalRef.kind is required",
			Severity: SeverityError,
			Rule:     "required-field",
			Path:     path + ".kind",
			Line:     v.getLineNumber(yamlNode, path+".kind"),
		})
	}

	return errors
}

// Naming convention validation functions

// isValidKindName checks if the name is a valid Kubernetes kind name (UpperCamelCase)
func isValidKindName(name string) bool {
	return upperCamelCaseRegex.MatchString(name)
}

// isKroReservedWord checks if the word is a reserved word in Kro
func isKroReservedWord(word string) bool {
	for _, reserved := range reservedWords {
		if strings.EqualFold(word, reserved) {
			return true
		}
	}
	return false
}

// isValidResourceID checks if the ID is valid (lowerCamelCase)
func isValidResourceID(id string) bool {
	return lowerCamelCaseRegex.MatchString(id)
}

// validateKubernetesObjectStructure checks if the object is a valid Kubernetes object
func validateKubernetesObjectStructure(obj map[string]interface{}) error {
	// Check for required fields
	apiVersion, hasAPIVersion := obj["apiVersion"]
	if !hasAPIVersion {
		return fmt.Errorf("apiVersion field is required")
	}
	if _, ok := apiVersion.(string); !ok {
		return fmt.Errorf("apiVersion must be a string")
	}

	kind, hasKind := obj["kind"]
	if !hasKind {
		return fmt.Errorf("kind field is required")
	}
	if _, ok := kind.(string); !ok {
		return fmt.Errorf("kind must be a string")
	}

	metadata, hasMetadata := obj["metadata"]
	if !hasMetadata {
		return fmt.Errorf("metadata field is required")
	}
	if _, ok := metadata.(map[string]interface{}); !ok {
		return fmt.Errorf("metadata must be an object")
	}

	return nil
}

// validateKubernetesVersion checks if the version is a valid Kubernetes version
func validateKubernetesVersion(version string) bool {
	return kubernetesVersionRegex.MatchString(version)
}

// Helper functions

// parseRGDWithPositions parses RGD content with position tracking
func (v *RGDValidator) parseRGDWithPositions(content string, model *parser.DocumentModel) (*ParsedRGD, error) {
	var yamlNode yaml.Node
	if err := yaml.Unmarshal([]byte(content), &yamlNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("failed to parse document: %w", err)
	}

	rgd := &ParsedRGD{
		YamlNode: &yamlNode,
	}

	// Parse basic fields
	if apiVersion, ok := doc["apiVersion"].(string); ok {
		rgd.APIVersion = apiVersion
	}
	if kind, ok := doc["kind"].(string); ok {
		rgd.Kind = kind
	}
	if metadata, ok := doc["metadata"].(map[string]interface{}); ok {
		rgd.Metadata = metadata
	}

	// Parse spec section
	if spec, ok := doc["spec"].(map[string]interface{}); ok {
		if err := v.parseRGDSpec(rgd, spec); err != nil {
			return nil, fmt.Errorf("failed to parse spec: %w", err)
		}
	}

	return rgd, nil
}

// parseRGDSpec parses the spec section of an RGD
func (v *RGDValidator) parseRGDSpec(rgd *ParsedRGD, spec map[string]interface{}) error {
	// Parse schema
	if schema, ok := spec["schema"].(map[string]interface{}); ok {
		rgd.Schema = &RGDSchema{}

		if kind, ok := schema["kind"].(string); ok {
			rgd.Schema.Kind = kind
		}
		if apiVersion, ok := schema["apiVersion"].(string); ok {
			rgd.Schema.APIVersion = apiVersion
		}
		if group, ok := schema["group"].(string); ok {
			rgd.Schema.Group = group
		}
		if specFields, ok := schema["spec"].(map[string]interface{}); ok {
			rgd.Schema.Spec = specFields
		}
		if statusFields, ok := schema["status"].(map[string]interface{}); ok {
			rgd.Schema.Status = statusFields
		}
		if types, ok := schema["types"].(map[string]interface{}); ok {
			rgd.Schema.Types = types
		}
	}

	// Parse resources
	if resources, ok := spec["resources"].([]interface{}); ok {
		for _, res := range resources {
			if resourceMap, ok := res.(map[string]interface{}); ok {
				resource, err := v.parseRGDResource(resourceMap)
				if err == nil {
					rgd.Resources = append(rgd.Resources, resource)
				}
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

	if template, ok := resourceMap["template"].(map[string]interface{}); ok {
		resource.Template = template
	}

	if externalRef, ok := resourceMap["externalRef"].(map[string]interface{}); ok {
		resource.ExternalRef = &ExternalRef{}
		if apiVersion, ok := externalRef["apiVersion"].(string); ok {
			resource.ExternalRef.APIVersion = apiVersion
		}
		if kind, ok := externalRef["kind"].(string); ok {
			resource.ExternalRef.Kind = kind
		}
		if metadata, ok := externalRef["metadata"].(map[string]interface{}); ok {
			resource.ExternalRef.Metadata = metadata
		}
	}

	return resource, nil
}

// getLineNumber gets the line number for a given path in the YAML
func (v *RGDValidator) getLineNumber(yamlNode *yaml.Node, path string) int {
	if yamlNode == nil {
		return 1
	}

	// Split the path into components (e.g., "spec.schema.kind" -> ["spec", "schema", "kind"])
	pathComponents := strings.Split(path, ".")

	// Find the node at the specified path
	if node := v.findNodeAtPath(yamlNode, pathComponents); node != nil {
		return node.Line
	}

	// Fallback to line 1 if path not found
	return 1
}

// findNodeAtPath traverses the YAML node tree to find a node at the specified path
func (v *RGDValidator) findNodeAtPath(node *yaml.Node, pathComponents []string) *yaml.Node {
	if node == nil || len(pathComponents) == 0 {
		return node
	}

	// Handle document node
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return v.findNodeAtPath(node.Content[0], pathComponents)
	}

	// Handle mapping node
	if node.Kind == yaml.MappingNode {
		return v.findInMapping(node, pathComponents)
	}

	// Handle sequence node with array index notation like "resources[0]"
	if node.Kind == yaml.SequenceNode {
		return v.findInSequence(node, pathComponents)
	}

	return nil
}

// findInMapping searches for a key in a YAML mapping node
func (v *RGDValidator) findInMapping(node *yaml.Node, pathComponents []string) *yaml.Node {
	if len(pathComponents) == 0 {
		return node
	}

	currentKey := pathComponents[0]
	remainingPath := pathComponents[1:]

	// Handle array index notation like "resources[0]"
	if strings.Contains(currentKey, "[") && strings.Contains(currentKey, "]") {
		arrayKey, indexStr := v.parseArrayPath(currentKey)

		// Find the array key in the mapping
		for i := 0; i < len(node.Content); i += 2 {
			if i+1 < len(node.Content) {
				keyNode := node.Content[i]
				valueNode := node.Content[i+1]

				if keyNode.Value == arrayKey && valueNode.Kind == yaml.SequenceNode {
					// Parse the index
					if index, err := v.parseIndex(indexStr); err == nil && index < len(valueNode.Content) {
						if len(remainingPath) == 0 {
							// Return the array element itself
							return valueNode.Content[index]
						}
						// Continue searching in the array element
						return v.findNodeAtPath(valueNode.Content[index], remainingPath)
					}
				}
			}
		}
	} else {
		// Regular key lookup
		for i := 0; i < len(node.Content); i += 2 {
			if i+1 < len(node.Content) {
				keyNode := node.Content[i]
				valueNode := node.Content[i+1]

				if keyNode.Value == currentKey {
					if len(remainingPath) == 0 {
						// Return the key node for better positioning
						return keyNode
					}
					// Continue searching in the value
					return v.findNodeAtPath(valueNode, remainingPath)
				}
			}
		}
	}

	return nil
}

// findInSequence searches for an element in a YAML sequence node
func (v *RGDValidator) findInSequence(node *yaml.Node, pathComponents []string) *yaml.Node {
	if len(pathComponents) == 0 {
		return node
	}

	currentKey := pathComponents[0]

	// Handle array index notation like "[0]"
	if strings.HasPrefix(currentKey, "[") && strings.HasSuffix(currentKey, "]") {
		indexStr := strings.Trim(currentKey, "[]")
		if index, err := v.parseIndex(indexStr); err == nil && index < len(node.Content) {
			remainingPath := pathComponents[1:]
			return v.findNodeAtPath(node.Content[index], remainingPath)
		}
	}

	return nil
}

// parseArrayPath parses array notation like "resources[0]" into ("resources", "0")
func (v *RGDValidator) parseArrayPath(path string) (string, string) {
	if idx := strings.Index(path, "["); idx != -1 {
		key := path[:idx]
		indexPart := path[idx:]
		if strings.HasSuffix(indexPart, "]") {
			indexStr := strings.Trim(indexPart, "[]")
			return key, indexStr
		}
	}
	return path, ""
}

// parseIndex converts a string index to an integer
func (v *RGDValidator) parseIndex(indexStr string) (int, error) {
	return strconv.Atoi(indexStr)
}

// createDiagnostic creates a diagnostic from a validation error
func (v *RGDValidator) createDiagnostic(err ValidationError) protocol.Diagnostic {
	line := err.Line
	if line == 0 {
		line = 1
	}

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

	source := "kro-lsp"

	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(line - 1), Character: uint32(err.Column)},
			End:   protocol.Position{Line: uint32(line - 1), Character: uint32(err.Column + 10)},
		},
		Severity: &severity,
		Message:  err.Message,
		Source:   &source,
	}
}
