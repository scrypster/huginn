package connections

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorKind classifies a connection error for HTTP status mapping and programmatic checks.
type ErrorKind int

const (
	ErrKindNotFound          ErrorKind = iota // 404
	ErrKindAlreadyExists                      // 409
	ErrKindAuthFailed                         // 401
	ErrKindAuthExpired                        // 401 (token expired; distinct from auth denial for logging)
	ErrKindValidation                         // 400
	ErrKindStorageFailed                      // 500
	ErrKindExternalService                    // 502
	ErrKindResourceExhausted                  // 429
)

// ConnectionError is the canonical typed error for the connections package.
// All error returns from Store, SQLiteConnectionStore, and Manager use this type
// so callers can discriminate via errors.As without string matching.
type ConnectionError struct {
	Kind    ErrorKind
	Op      string // calling operation, e.g. "RemoveConnection", "GetHTTPClient"
	Message string // human-readable; safe to surface in API responses
	Cause   error  // underlying error; NOT surfaced to API callers
}

func (e *ConnectionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("connections.%s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("connections.%s: %s", e.Op, e.Message)
}

func (e *ConnectionError) Unwrap() error { return e.Cause }

// HTTPStatus maps the error kind to a canonical HTTP status code.
func (e *ConnectionError) HTTPStatus() int {
	return KindToHTTPStatus(e.Kind)
}

// KindToHTTPStatus maps an ErrorKind to a canonical HTTP status code.
func KindToHTTPStatus(kind ErrorKind) int {
	switch kind {
	case ErrKindNotFound:
		return http.StatusNotFound
	case ErrKindAlreadyExists:
		return http.StatusConflict
	case ErrKindAuthFailed, ErrKindAuthExpired:
		return http.StatusUnauthorized
	case ErrKindValidation:
		return http.StatusBadRequest
	case ErrKindExternalService:
		return http.StatusBadGateway
	case ErrKindResourceExhausted:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// IsNotFound returns true when err is a *ConnectionError with Kind == ErrKindNotFound.
func IsNotFound(err error) bool {
	var ce *ConnectionError
	return errors.As(err, &ce) && ce.Kind == ErrKindNotFound
}

// IsAlreadyExists returns true when err is a *ConnectionError with Kind == ErrKindAlreadyExists.
func IsAlreadyExists(err error) bool {
	var ce *ConnectionError
	return errors.As(err, &ce) && ce.Kind == ErrKindAlreadyExists
}

// Package-private constructors — external packages discriminate via errors.As and IsNotFound/IsAlreadyExists.
func errNotFound(op, msg string) error {
	return &ConnectionError{Kind: ErrKindNotFound, Op: op, Message: msg}
}

func errAlreadyExists(op, msg string) error {
	return &ConnectionError{Kind: ErrKindAlreadyExists, Op: op, Message: msg}
}

func errAuthFailed(op, msg string, cause error) error {
	return &ConnectionError{Kind: ErrKindAuthFailed, Op: op, Message: msg, Cause: cause}
}

func errAuthExpired(op, msg string) error {
	return &ConnectionError{Kind: ErrKindAuthExpired, Op: op, Message: msg}
}

func errValidation(op, msg string) error {
	return &ConnectionError{Kind: ErrKindValidation, Op: op, Message: msg}
}

func errStorage(op, msg string, cause error) error {
	return &ConnectionError{Kind: ErrKindStorageFailed, Op: op, Message: msg, Cause: cause}
}

func errExternal(op, msg string, cause error) error {
	return &ConnectionError{Kind: ErrKindExternalService, Op: op, Message: msg, Cause: cause}
}

func errResourceExhausted(op, msg string) error {
	return &ConnectionError{Kind: ErrKindResourceExhausted, Op: op, Message: msg}
}

// keep errAuthExpired available for token-expiry paths
var _ = errAuthExpired
