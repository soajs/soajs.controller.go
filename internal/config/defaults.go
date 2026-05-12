package config

// Default error codes
const (
	ErrCodeInvalidKey       = 150
	ErrCodeInvalidTenant    = 151
	ErrCodeInvalidProduct   = 152
	ErrCodeInvalidPackage   = 153
	ErrCodeUnauthorized     = 154
	ErrCodeForbidden        = 155
	ErrCodeServiceNotFound  = 156
	ErrCodeMaintenanceMode  = 157
	ErrCodeRateLimitExceeded = 158
	ErrCodeIPBanned         = 159
)

// Default error messages
var DefaultErrors = map[int]string{
	ErrCodeInvalidKey:        "Invalid API key",
	ErrCodeInvalidTenant:     "Invalid tenant",
	ErrCodeInvalidProduct:    "Invalid product",
	ErrCodeInvalidPackage:    "Invalid package",
	ErrCodeUnauthorized:      "Unauthorized",
	ErrCodeForbidden:         "Access forbidden",
	ErrCodeServiceNotFound:   "Service not found",
	ErrCodeMaintenanceMode:   "System is under maintenance",
	ErrCodeRateLimitExceeded: "Rate limit exceeded",
	ErrCodeIPBanned:          "IP address is banned",
}

// Header names
const (
	HeaderSOAJSKey         = "key"
	HeaderSOAJSAccessToken = "access_token"
	HeaderSOAJSTenant      = "soajsinjectobj"
	HeaderContentType      = "Content-Type"
	HeaderAuthorization    = "Authorization"
)

// Context keys
type contextKey string

const (
	CtxKeySOAJS      contextKey = "soajs"
	CtxKeyLogger     contextKey = "logger"
	CtxKeyRequestID  contextKey = "request_id"
	CtxKeyStartTime  contextKey = "start_time"
)
