package parser

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	yamltoken "github.com/goccy/go-yaml/token"
)

// Node represents a YAML AST node with position information
type Node struct {
	Value    any
	Children map[string]*Node
	Range    Range
	Parent   *Node
	ASTNode  ast.Node // Keep reference to original AST node
}

// DocumentModel represents the YAML document structure
type DocumentModel struct {
	RootMap  map[string]*Node
	RootNode *Node
}

// YAMLParser handles the parsing operation
type YAMLParser struct {
	content string
}

// NewYAMLParser creates a new YAML parser instance
func NewYAMLParser() *YAMLParser {
	return &YAMLParser{}
}

// Parse parses YAML content and returns a document model with position information
func (p *YAMLParser) Parse(content string) (*DocumentModel, error) {
	p.content = content

	// Parse the YAML into an AST
	file, err := parser.ParseBytes([]byte(content), 0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Convert the AST to our document model
	model := &DocumentModel{
		RootMap: make(map[string]*Node),
	}

	// Process each document in the file
	for _, doc := range file.Docs {
		if doc.Body != nil {
			model.RootNode = p.processNode(doc.Body, nil)

			// If the root is a mapping, populate RootMap
			if mappingNode, ok := doc.Body.(*ast.MappingNode); ok {
				for _, value := range mappingNode.Values {
					if keyNode, ok := value.Key.(*ast.StringNode); ok {
						model.RootMap[keyNode.Value] = p.processNode(value.Value, model.RootNode)
					}
				}
			}
		}
	}

	return model, nil
}

// processNode builds our structured model from AST nodes
func (p *YAMLParser) processNode(astNode ast.Node, parent *Node) *Node {
	node := &Node{
		Children: make(map[string]*Node),
		Range:    p.rangeFromNode(astNode),
		Parent:   parent,
		ASTNode:  astNode,
	}

	switch n := astNode.(type) {
	case *ast.MappingNode:
		// Process mapping (object) nodes
		mapValue := make(map[string]interface{})
		for _, value := range n.Values {
			if keyNode := p.getStringValue(value.Key); keyNode != "" {
				childNode := p.processNode(value.Value, node)
				node.Children[keyNode] = childNode
				mapValue[keyNode] = childNode.Value
			}
		}
		node.Value = mapValue

	case *ast.SequenceNode:
		// Process sequence (array) nodes
		var seqValue []interface{}
		for i, value := range n.Values {
			childNode := p.processNode(value, node)
			// Store array items with index as key
			node.Children[fmt.Sprintf("[%d]", i)] = childNode
			seqValue = append(seqValue, childNode.Value)
		}
		node.Value = seqValue

	case *ast.StringNode:
		node.Value = n.Value

	case *ast.IntegerNode:
		node.Value = n.Value

	case *ast.FloatNode:
		node.Value = n.Value

	case *ast.BoolNode:
		node.Value = n.Value

	case *ast.NullNode:
		node.Value = nil

	case *ast.LiteralNode:
		node.Value = n.Value.Value

	case *ast.AnchorNode:
		// Handle anchor references
		if n.Name != nil {
			node.Value = p.processNode(n.Name, node).Value
		}

	case *ast.AliasNode:
		// Handle alias references
		if n.Value != nil {
			node.Value = p.processNode(n.Value, node).Value
		}
	}

	return node
}

// rangeFromNode extracts range information from an AST node
func (p *YAMLParser) rangeFromNode(node ast.Node) Range {
	tok := node.GetToken()
	if tok == nil {
		return Range{}
	}

	startPos := p.positionFromToken(tok.Position)

	// Calculate end position based on node type and content
	endPos := startPos

	switch n := node.(type) {
	case *ast.MappingNode:
		// For mappings, find the last value's end position
		if len(n.Values) > 0 {
			lastValue := n.Values[len(n.Values)-1]
			endPos = p.rangeFromNode(lastValue.Value).End
		}

	case *ast.SequenceNode:
		// For sequences, find the last item's end position
		if len(n.Values) > 0 {
			lastValue := n.Values[len(n.Values)-1]
			endPos = p.rangeFromNode(lastValue).End
		}

	default:
		// For scalar values, calculate based on content length
		valueStr := p.getNodeString(node)
		lines := strings.Split(valueStr, "\n")
		if len(lines) > 1 {
			endPos.Line = startPos.Line + len(lines) - 1
			endPos.Character = len(lines[len(lines)-1])
		} else {
			endPos.Character = startPos.Character + len(valueStr)
		}
	}

	return Range{Start: startPos, End: endPos}
}

// positionFromToken converts a YAML token position to our Position type
func (p *YAMLParser) positionFromToken(pos *yamltoken.Position) Position {
	if pos == nil {
		return Position{}
	}
	return Position{
		Line:      pos.Line - 1, // Convert to 0-based
		Character: pos.Column - 1,
	}
}

// getStringValue extracts string value from a node
func (p *YAMLParser) getStringValue(node ast.Node) string {
	switch n := node.(type) {
	case *ast.StringNode:
		return n.Value
	case *ast.LiteralNode:
		return n.Value.Value
	default:
		return ""
	}
}

// getNodeString gets the string representation of a node
func (p *YAMLParser) getNodeString(node ast.Node) string {
	switch n := node.(type) {
	case *ast.StringNode:
		return n.Value
	case *ast.IntegerNode:
		return fmt.Sprintf("%d", n.Value)
	case *ast.FloatNode:
		return fmt.Sprintf("%f", n.Value)
	case *ast.BoolNode:
		return fmt.Sprintf("%t", n.Value)
	case *ast.LiteralNode:
		return n.Value.Value
	default:
		return ""
	}
}

// FindNodeAtPosition finds the most specific node at a given position
func (model *DocumentModel) FindNodeAtPosition(pos Position) *Node {
	if model.RootNode == nil {
		return nil
	}
	return findNodeAtPositionRecursive(model.RootNode, pos)
}

func findNodeAtPositionRecursive(node *Node, pos Position) *Node {
	if !IsPositionInRange(pos, node.Range) {
		return nil
	}

	// Check children for more specific match
	for _, child := range node.Children {
		if found := findNodeAtPositionRecursive(child, pos); found != nil {
			return found
		}
	}

	// No child contains the position, so this node is the most specific
	return node
}

// GetPath returns the path to a node from the root
func (n *Node) GetPath() []string {
	var path []string
	current := n

	for current != nil && current.Parent != nil {
		// Find the key for this node in its parent
		for key, child := range current.Parent.Children {
			if child == current {
				path = append([]string{key}, path...)
				break
			}
		}
		current = current.Parent
	}

	return path
}

// IsCELExpression checks if a node contains a CEL expression
func IsCELExpression(node *Node) bool {
	if str, ok := node.Value.(string); ok {
		// Simple heuristic: check for ${} pattern
		return strings.Contains(str, "${") && strings.Contains(str, "}")
	}
	return false
}

// ExtractCELExpression extracts the CEL expression from a string value
func ExtractCELExpression(value string) (string, bool) {
	start := strings.Index(value, "${")
	if start == -1 {
		return "", false
	}

	end := strings.Index(value[start:], "}")
	if end == -1 {
		return "", false
	}

	return value[start+2 : start+end], true
}

// ParseYAMLContent is a helper function to parse YAML content for testing
func ParseYAMLContent(content string) (*DocumentModel, error) {
	parser := NewYAMLParser()
	return parser.Parse(content)
}
