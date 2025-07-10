package parser

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/tliron/commonlog"
)

type YamlParser struct {
	logger commonlog.Logger
}

type ParsedYAML struct {
	Root     ast.Node
	Content  string
	FilePath string
}

func NewYAMLParser(logger commonlog.Logger) *YamlParser {
	return &YamlParser{
		logger: logger,
	}
}

// parses YAML content and returns AST with position information
func (p *YamlParser) Parse(content, filePath string) (*ParsedYAML, error) {
	p.logger.Debugf("Parsing YAML file: %s", filePath)

	file, err := parser.ParseBytes([]byte(content), parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if len(file.Docs) == 0 {
		return &ParsedYAML{
			Root:     nil,
			Content:  content,
			FilePath: filePath,
		}, nil
	}

	root := file.Docs[0].Body

	return &ParsedYAML{
		Root:     root,
		Content:  content,
		FilePath: filePath,
	}, nil
}

// === AST Utility Functions ===

// finds a node by key in a mapping node
func FindNodeByKey(node ast.Node, key string) ast.Node {
	if node == nil {
		return nil
	}

	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		return nil
	}

	for _, value := range mapping.Values {
		if value.Key != nil {
			keyString := strings.TrimSpace(value.Key.String())
			keyString = strings.Trim(keyString, `"'`)
			if keyString == key {
				return value.Value
			}
		}
	}

	return nil
}

// gets the range from a node with proper start and end positions
func GetNodeRange(node ast.Node, content string) Range {
	if node == nil {
		return Range{
			Start: Position{Line: 1, Column: 1},
			End:   Position{Line: 1, Column: 1},
		}
	}

	token := node.GetToken()
	if token == nil {
		return Range{
			Start: Position{Line: 1, Column: 1},
			End:   Position{Line: 1, Column: 1},
		}
	}

	start := Position{
		Line:   token.Position.Line,
		Column: token.Position.Column,
	}

	// Calculate end position based on node content
	nodeText := strings.TrimSpace(node.String())
	end := calculateEndPosition(start, nodeText)

	return Range{
		Start: start,
		End:   end,
	}
}

// gets a value at a specific YAML path (e.g., "metadata.name")
func GetValueAtPath(node ast.Node, path string) ast.Node {
	if node == nil {
		return nil
	}

	parts := strings.Split(path, ".")
	current := node

	for _, part := range parts {
		current = FindNodeByKey(current, part)
		if current == nil {
			return nil
		}
	}

	return current
}

// gets a precise position for a field at a given path
func GetPrecisePosition(node ast.Node, path string, content string) Range {
	if node == nil {
		return Range{
			Start: Position{Line: 1, Column: 1},
			End:   Position{Line: 1, Column: 1},
		}
	}

	if path == "" {
		return GetNodeRange(node, content)
	}

	// Navigate to the specific path
	targetNode := GetValueAtPath(node, path)
	if targetNode != nil {
		return GetNodeRange(targetNode, content)
	}

	// If path not found, calculate where it should be
	return calculatePositionForMissingField(node, path, content)
}

// calculates where a missing field should be positioned
func calculatePositionForMissingField(node ast.Node, path string, content string) Range {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return Range{
			Start: Position{Line: 1, Column: 1},
			End:   Position{Line: 1, Column: 1},
		}
	}

	// Find the deepest existing parent
	current := node
	var lastExisting ast.Node
	existingPath := ""

	for _, part := range parts {
		next := FindNodeByKey(current, part)
		if next == nil {
			// This is where the missing field should be
			if lastExisting != nil {
				// Position after the last existing field
				lastRange := GetNodeRange(lastExisting, content)
				return Range{
					Start: Position{Line: lastRange.End.Line, Column: lastRange.End.Column},
					End:   Position{Line: lastRange.End.Line, Column: lastRange.End.Column + len(part) + 1},
				}
			}
			break
		}
		lastExisting = next
		current = next
		if existingPath == "" {
			existingPath = part
		} else {
			existingPath += "." + part
		}
	}

	// If we have a parent, position inside it
	if current != nil && current != node {
		currentRange := GetNodeRange(current, content)
		return Range{
			Start: Position{Line: currentRange.Start.Line + 1, Column: 3}, // Indented
			End:   Position{Line: currentRange.Start.Line + 1, Column: 3 + len(parts[len(parts)-1])},
		}
	}

	// Default to document start
	return Range{
		Start: Position{Line: 1, Column: 1},
		End:   Position{Line: 1, Column: 1},
	}
}

// returns the type of a node value for validation
func GetNodeType(node ast.Node) string {
	if node == nil {
		return "unknown"
	}

	switch n := node.(type) {
	case *ast.StringNode:
		return "string"
	case *ast.IntegerNode:
		return "integer"
	case *ast.FloatNode:
		return "number"
	case *ast.BoolNode:
		return "boolean"
	case *ast.MappingNode:
		return "object"
	case *ast.SequenceNode:
		return "array"
	case *ast.NullNode:
		return "null"
	default:
		// For other types, try to infer from string representation
		str := strings.TrimSpace(n.String())
		if str == "true" || str == "false" {
			return "boolean"
		}
		if strings.HasPrefix(str, "[") && strings.HasSuffix(str, "]") {
			return "array"
		}
		if strings.HasPrefix(str, "{") && strings.HasSuffix(str, "}") {
			return "object"
		}
		return "string"
	}
}

// === Private helper functions ===

// calculates end position from start position and text
func calculateEndPosition(start Position, text string) Position {
	lines := strings.Split(text, "\n")

	if len(lines) == 1 {
		// Single line
		return Position{
			Line:   start.Line,
			Column: start.Column + len(text),
		}
	}

	// Multi-line
	return Position{
		Line:   start.Line + len(lines) - 1,
		Column: len(lines[len(lines)-1]) + 1,
	}
}
