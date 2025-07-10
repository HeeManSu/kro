package validation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/kro-run/kro/tools/lsp/server/parser"
	"github.com/tliron/commonlog"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type RGDValidator struct {
	logger     commonlog.Logger
	crdManager *CRDManager
}

func NewRGDValidator(logger commonlog.Logger) *RGDValidator {
	return &RGDValidator{
		logger: logger,
	}
}

func (v *RGDValidator) SetCRDManager(crdManager *CRDManager) {
	v.crdManager = crdManager
	v.logger.Debug("CRD manager set for RGD validator")
}

func (v *RGDValidator) ValidateRGD(parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	// Basic validation checks
	// if parsed == nil || parsed.Root == nil {
	// 	return []ValidationError{{
	// 		Message:  "Document is empty or invalid",
	// 		Range:    parser.Range{Start: parser.Position{Line: 1, Column: 1}, End: parser.Position{Line: 1, Column: 1}},
	// 		Severity: "error",
	// 		Source:   "kro-lsp",
	// 	}}
	// }

	// 1. Basic structure validation
	errors = append(errors, v.validateBasicStructure(parsed)...)

	// 2. Skip further validation if basic structure is invalid
	if len(errors) > 0 {
		return errors
	}

	// 3. Metadata validation
	errors = append(errors, v.validateMetadata(parsed)...)

	// 4. Spec section validation
	errors = append(errors, v.validateSpec(parsed)...)

	v.logger.Debugf("RGD validation completed with %d errors", len(errors))
	return errors
}

func (v *RGDValidator) IsRGDFile(parsed *parser.ParsedYAML) bool {
	if parsed.Root == nil {
		return false
	}

	apiVersionNode := parser.FindNodeByKey(parsed.Root, "apiVersion")
	kindNode := parser.FindNodeByKey(parsed.Root, "kind")

	if apiVersionNode == nil || kindNode == nil {
		return false
	}

	apiVersion := strings.TrimSpace(apiVersionNode.String())
	kind := strings.TrimSpace(kindNode.String())

	apiVersion = strings.Trim(apiVersion, `"'`)
	kind = strings.Trim(kind, `"'`)

	return strings.Contains(apiVersion, "kro.run") && kind == "ResourceGraphDefinition"
}

func (v *RGDValidator) validateBasicStructure(parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	// Check required fields: apiVersion, kind, metadata
	// Add spec and resources
	requiredFields := []string{"apiVersion", "kind", "metadata"}

	for _, field := range requiredFields {
		node := parser.FindNodeByKey(parsed.Root, field)
		if node == nil {
			errorRange := parser.GetPrecisePosition(parsed.Root, field, parsed.Content)
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("RGD must have '%s' field", field),
				Range:    errorRange,
				Severity: "error",
				Source:   "kro-lsp",
			})
		}
	}

	return errors
}

func (v *RGDValidator) validateMetadata(parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	metadataNode := parser.FindNodeByKey(parsed.Root, "metadata")

	// Check metadata.name is required and not empty
	nameNode := parser.FindNodeByKey(metadataNode, "name")
	if nameNode == nil {
		errorRange := parser.GetPrecisePosition(metadataNode, "name", parsed.Content)
		errors = append(errors, ValidationError{
			Message:  "metadata must have 'name' field",
			Range:    errorRange,
			Severity: "error",
			Source:   "kro-lsp",
		})
		return errors
	}

	nameValue := strings.TrimSpace(nameNode.String())
	nameValue = strings.Trim(nameValue, `"'`)
	if nameValue == "" {
		errors = append(errors, ValidationError{
			Message:  "metadata.name cannot be empty",
			Range:    parser.GetNodeRange(nameNode, parsed.Content),
			Severity: "error",
			Source:   "kro-lsp",
		})
	}

	return errors
}

func (v *RGDValidator) validateSpec(parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	specNode := parser.FindNodeByKey(parsed.Root, "spec")
	if specNode == nil {
		return errors
	}

	// Validate schema
	errors = append(errors, v.validateSchema(specNode, parsed)...)

	// Validate resources
	errors = append(errors, v.validateResources(specNode, parsed)...)

	return errors
}

