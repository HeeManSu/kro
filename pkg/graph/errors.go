package graph

import "fmt"

// MultiError aggregates multiple errors into a single error value.
// This is useful for validations where we want to surface all issues at once.
type MultiError struct {
	Errors []error
}

func (e *MultiError) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return "no errors"
	}
	return fmt.Sprintf("%d error(s); first: %v", len(e.Errors), e.Errors[0])
}
