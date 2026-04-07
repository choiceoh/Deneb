package ffi

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coreprotocol"
)

// ValidateFrame validates a gateway frame JSON string.
// Delegates to the pure-Go coreprotocol implementation.
func ValidateFrame(jsonStr string) error {
	return coreprotocol.ValidateFrame(jsonStr)
}

// knownErrorCodes contains all valid gateway error codes.
// Matches the ErrorCode enum in proto/gateway.proto.
var knownErrorCodes = map[string]struct{}{
	"NOT_LINKED": {}, "NOT_PAIRED": {}, "AGENT_TIMEOUT": {},
	"INVALID_REQUEST": {}, "UNAVAILABLE": {}, "MISSING_PARAM": {},
	"NOT_FOUND": {}, "UNAUTHORIZED": {}, "VALIDATION_FAILED": {},
	"CONFLICT": {}, "FORBIDDEN": {}, "NODE_DISCONNECTED": {},
	"DEPENDENCY_FAILED": {}, "FEATURE_DISABLED": {},
}

// ValidateErrorCode checks if an error code string is a known gateway error code.
func ValidateErrorCode(code string) bool {
	_, ok := knownErrorCodes[code]
	return ok
}

// ValidateParams validates RPC parameters for a given method name.
// Delegates to the pure-Go coreprotocol schema validators.
func ValidateParams(method, jsonStr string) (valid bool, errorsJSON []byte, err error) {
	result, err := coreprotocol.ValidateParams(method, jsonStr)
	if err != nil {
		return false, nil, err
	}
	if result.Valid {
		return true, nil, nil
	}
	// Serialize validation errors as JSON array for wire compatibility.
	data, jsonErr := json.Marshal(result.Errors)
	if jsonErr != nil {
		return false, nil, jsonErr
	}
	return false, data, nil
}