func (v *RGDValidator) validateSchema(specNode ast.Node, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	schemaNode := parser.FindNodeByKey(specNode, "schema")
	if schemaNode == nil {
		return errors
	}

	// Checks for kind and apiVersion
	if kindNode := parser.FindNodeByKey(schemaNode, "kind"); kindNode == nil {
		errorRange := parser.GetPrecisePosition(schemaNode, "kind", parsed.Content)
		errors = append(errors, ValidationError{
			Message:  "schema must have 'kind' field",
			Range:    errorRange,
			Severity: "error",
			Source:   "kro-lsp",
		})
	}

	if apiVersionNode := parser.FindNodeByKey(schemaNode, "apiVersion"); apiVersionNode == nil {
		errorRange := parser.GetPrecisePosition(schemaNode, "apiVersion", parsed.Content)
		errors = append(errors, ValidationError{
			Message:  "schema must have 'apiVersion' field",
			Range:    errorRange,
			Severity: "error",
			Source:   "kro-lsp",
		})
	}

	return errors
}

func (v *RGDValidator) validateResources(specNode ast.Node, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	resourcesNode := parser.FindNodeByKey(specNode, "resources")
	if resourcesNode == nil {
		return errors
	}

	sequence, ok := resourcesNode.(*ast.SequenceNode)
	if !ok {
		errors = append(errors, ValidationError{
			Message:  "spec.resources must be an array",
			Range:    parser.GetNodeRange(resourcesNode, parsed.Content),
			Severity: "error",
			Source:   "kro-lsp",
		})
		return errors
	}

	// Validate each resource
	for i, resourceNode := range sequence.Values {
		errors = append(errors, v.validateResource(resourceNode, fmt.Sprintf("resources[%d]", i), parsed)...)
	}

	return errors
}

func (v *RGDValidator) validateResource(resourceNode ast.Node, path string, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	mapping, ok := resourceNode.(*ast.MappingNode)
	if !ok {
		errors = append(errors, ValidationError{
			Message:  fmt.Sprintf("%s must be an object", path),
			Range:    parser.GetNodeRange(resourceNode, parsed.Content),
			Severity: "error",
			Source:   "kro-lsp",
		})
		return errors
	}

	// Check required field: id
	idNode := parser.FindNodeByKey(mapping, "id")
	if idNode == nil {
		errorRange := parser.GetPrecisePosition(resourceNode, "id", parsed.Content)
		errors = append(errors, ValidationError{
			Message:  fmt.Sprintf("%s must have 'id' field", path),
			Range:    errorRange,
			Severity: "error",
			Source:   "kro-lsp",
		})
	} else {
		// Basic id validation
		idValue := strings.TrimSpace(idNode.String())
		idValue = strings.Trim(idValue, `"'`)
		if idValue == "" {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("%s.id cannot be empty", path),
				Range:    parser.GetNodeRange(idNode, parsed.Content),
				Severity: "error",
				Source:   "kro-lsp",
			})
		}
	}

	// Validate template if present
	if templateNode := parser.FindNodeByKey(mapping, "template"); templateNode != nil {
		errors = append(errors, v.validateTemplate(templateNode, path+".template", parsed)...)
	}

	return errors
}

func (v *RGDValidator) validateTemplate(templateNode ast.Node, path string, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	mapping, ok := templateNode.(*ast.MappingNode)
	if !ok {
		errors = append(errors, ValidationError{
			Message:  fmt.Sprintf("%s must be an object", path),
			Range:    parser.GetNodeRange(templateNode, parsed.Content),
			Severity: "error",
			Source:   "kro-lsp",
		})
		return errors
	}

	// Check required fields: apiVersion, kind
	requiredFields := []string{"apiVersion", "kind"}

	for _, field := range requiredFields {
		node := parser.FindNodeByKey(mapping, field)
		if node == nil {
			errorRange := parser.GetPrecisePosition(templateNode, field, parsed.Content)
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("%s must have '%s' field", path, field),
				Range:    errorRange,
				Severity: "error",
				Source:   "kro-lsp",
			})
		}
	}

	// CRD validation
	if v.crdManager != nil && v.crdManager.IsEnabled() {
		errors = append(errors, v.validateTemplateAgainstCRD(templateNode, path, parsed)...)
	}

	return errors
}

func (v *RGDValidator) validateTemplateAgainstCRD(templateNode ast.Node, path string, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	gvk, err := v.extractGVKFromTemplate(templateNode)
	if err != nil {
		return errors
	}

	crdSchema := v.crdManager.GetCRDSchema(gvk)
	if crdSchema == nil {
		return errors
	}

	templateData, err := v.convertTemplateToMap(templateNode)
	if err != nil {
		return errors
	}

	if crdSchema.Schema != nil {
		schemaErrors := v.validateMapAgainstSchema(templateData, crdSchema.Schema, path, templateNode, parsed)
		for _, schemaError := range schemaErrors {
			schemaError.Source = "kro-crd"
			errors = append(errors, schemaError)
		}
	}

	return errors
}

