package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kro-run/kro/api/v1alpha1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ValidationError stores error information with line position
type ValidationError struct {
	Message string
	Line    int
	Column  int
	EndLine int
	EndCol  int
}

func (e ValidationError) Error() string {
	return e.Message
}

// ValidateKroFileWithPositions validates a Kro document with position information
func ValidateKroFileWithPositions(doc *YAMLDocument) []ValidationError {
	var errors []ValidationError

	// 1. Validate apiVersion and kind
	if err := validateAPIVersionAndKindWithPosition(doc); err != nil {
		errors = append(errors, err...)
	}

	// 2. Validate metadata
	if err := validateMetadataWithPosition(doc); err != nil {
		errors = append(errors, err...)
	}

	// 3. Validate spec section exists
	if _, specExists := doc.Positions["spec"]; !specExists {
		// Determine where spec should be (after metadata)
		line := 2 // Default position if metadata not found
		if metadataField, exists := doc.Positions["metadata"]; exists {
			line = metadataField.EndLine + 1
		}

		errors = append(errors, ValidationError{
			Message: "spec section is required",
			Line:    line,
			Column:  0,
			EndLine: line,
			EndCol:  4, // Length of "spec"
		})
		return errors // Return early if spec missing
	}

	// 4. Validate schema types in spec.schema
	schemaTypeErrors := ValidateSchemaTypes(doc)
	errors = append(errors, schemaTypeErrors...)

	// 5. Additional validation using ResourceGraphDefinition
	// Extract data to validate through conversion to structured type
	if len(errors) == 0 && doc.Data != nil {
		rgd, err := mapToResourceGraphDefinition(doc.Data)
		if err != nil {
			// Add conversion error at the start of the document
			errors = append(errors, ValidationError{
				Message: fmt.Sprintf("Invalid format: %s", err.Error()),
				Line:    0,
				Column:  0,
				EndLine: 0,
				EndCol:  10,
			})
		} else if rgd != nil {
			// Perform additional validation
			validationErrors := validateResourceGraphDefinitionWithPositions(rgd, doc)
			errors = append(errors, validationErrors...)
		}
	}

	return errors
}

// validateAPIVersionAndKindWithPosition validates apiVersion and kind fields with position info
func validateAPIVersionAndKindWithPosition(doc *YAMLDocument) []ValidationError {
	var errors []ValidationError

	// Check if apiVersion exists
	apiVersionField, apiVersionExists := doc.Positions["apiVersion"]
	if !apiVersionExists {
		// Since apiVersion is missing, create error at start of document
		errors = append(errors, ValidationError{
			Message: "apiVersion is required and must be the first field",
			Line:    0,
			Column:  0,
			EndLine: 0,
			EndCol:  10,
		})
	} else {
		// Check apiVersion value
		apiVersionValue, ok := apiVersionField.Value.(string)
		if !ok || !strings.HasPrefix(apiVersionValue, "kro.run/v1alpha") {
			errors = append(errors, ValidationError{
				Message: fmt.Sprintf("apiVersion must be 'kro.run/v1alpha1', got '%v'", apiVersionField.Value),
				Line:    apiVersionField.Line,
				Column:  apiVersionField.Column,
				EndLine: apiVersionField.EndLine,
				EndCol:  apiVersionField.EndCol,
			})
		}

		// Check if apiVersion is the first field (line 0 or 1 in the document)
		if apiVersionField.Line > 1 {
			errors = append(errors, ValidationError{
				Message: "apiVersion must be the first field in the document",
				Line:    apiVersionField.Line,
				Column:  apiVersionField.Column,
				EndLine: apiVersionField.EndLine,
				EndCol:  apiVersionField.EndCol,
			})
		}
	}

	// Check if kind exists
	kindField, kindExists := doc.Positions["kind"]
	if !kindExists {
		// Since kind is missing, create error at start of document
		errors = append(errors, ValidationError{
			Message: "kind is required and must be the second field",
			Line:    1, // Assume it should be on the second line
			Column:  0,
			EndLine: 1,
			EndCol:  4,
		})
	} else {
		// Check kind value
		kindValue, ok := kindField.Value.(string)
		if !ok || kindValue != "ResourceGraphDefinition" {
			errors = append(errors, ValidationError{
				Message: fmt.Sprintf("kind must be 'ResourceGraphDefinition', got '%v'", kindField.Value),
				Line:    kindField.Line,
				Column:  kindField.Column,
				EndLine: kindField.EndLine,
				EndCol:  kindField.EndCol,
			})
		}

		// Check if kind is the second field
		if kindField.Line != 1 && apiVersionExists && kindField.Line != apiVersionField.Line+1 {
			errors = append(errors, ValidationError{
				Message: "kind must be the second field after apiVersion",
				Line:    kindField.Line,
				Column:  kindField.Column,
				EndLine: kindField.EndLine,
				EndCol:  kindField.EndCol,
			})
		}
	}

	return errors
}

