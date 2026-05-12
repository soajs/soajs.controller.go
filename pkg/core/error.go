package core

import "fmt"

// SOAJSError represents a SOAJS error
type SOAJSError struct {
	Code    int
	Message string
}

func (e *SOAJSError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// NewError creates a new SOAJS error
func NewError(code int, message string) *SOAJSError {
	return &SOAJSError{
		Code:    code,
		Message: message,
	}
}

// Common error codes
const (
	ErrCodeGeneric          = 100
	ErrCodeValidation       = 101
	ErrCodeNotFound         = 102
	ErrCodeDuplicate        = 103
	ErrCodeDatabase         = 104
	ErrCodeUnauthorized     = 150
	ErrCodeForbidden        = 151
	ErrCodeInvalidKey       = 152
	ErrCodeInvalidTenant    = 153
	ErrCodeServiceNotFound  = 154
	ErrCodeRateLimited      = 155
	ErrCodeMaintenanceMode  = 156
)

// Common errors
var (
	ErrNotFound         = NewError(ErrCodeNotFound, "Resource not found")
	ErrUnauthorized     = NewError(ErrCodeUnauthorized, "Unauthorized")
	ErrForbidden        = NewError(ErrCodeForbidden, "Access forbidden")
	ErrInvalidKey       = NewError(ErrCodeInvalidKey, "Invalid API key")
	ErrInvalidTenant    = NewError(ErrCodeInvalidTenant, "Invalid tenant")
	ErrServiceNotFound  = NewError(ErrCodeServiceNotFound, "Service not found")
	ErrRateLimited      = NewError(ErrCodeRateLimited, "Rate limit exceeded")
	ErrMaintenanceMode  = NewError(ErrCodeMaintenanceMode, "System under maintenance")
)