func (v *RGDValidator) validateMapAgainstSchema(data map[string]interface{}, schema *v1.JSONSchemaProps, path string, templateNode ast.Node, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	if schema == nil {
		return errors
	}

	// Validate object type and properties
	if schema.Type == "object" && schema.Properties != nil {
		// Check required fields
		for _, requiredField := range schema.Required {
			if _, exists := data[requiredField]; !exists {
				fieldRange := v.getFieldPositionEnhanced(templateNode, requiredField, parsed)
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Required field '%s' is missing", requiredField),
					Range:    fieldRange,
					Severity: "error",
				})
			}
		}

		// Validate existing fields
		for fieldName, fieldValue := range data {
			fieldSchema, schemaExists := schema.Properties[fieldName]

			if !schemaExists {
				// Unknown field - warning in case it's a CEL expression placeholder
				fieldRange := v.getFieldPositionEnhanced(templateNode, fieldName, parsed)
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Unknown field '%s' (not defined in CRD schema)", fieldName),
					Range:    fieldRange,
					Severity: "warning",
				})
				continue
			}

			// Recursively validate field
			fieldErrors := v.validateValueAgainstSchema(fieldValue, &fieldSchema, path, templateNode, fieldName, parsed)
			errors = append(errors, fieldErrors...)
		}
	}

	return errors
}

// validateValueAgainstSchema - Validates a specific value against its schema
func (v *RGDValidator) validateValueAgainstSchema(value interface{}, schema *v1.JSONSchemaProps, path string, templateNode ast.Node, fieldName string, parsed *parser.ParsedYAML) []ValidationError {
	var errors []ValidationError

	if schema == nil {
		return errors
	}

	// Get precise position for this field
	fieldRange := v.getFieldPositionEnhanced(templateNode, fieldName, parsed)

	// Type validation
	if schema.Type != "" {
		if !v.validateType(value, schema.Type) {
			errors = append(errors, ValidationError{
				Message:  fmt.Sprintf("Field '%s' must be of type '%s', got '%T'", fieldName, schema.Type, value),
				Range:    fieldRange,
				Severity: "error",
			})
			return errors // Skip further validation if type is wrong
		}
	}

	// String-specific validations
	if schema.Type == "string" {
		if str, ok := value.(string); ok {
			// Check if value looks like a CEL expression - skip validation
			if v.isCELExpression(str) {
				v.logger.Debugf("Skipping validation for CEL expression in field '%s'", fieldName)
				return errors
			}

			// Pattern validation
			if schema.Pattern != "" {
				matched, err := regexp.MatchString(schema.Pattern, str)
				if err != nil {
					v.logger.Debugf("Invalid regex pattern in schema: %s", schema.Pattern)
				} else if !matched {
					errors = append(errors, ValidationError{
						Message:  fmt.Sprintf("Field '%s' does not match pattern '%s'", fieldName, schema.Pattern),
						Range:    fieldRange,
						Severity: "error",
					})
				}
			}

			// Enum validation - FIXED: Convert byte arrays to readable strings
			if len(schema.Enum) > 0 {
				validValues := make([]string, len(schema.Enum))
				found := false
				for i, enumVal := range schema.Enum {
					// Convert enum value to string properly
					enumStr := v.convertEnumToString(enumVal)
					validValues[i] = enumStr
					if str == enumStr {
						found = true
						break
					}
				}
				if !found {
					errors = append(errors, ValidationError{
						Message:  fmt.Sprintf("Field '%s' must be one of [%s], got '%s'", fieldName, strings.Join(validValues, ", "), str),
						Range:    fieldRange,
						Severity: "error",
					})
				}
			}

			// MinLength validation
			if schema.MinLength != nil && int64(len(str)) < *schema.MinLength {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be at least %d characters long", fieldName, *schema.MinLength),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// MaxLength validation
			if schema.MaxLength != nil && int64(len(str)) > *schema.MaxLength {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be at most %d characters long", fieldName, *schema.MaxLength),
					Range:    fieldRange,
					Severity: "error",
				})
			}
		}
	}

	// Integer/Number-specific validations
	if schema.Type == "integer" || schema.Type == "number" {
		if num, ok := v.convertToFloat64(value); ok {
			// Minimum validation
			if schema.Minimum != nil && num < *schema.Minimum {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be >= %v", fieldName, *schema.Minimum),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// Maximum validation
			if schema.Maximum != nil && num > *schema.Maximum {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be <= %v", fieldName, *schema.Maximum),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// ExclusiveMinimum validation (in v1, this is a boolean flag)
			if schema.ExclusiveMinimum && schema.Minimum != nil && num <= *schema.Minimum {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be > %v", fieldName, *schema.Minimum),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// ExclusiveMaximum validation (in v1, this is a boolean flag)
			if schema.ExclusiveMaximum && schema.Maximum != nil && num >= *schema.Maximum {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must be < %v", fieldName, *schema.Maximum),
					Range:    fieldRange,
					Severity: "error",
				})
			}
		}
	}

	// Array-specific validations
	if schema.Type == "array" {
		if arr, ok := value.([]interface{}); ok {
			// MinItems validation
			if schema.MinItems != nil && int64(len(arr)) < *schema.MinItems {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must have at least %d items", fieldName, *schema.MinItems),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// MaxItems validation
			if schema.MaxItems != nil && int64(len(arr)) > *schema.MaxItems {
				errors = append(errors, ValidationError{
					Message:  fmt.Sprintf("Field '%s' must have at most %d items", fieldName, *schema.MaxItems),
					Range:    fieldRange,
					Severity: "error",
				})
			}

			// Validate array items
			if schema.Items != nil && schema.Items.Schema != nil {
				for i, item := range arr {
					itemErrors := v.validateValueAgainstSchema(item, schema.Items.Schema, path, templateNode, fmt.Sprintf("%s[%d]", fieldName, i), parsed)
					errors = append(errors, itemErrors...)
				}
			}
		}
	}

	// Object-specific validations
	if schema.Type == "object" {
		if obj, ok := value.(map[string]interface{}); ok {
			// Recursively validate nested object
			nestedErrors := v.validateMapAgainstSchema(obj, schema, path, templateNode, parsed)
			errors = append(errors, nestedErrors...)
		}
	}

	return errors
}

// Helper functions

// convertEnumToString converts an enum value to a readable string
func (v *RGDValidator) convertEnumToString(enumVal v1.JSON) string {
	if enumVal.Raw == nil {
		return ""
	}

	// Convert byte array to string and remove quotes
	str := string(enumVal.Raw)
	str = strings.Trim(str, `"`)
	return str
}

// validateType checks if a value matches the expected JSON schema type
func (v *RGDValidator) validateType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		switch value.(type) {
		case int, int32, int64, float64:
			// Check if float64 is actually an integer
			if f, ok := value.(float64); ok {
				return f == float64(int64(f))
			}
			return true
		}
		return false
	case "number":
		switch value.(type) {
		case int, int32, int64, float32, float64:
			return true
		}
		return false
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "null":
		return value == nil
	}
	return true // Unknown type, assume valid
}

