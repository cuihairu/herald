package template

import "fmt"

// Template validation errors
var (
	ErrInvalidTemplateID   = fmt.Errorf("template id cannot be empty")
	ErrInvalidTemplateName = fmt.Errorf("template name cannot be empty")
	ErrInvalidTemplateTitle = fmt.Errorf("template title cannot be empty")
	ErrInvalidFieldLabel   = fmt.Errorf("field label cannot be empty")
	ErrInvalidFieldValue   = fmt.Errorf("field value cannot be empty")
	ErrTemplateNotFound    = fmt.Errorf("template not found")
)

// FieldError represents a field validation error
type FieldError struct {
	Index int
	Err   error
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("field at index %d: %s", e.Index, e.Err)
}

func (e *FieldError) Unwrap() error {
	return e.Err
}