// validateMetadataWithPosition validates metadata section with position info
func validateMetadataWithPosition(doc *YAMLDocument) []ValidationError {
	var errors []ValidationError

	// Check if metadata exists
	metadataField, metadataExists := doc.Positions["metadata"]
	if !metadataExists {
		// Metadata is missing, report error at line after kind
		kindLine := 1 // Default position if we don't have kind data
		if kindField, exists := doc.Positions["kind"]; exists {
			kindLine = kindField.Line
		}

		errors = append(errors, ValidationError{
			Message: "metadata section is required",
			Line:    kindLine + 1, // Assume it should be right after kind
			Column:  0,
			EndLine: kindLine + 1,
			EndCol:  8, // Length of "metadata"
		})
		return errors // Return early since we can't check metadata.name
	}

	// Extract metadata value
	metadataValue, ok := metadataField.Value.(map[string]interface{})
	if !ok {
		// Metadata is not a map, report error
		errors = append(errors, ValidationError{
			Message: "metadata must be a mapping",
			Line:    metadataField.Line,
			Column:  0,
			EndLine: metadataField.Line,
			EndCol:  100,
		})
		return errors // Return early since we can't check metadata.name
	}

	// Check if name exists in metadata
	nameField, nameExists := doc.NestedFields["metadata.name"]
	_, nameInMap := metadataValue["name"]

	if !nameExists && !nameInMap {
		// Name is missing completely, determine where it should be
		metadataLine := metadataField.Line

		errors = append(errors, ValidationError{
			Message: "metadata.name is required and must be a non-empty string",
			Line:    metadataLine + 1, // Assume name should be on next line after metadata
			Column:  2,                // Assume indentation of 2 spaces
			EndLine: metadataLine + 1,
			EndCol:  6, // Length of "name"
		})
	} else if nameExists && (nameField.Value == nil || nameField.Value == "") {
		// Name exists but is empty
		errors = append(errors, ValidationError{
			Message: "metadata.name must be a non-empty string",
			Line:    nameField.Line,
			Column:  0,
			EndLine: nameField.Line,
			EndCol:  100,
		})
	} else if nameInMap && !nameExists {
		// Name exists in the map but not in nested fields
		// This is a fallback case if our nested field tracking didn't work correctly
		errors = append(errors, ValidationError{
			Message: "metadata.name must be a valid string",
			Line:    metadataField.Line + 1,
			Column:  2,
			EndLine: metadataField.Line + 1,
			EndCol:  6,
		})
	}

	return errors
}

// mapToResourceGraphDefinition converts a map to a ResourceGraphDefinition
func mapToResourceGraphDefinition(data map[string]interface{}) (*v1alpha1.ResourceGraphDefinition, error) {
	// Convert map to unstructured
	u := &unstructured.Unstructured{Object: data}

	// Create a new ResourceGraphDefinition
	rgd := &v1alpha1.ResourceGraphDefinition{}

	// Convert unstructured to ResourceGraphDefinition
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, rgd)
	if err != nil {
		return nil, fmt.Errorf("error converting to ResourceGraphDefinition: %w", err)
	}

	return rgd, nil
}

