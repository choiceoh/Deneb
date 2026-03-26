// Package rpcerr provides structured RPC error types with debugging context.
//
// Instead of flat protocol.NewError(code, message) calls, handlers can build
// errors with contextual fields (session key, model, channel, etc.) that are
// preserved for logging and optionally serialized into the wire ErrorShape.Details.
package rpcerr

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Error is a structured RPC error that carries debugging context alongside
// the standard error code and message. Use New() to create, chain With*
// methods to attach context, then call Response() to produce a ResponseFrame.
type Error struct {
	Code    string
	Message string
	Context map[string]any
}

// New creates a structured RPC error with the given code and message.
func New(code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Newf creates a structured RPC error with a formatted message.
func Newf(code, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap creates a structured error from a Go error, using the given code.
func Wrap(code string, err error) *Error {
	return &Error{
		Code:    code,
		Message: err.Error(),
	}
}

// WithSession attaches a session key to the error context.
func (e *Error) WithSession(key string) *Error {
	return e.with("sessionKey", key)
}

// WithModel attaches a model identifier to the error context.
func (e *Error) WithModel(model string) *Error {
	return e.with("model", model)
}

// WithChannel attaches a channel ID to the error context.
func (e *Error) WithChannel(id string) *Error {
	return e.with("channel", id)
}

// WithMethod attaches the RPC method name to the error context.
func (e *Error) WithMethod(method string) *Error {
	return e.with("method", method)
}

// WithAgent attaches an agent ID to the error context.
func (e *Error) WithAgent(id string) *Error {
	return e.with("agent", id)
}

// With attaches an arbitrary key-value pair to the error context.
func (e *Error) With(key string, value any) *Error {
	return e.with(key, value)
}

func (e *Error) with(key string, value any) *Error {
	if e.Context == nil {
		e.Context = make(map[string]any, 4)
	}
	e.Context[key] = value
	return e
}

// Error implements the error interface.
func (e *Error) Error() string {
	if len(e.Context) == 0 {
		return fmt.Sprintf("[%s] %s", e.Code, e.Message)
	}
	return fmt.Sprintf("[%s] %s %v", e.Code, e.Message, e.Context)
}

// ToShape converts to a protocol ErrorShape, encoding context into Details.
func (e *Error) ToShape() *protocol.ErrorShape {
	shape := protocol.NewError(e.Code, e.Message)
	if len(e.Context) > 0 {
		if b, err := json.Marshal(e.Context); err == nil {
			shape.Details = b
		}
	}
	return shape
}

// Response produces a complete error ResponseFrame for the given request ID.
func (e *Error) Response(reqID string) *protocol.ResponseFrame {
	return protocol.NewResponseError(reqID, e.ToShape())
}

// LogAttrs returns the context as a flat slice of key-value pairs suitable
// for slog structured logging: slog.Error("rpc error", rpcerr.LogAttrs()...)
func (e *Error) LogAttrs() []any {
	attrs := make([]any, 0, 2+len(e.Context)*2)
	attrs = append(attrs, "code", e.Code, "message", e.Message)
	for k, v := range e.Context {
		attrs = append(attrs, k, v)
	}
	return attrs
}

// --- Convenience constructors for common error patterns ---

// MissingParam returns a MISSING_PARAM error for the given parameter name.
func MissingParam(param string) *Error {
	return New(protocol.ErrMissingParam, param+" is required")
}

// InvalidParams returns an INVALID_REQUEST error for malformed parameters.
func InvalidParams(err error) *Error {
	return New(protocol.ErrInvalidRequest, "invalid params: "+err.Error())
}

// NotFound returns a NOT_FOUND error for the given resource description.
func NotFound(resource string) *Error {
	return New(protocol.ErrNotFound, resource+" not found")
}

// Unavailable returns an UNAVAILABLE error.
func Unavailable(msg string) *Error {
	return New(protocol.ErrUnavailable, msg)
}

// Conflict returns a CONFLICT error.
func Conflict(msg string) *Error {
	return New(protocol.ErrConflict, msg)
}

// FeatureDisabled returns a FEATURE_DISABLED error.
func FeatureDisabled(feature string) *Error {
	return New(protocol.ErrFeatureDisabled, feature+" is disabled")
}
