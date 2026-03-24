package protocol

// Error code constants matching src/gateway/protocol/schema/error-codes.ts.
const (
	ErrNotLinked        = "NOT_LINKED"
	ErrNotPaired        = "NOT_PAIRED"
	ErrAgentTimeout     = "AGENT_TIMEOUT"
	ErrInvalidRequest   = "INVALID_REQUEST"
	ErrUnavailable      = "UNAVAILABLE"
	ErrMissingParam     = "MISSING_PARAM"
	ErrNotFound         = "NOT_FOUND"
	ErrUnauthorized     = "UNAUTHORIZED"
	ErrValidationFailed = "VALIDATION_FAILED"
	ErrConflict         = "CONFLICT"
	ErrForbidden        = "FORBIDDEN"
	ErrNodeDisconnected = "NODE_DISCONNECTED"
	ErrDependencyFailed = "DEPENDENCY_FAILED"
	ErrFeatureDisabled  = "FEATURE_DISABLED"
)