// convertToFloat64 converts various numeric types to float64
func (v *RGDValidator) convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// isCELExpression checks if a string looks like a CEL expression
func (v *RGDValidator) isCELExpression(value string) bool {
	// Simple heuristic: check for common CEL patterns
	celPatterns := []string{
		"${",                            // Template expressions
		"$.",                            // Field references
		"spec.", "status.", "metadata.", // Common K8s field references
	}

	for _, pattern := range celPatterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}

// getFieldPositionEnhanced - FIXED: Better AST navigation with proper field finding
func (v *RGDValidator) getFieldPositionEnhanced(templateNode ast.Node, fieldName string, parsed *parser.ParsedYAML) parser.Range {
	// First, try to find the field directly in the template node
	if fieldNode := v.findFieldNodeRecursive(templateNode, fieldName); fieldNode != nil {
		range_ := parser.GetNodeRange(fieldNode, parsed.Content)
		v.logger.Debugf("✅ Found field '%s' at line %d, column %d", fieldName, range_.Start.Line, range_.Start.Column)
		return range_
	}

	// If not found directly, try to find the key node (for missing field positioning)
	if keyNode := v.findKeyNodeRecursive(templateNode, fieldName); keyNode != nil {
		range_ := parser.GetNodeRange(keyNode, parsed.Content)
		v.logger.Debugf("✅ Found key '%s' at line %d, column %d", fieldName, range_.Start.Line, range_.Start.Column)
		return range_
	}

	// Last fallback - use template position but log it
	fallbackRange := parser.GetNodeRange(templateNode, parsed.Content)
	v.logger.Debugf("⚠️ Field '%s' not found, using template position: line %d", fieldName, fallbackRange.Start.Line)
	return fallbackRange
}

// findFieldNodeRecursive - ENHANCED: Deep recursive search for field nodes
func (v *RGDValidator) findFieldNodeRecursive(node ast.Node, fieldName string) ast.Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.MappingNode:
		// First check direct children
		for _, value := range n.Values {
			if value.Key != nil {
				keyStr := v.cleanString(value.Key.String())
				if keyStr == fieldName {
					return value.Value // Found it!
				}
			}
		}

		// Then recursively search in all child nodes
		for _, value := range n.Values {
			if found := v.findFieldNodeRecursive(value.Value, fieldName); found != nil {
				return found
			}
		}

	case *ast.SequenceNode:
		// Search in all sequence items
		for _, item := range n.Values {
			if found := v.findFieldNodeRecursive(item, fieldName); found != nil {
				return found
			}
		}
	}

	return nil
}

