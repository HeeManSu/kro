package parser

import "fmt"

// Position represents a position in the document
type Position struct {
	Line   int // 1-based line number
	Column int // 0-based column number
}

// Range represents a range in the document
type Range struct {
	Start Position // Start position (inclusive)
	End   Position // End position (exclusive)
}

// string representation of the position
func (p Position) String() string {
	return fmt.Sprintf("Line %d, Column %d", p.Line, p.Column)
}

// string representation of the range
func (r Range) String() string {
	return fmt.Sprintf("Start: %s, End: %s", r.Start.String(), r.End.String())
}

// checks if a position is within this range
func (r Range) Contains(pos Position) bool {

	if pos.Line < r.Start.Line || pos.Line > r.End.Line {
		return false
	}

	if pos.Line == r.Start.Line && pos.Column < r.Start.Column {
		return false
	}

	if pos.Line == r.End.Line && pos.Column > r.End.Column {
		return false
	}

	return true

}

// checks if the position is valid (positive line and column)
func (p Position) IsValid() bool {
	return p.Line > 0 && p.Column >= 0
}

// checks if the range is valid (start and end positions are valid)
func (r Range) IsValid() bool {
	return r.Start.IsValid() && r.End.IsValid() && (r.Start.Line < r.End.Line || (r.Start.Line == r.End.Line && r.Start.Column <= r.End.Column))
}

// converts a byte offset to a line/column position
func PositionFromOffset(content string, offset int) Position {
	if offset >= len(content) {
		offset = len(content) - 1
	}

	line := 1
	column := 1

	for i := 0; i < offset && i < len(content); i++ {
		if content[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return Position{Line: line, Column: column}
}

// converts a line/column position to a byte offset
func OffsetFromPosition(content string, pos Position) int {
	currentLine := 1
	currentColumn := 1

	for i := range content {
		if currentLine == pos.Line && currentColumn == pos.Column {
			return i
		}

		if content[i] == '\n' {
			currentLine++
			currentColumn = 1
		} else {
			currentColumn++
		}
	}

	return len(content)
}