// validateResourceGraphDefinitionWithPositions performs validation with position tracking
func validateResourceGraphDefinitionWithPositions(rgd *v1alpha1.ResourceGraphDefinition, doc *YAMLDocument) []ValidationError {
	var errors []ValidationError

	// Validate schema kind naming conventions
	if rgd.Spec.Schema != nil {
		// Check if kind name follows UpperCamelCase
		if rgd.Spec.Schema.Kind != "" {
			kindField, found := doc.NestedFields["spec.schema.kind"]
			if !found {
				// Fallback if we can't find the exact position
				kindField = findClosestField(doc, "spec.schema")
			}

			if len(rgd.Spec.Schema.Kind) > 0 && (rgd.Spec.Schema.Kind[0] < 'A' || rgd.Spec.Schema.Kind[0] > 'Z') {
				errors = append(errors, ValidationError{
					Message: fmt.Sprintf("kind '%s' is not a valid KRO kind name: must be UpperCamelCase", rgd.Spec.Schema.Kind),
					Line:    kindField.Line,
					Column:  kindField.Column,
					EndLine: kindField.EndLine,
					EndCol:  kindField.EndCol,
				})
			}
		} else {
			schemaField := findClosestField(doc, "spec.schema")
			errors = append(errors, ValidationError{
				Message: "spec.schema.kind is required",
				Line:    schemaField.Line,
				Column:  schemaField.Column,
				EndLine: schemaField.EndLine,
				EndCol:  schemaField.EndCol,
			})
		}

		// Validate schema apiVersion
		if rgd.Spec.Schema.APIVersion != "" {
			apiVersionField, found := doc.NestedFields["spec.schema.apiVersion"]
			if !found {
				// Fallback if we can't find the exact position
				apiVersionField = findClosestField(doc, "spec.schema")
			}

			if !strings.HasPrefix(rgd.Spec.Schema.APIVersion, "v") {
				errors = append(errors, ValidationError{
					Message: fmt.Sprintf("apiVersion '%s' is not a valid Kubernetes version, must start with 'v'", rgd.Spec.Schema.APIVersion),
					Line:    apiVersionField.Line,
					Column:  apiVersionField.Column,
					EndLine: apiVersionField.EndLine,
					EndCol:  apiVersionField.EndCol,
				})
			}
		}
	}

	// Validate resource IDs and templates
	if rgd.Spec.Resources != nil && len(rgd.Spec.Resources) > 0 {
		seen := make(map[string]struct{})

		for i, resource := range rgd.Spec.Resources {
			// Find position of this resource
			resourcePath := fmt.Sprintf("spec.resources[%d]", i)
			resourceIDPath := fmt.Sprintf("spec.resources[%d].id", i)
			resourceTemplatePath := fmt.Sprintf("spec.resources[%d].template", i)

			// Look for resource position
			resourceIDField, idFound := doc.NestedFields[resourceIDPath]
			resourceField := findClosestField(doc, resourcePath)

			// Check if ID is valid (starts with lowercase)
			if len(resource.ID) > 0 && (resource.ID[0] < 'a' || resource.ID[0] > 'z') {
				if idFound {
					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("id '%s' is not a valid KRO resource id: must be lower camelCase", resource.ID),
						Line:    resourceIDField.Line,
						Column:  resourceIDField.Column,
						EndLine: resourceIDField.EndLine,
						EndCol:  resourceIDField.EndCol,
					})
				} else {
					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("id '%s' is not a valid KRO resource id: must be lower camelCase", resource.ID),
						Line:    resourceField.Line,
						Column:  resourceField.Column,
						EndLine: resourceField.EndLine,
						EndCol:  resourceField.EndCol,
					})
				}
			}

			// Check for duplicate resource IDs
			if _, ok := seen[resource.ID]; ok {
				if idFound {
					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("found duplicate resource ID '%s'", resource.ID),
						Line:    resourceIDField.Line,
						Column:  resourceIDField.Column,
						EndLine: resourceIDField.EndLine,
						EndCol:  resourceIDField.EndCol,
					})
				} else {
					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("found duplicate resource ID '%s'", resource.ID),
						Line:    resourceField.Line,
						Column:  resourceField.Column,
						EndLine: resourceField.EndLine,
						EndCol:  resourceField.EndCol,
					})
				}
			}
			seen[resource.ID] = struct{}{}

			// Validate template structure
			if resource.Template.Raw != nil {
				var obj map[string]interface{}
				if err := json.Unmarshal(resource.Template.Raw, &obj); err != nil {
					templateField, found := doc.NestedFields[resourceTemplatePath]
					if !found {
						templateField = resourceField
					}

					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("invalid template for resource '%s': %s", resource.ID, err.Error()),
						Line:    templateField.Line,
						Column:  templateField.Column,
						EndLine: templateField.EndLine,
						EndCol:  templateField.EndCol,
					})
					continue
				}

				if err := validateKubernetesObjectStructure(obj); err != nil {
					templateField, found := doc.NestedFields[resourceTemplatePath]
					if !found {
						templateField = resourceField
					}

					errors = append(errors, ValidationError{
						Message: fmt.Sprintf("invalid Kubernetes object in resource '%s': %s", resource.ID, err.Error()),
						Line:    templateField.Line,
						Column:  templateField.Column,
						EndLine: templateField.EndLine,
						EndCol:  templateField.EndCol,
					})
				}
			}
		}
	}

	return errors
}

// findClosestField finds the closest parent field if the exact field is not found
func findClosestField(doc *YAMLDocument, path string) YAMLField {
	// Try direct lookup first
	if field, found := doc.NestedFields[path]; found {
		return field
	}

	// Try parent paths
	parts := strings.Split(path, ".")
	for i := len(parts) - 1; i > 0; i-- {
		parentPath := strings.Join(parts[:i], ".")
		if field, found := doc.NestedFields[parentPath]; found {
			return field
		}
	}

	// Fallback to spec if nothing else works
	if field, found := doc.Positions["spec"]; found {
		return field
	}

	// Last resort fallback
	return YAMLField{
		Line:    0,
		Column:  0,
		EndLine: 0,
		EndCol:  10,
	}
}

// validateKubernetesObjectStructure checks if the given object is a valid Kubernetes object
func validateKubernetesObjectStructure(obj map[string]interface{}) error {
	apiVersion, exists := obj["apiVersion"]
	if !exists {
		return fmt.Errorf("apiVersion field not found")
	}
	_, isString := apiVersion.(string)
	if !isString {
		return fmt.Errorf("apiVersion field is not a string")
	}

	kind, exists := obj["kind"]
	if !exists {
		return fmt.Errorf("kind field not found")
	}
	_, isString = kind.(string)
	if !isString {
		return fmt.Errorf("kind field is not a string")
	}

	metadata, exists := obj["metadata"]
	if !exists {
		return fmt.Errorf("metadata field not found")
	}
	_, isMap := metadata.(map[string]interface{})
	if !isMap {
		return fmt.Errorf("metadata field is not a map")
	}

	return nil
}