// findKeyNodeRecursive - ENHANCED: Deep recursive search for key nodes
func (v *RGDValidator) findKeyNodeRecursive(node ast.Node, fieldName string) ast.Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.MappingNode:
		// First check direct children
		for _, value := range n.Values {
			if value.Key != nil {
				keyStr := v.cleanString(value.Key.String())
				if keyStr == fieldName {
					return value.Key // Found the key!
				}
			}
		}

		// Then recursively search in all child nodes
		for _, value := range n.Values {
			if found := v.findKeyNodeRecursive(value.Value, fieldName); found != nil {
				return found
			}
		}

	case *ast.SequenceNode:
		// Search in all sequence items
		for _, item := range n.Values {
			if found := v.findKeyNodeRecursive(item, fieldName); found != nil {
				return found
			}
		}
	}

	return nil
}

// cleanString removes quotes and trims whitespace
func (v *RGDValidator) cleanString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}

func (v *RGDValidator) extractGVKFromTemplate(templateNode ast.Node) (schema.GroupVersionKind, error) {
	var gvk schema.GroupVersionKind

	mapping, ok := templateNode.(*ast.MappingNode)
	if !ok {
		return gvk, fmt.Errorf("template is not a mapping node")
	}

	// Get apiVersion
	apiVersionNode := parser.FindNodeByKey(mapping, "apiVersion")
	if apiVersionNode == nil {
		return gvk, fmt.Errorf("template missing apiVersion")
	}
	apiVersion := strings.TrimSpace(apiVersionNode.String())
	apiVersion = strings.Trim(apiVersion, `"'`)

	// Get kind
	kindNode := parser.FindNodeByKey(mapping, "kind")
	if kindNode == nil {
		return gvk, fmt.Errorf("template missing kind")
	}
	kind := strings.TrimSpace(kindNode.String())
	kind = strings.Trim(kind, `"'`)

	// Parse group and version from apiVersion
	var group, version string
	if parts := strings.Split(apiVersion, "/"); len(parts) == 2 {
		group = parts[0]
		version = parts[1]
	} else if len(parts) == 1 {
		// Core API group
		group = ""
		version = parts[0]
	} else {
		return gvk, fmt.Errorf("invalid apiVersion format: %s", apiVersion)
	}

	return schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}, nil
}

// converts a template AST node to a map for validation
func (v *RGDValidator) convertTemplateToMap(templateNode ast.Node) (map[string]interface{}, error) {
	// Note: Explore YAML-to-map conversion

	result := make(map[string]interface{})

	mapping, ok := templateNode.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("template is not a mapping node")
	}

	for _, value := range mapping.Values {
		if value.Key != nil && value.Value != nil {
			keyStr := strings.TrimSpace(value.Key.String())
			keyStr = strings.Trim(keyStr, `"'`)

			valueData := v.convertASTNodeToValue(value.Value)
			result[keyStr] = valueData
		}
	}

	return result, nil
}

// converts an AST node to a Go value for validation
func (v *RGDValidator) convertASTNodeToValue(node ast.Node) interface{} {
	switch n := node.(type) {
	case *ast.StringNode:
		return strings.Trim(n.Value, `"'`)
	case *ast.IntegerNode:
		if valStr, ok := n.Value.(string); ok {
			if val, err := strconv.ParseInt(valStr, 10, 64); err == nil {
				return val
			}
		}
		return n.Value
	case *ast.FloatNode:
		return n.Value // FloatNode.Value is already float64
	case *ast.BoolNode:
		return n.Value
	case *ast.NullNode:
		return nil
	case *ast.SequenceNode:
		result := make([]interface{}, len(n.Values))
		for i, item := range n.Values {
			result[i] = v.convertASTNodeToValue(item)
		}
		return result
	case *ast.MappingNode:
		result := make(map[string]interface{})
		for _, value := range n.Values {
			if value.Key != nil && value.Value != nil {
				keyStr := strings.TrimSpace(value.Key.String())
				keyStr = strings.Trim(keyStr, `"'`)
				result[keyStr] = v.convertASTNodeToValue(value.Value)
			}
		}
		return result
	default:
		// For other node types, return the string representation
		return strings.TrimSpace(node.String())
	}
}
