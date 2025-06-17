package parser

import (
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Position represents a position in a text document
type Position struct {
	Line      int
	Character int
}

// Range represents a range in a text document
type Range struct {
	Start Position
	End   Position
}

// ToProtocolPosition converts our Position to LSP protocol Position
func (p Position) ToProtocolPosition() protocol.Position {
	return protocol.Position{
		Line:      uint32(p.Line),
		Character: uint32(p.Character),
	}
}

// ToProtocolRange converts our Range to LSP protocol Range
func (r Range) ToProtocolRange() protocol.Range {
	return protocol.Range{
		Start: r.Start.ToProtocolPosition(),
		End:   r.End.ToProtocolPosition(),
	}
}

// PositionFromProtocol converts from LSP protocol Position to our Position
func PositionFromProtocol(p protocol.Position) Position {
	return Position{
		Line:      int(p.Line),
		Character: int(p.Character),
	}
}

// RangeFromProtocol converts from LSP protocol Range to our Range
func RangeFromProtocol(r protocol.Range) Range {
	return Range{
		Start: PositionFromProtocol(r.Start),
		End:   PositionFromProtocol(r.End),
	}
}

// OffsetToPosition converts a byte offset to a Position in the document
func OffsetToPosition(content string, offset int) Position {
	if offset < 0 {
		return Position{Line: 0, Character: 0}
	}

	lines := strings.Split(content, "\n")
	currentOffset := 0

	for lineNum, line := range lines {
		lineLength := len(line) + 1 // +1 for newline

		if currentOffset+lineLength > offset {
			// The offset is within this line
			charOffset := offset - currentOffset
			return Position{
				Line:      lineNum,
				Character: charOffset,
			}
		}

		currentOffset += lineLength
	}

	// If we get here, offset is beyond the end of the document
	lastLine := max(len(lines)-1, 0)
	return Position{
		Line:      lastLine,
		Character: len(lines[lastLine]),
	}
}

// PositionToOffset converts a Position to a byte offset in the document
func PositionToOffset(content string, pos Position) int {
	lines := strings.Split(content, "\n")

	if pos.Line >= len(lines) {
		return len(content)
	}

	offset := 0
	for i := range pos.Line {
		offset += len(lines[i]) + 1 // +1 for newline
	}

	// Add the character offset within the line
	if pos.Character > len(lines[pos.Line]) {
		offset += len(lines[pos.Line])
	} else {
		offset += pos.Character
	}

	return offset
}

// IsPositionInRange checks if a position is within a range
func IsPositionInRange(pos Position, r Range) bool {
	// Check if position is after start
	if pos.Line < r.Start.Line || (pos.Line == r.Start.Line && pos.Character < r.Start.Character) {
		return false
	}

	// Check if position is before end
	if pos.Line > r.End.Line || (pos.Line == r.End.Line && pos.Character > r.End.Character) {
		return false
	}

	return true
}

// ExtendRange extends a range to include another range
func ExtendRange(r1, r2 Range) Range {
	start := r1.Start
	if r2.Start.Line < r1.Start.Line || (r2.Start.Line == r1.Start.Line && r2.Start.Character < r1.Start.Character) {
		start = r2.Start
	}

	end := r1.End
	if r2.End.Line > r1.End.Line || (r2.End.Line == r1.End.Line && r2.End.Character > r1.End.Character) {
		end = r2.End
	}

	return Range{Start: start, End: end}
}
