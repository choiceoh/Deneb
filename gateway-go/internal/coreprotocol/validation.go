package coreprotocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidationError is a single validation error with AJV-compatible fields.
type ValidationError struct {
	// Path is the JSON pointer path to the offending field (e.g. "/key", "/options/2").
	Path string `json:"path"`
	// Message is a human-readable error description.
	Message string `json:"message"`
	// Keyword is an AJV-compatible keyword (e.g. "required", "minLength").
	Keyword string `json:"keyword"`
}

// ValidationResult holds the outcome of validating RPC parameters.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// ValidateParamsError is returned when parameter validation cannot proceed.
type ValidateParamsError struct {
	Kind    string // "invalid_json" or "unknown_method"
	Message string
}

func (e *ValidateParamsError) Error() string { return e.Message }

// ValidatorFn is the signature for per-method schema validators.
type ValidatorFn func(value any, path string, errors *[]ValidationError)

// --- Core validation helpers ---

// RequireObject checks that value is a JSON object (map).
func RequireObject(value any, path string, errors *[]ValidationError) bool {
	if _, ok := value.(map[string]any); ok {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path: path, Message: "must be object", Keyword: "type",
	})
	return false
}

// CheckRequired checks that a required field exists on an object.
func CheckRequired(obj map[string]any, field, parentPath string, errors *[]ValidationError) bool {
	if _, ok := obj[field]; ok {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path:    parentPath + "/" + field,
		Message: fmt.Sprintf("must have required property '%s'", field),
		Keyword: "required",
	})
	return false
}

// CheckString checks that a value is a string.
func CheckString(value any, path string, errors *[]ValidationError) bool {
	if _, ok := value.(string); ok {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path: path, Message: "must be string", Keyword: "type",
	})
	return false
}

// CheckNonEmptyString checks that a value is a non-empty string.
func CheckNonEmptyString(value any, path string, errors *[]ValidationError) bool {
	s, ok := value.(string)
	if !ok {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must be string", Keyword: "type",
		})
		return false
	}
	if s == "" {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must NOT have fewer than 1 characters", Keyword: "minLength",
		})
		return false
	}
	return true
}

// CheckMaxLength checks that a string is at most maxLen characters.
func CheckMaxLength(value any, path string, maxLen int, errors *[]ValidationError) {
	s, ok := value.(string)
	if !ok {
		return
	}
	if utf8.RuneCountInString(s) > maxLen {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must NOT have more than %d characters", maxLen),
			Keyword: "maxLength",
		})
	}
}

// CheckPattern checks that a string matches a compiled regex pattern.
func CheckPattern(value any, path string, pattern *regexp.Regexp, errors *[]ValidationError) {
	s, ok := value.(string)
	if !ok {
		return
	}
	if !pattern.MatchString(s) {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must match pattern \"%s\"", pattern.String()),
			Keyword: "pattern",
		})
	}
}

// CheckBoolean checks that a value is a boolean.
func CheckBoolean(value any, path string, errors *[]ValidationError) bool {
	if _, ok := value.(bool); ok {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path: path, Message: "must be boolean", Keyword: "type",
	})
	return false
}

// CheckInteger checks that a value is an integer within the given range.
// JSON numbers are decoded as float64 by encoding/json; we check it's whole.
func CheckInteger(value any, path string, minimum, maximum *int64, errors *[]ValidationError) bool {
	f, ok := value.(float64)
	if !ok {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must be integer", Keyword: "type",
		})
		return false
	}
	n := int64(f)
	if f != float64(n) {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must be integer", Keyword: "type",
		})
		return false
	}
	if minimum != nil && n < *minimum {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must be >= %d", *minimum),
			Keyword: "minimum",
		})
		return false
	}
	if maximum != nil && n > *maximum {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must be <= %d", *maximum),
			Keyword: "maximum",
		})
		return false
	}
	return true
}

// CheckArray checks that a value is a JSON array (slice).
func CheckArray(value any, path string, errors *[]ValidationError) bool {
	if _, ok := value.([]any); ok {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path: path, Message: "must be array", Keyword: "type",
	})
	return false
}

// CheckMinItems checks array minimum item count.
func CheckMinItems(value any, path string, minItems int, errors *[]ValidationError) {
	arr, ok := value.([]any)
	if !ok {
		return
	}
	if len(arr) < minItems {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must NOT have fewer than %d items", minItems),
			Keyword: "minItems",
		})
	}
}

// CheckStringEnum checks that a string is one of the allowed enum values.
func CheckStringEnum(value any, path string, allowed []string, errors *[]ValidationError) bool {
	s, ok := value.(string)
	if !ok {
		*errors = append(*errors, ValidationError{
			Path: path, Message: "must be string", Keyword: "type",
		})
		return false
	}
	for _, a := range allowed {
		if s == a {
			return true
		}
	}
	*errors = append(*errors, ValidationError{
		Path:    path,
		Message: fmt.Sprintf("must be equal to one of the allowed values: %v, got \"%s\"", allowed, s),
		Keyword: "enum",
	})
	return false
}

// CheckLiteral checks that a value is a specific literal string.
func CheckLiteral(value any, path string, expected string, errors *[]ValidationError) bool {
	s, ok := value.(string)
	if ok && s == expected {
		return true
	}
	*errors = append(*errors, ValidationError{
		Path:    path,
		Message: fmt.Sprintf("must be equal to constant \"%s\"", expected),
		Keyword: "const",
	})
	return false
}

// IsNull checks if a value is nil (JSON null).
func IsNull(value any) bool {
	return value == nil
}

// CheckNoAdditionalProperties ensures an object has no properties outside the allowed set.
func CheckNoAdditionalProperties(obj map[string]any, allowed []string, parentPath string, errors *[]ValidationError) {
	for key := range obj {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
				break
			}
		}
		if !found {
			*errors = append(*errors, ValidationError{
				Path:    parentPath,
				Message: fmt.Sprintf("must NOT have additional properties: '%s'", key),
				Keyword: "additionalProperties",
			})
		}
	}
}

// CheckOptional validates an optional field: if present, run the checker.
func CheckOptional(obj map[string]any, field, parentPath string, errors *[]ValidationError, checker ValidatorFn) {
	if value, ok := obj[field]; ok {
		path := parentPath + "/" + field
		checker(value, path, errors)
	}
}

// CheckOptionalNullable validates an optional-nullable field.
// If present and not null, run the checker. Allows absent or JSON null.
func CheckOptionalNullable(obj map[string]any, field, parentPath string, errors *[]ValidationError, checker ValidatorFn) {
	if value, ok := obj[field]; ok {
		if !IsNull(value) {
			path := parentPath + "/" + field
			checker(value, path, errors)
		}
	}
}

// --- Array helpers ---

// CheckNonEmptyStringArray checks that a value is an array of non-empty strings.
func CheckNonEmptyStringArray(value any, path string, errors *[]ValidationError) {
	if CheckArray(value, path, errors) {
		arr := value.([]any)
		for i, item := range arr {
			CheckNonEmptyString(item, fmt.Sprintf("%s/%d", path, i), errors)
		}
	}
}

// CheckStringArray checks that a value is an array of strings.
func CheckStringArray(value any, path string, errors *[]ValidationError) {
	if CheckArray(value, path, errors) {
		arr := value.([]any)
		for i, item := range arr {
			CheckString(item, fmt.Sprintf("%s/%d", path, i), errors)
		}
	}
}

// CheckNonEmptyStringArrayMin1 checks non-empty array (minItems: 1) of non-empty strings.
func CheckNonEmptyStringArrayMin1(value any, path string, errors *[]ValidationError) {
	if CheckArray(value, path, errors) {
		CheckMinItems(value, path, 1, errors)
		arr := value.([]any)
		for i, item := range arr {
			CheckNonEmptyString(item, fmt.Sprintf("%s/%d", path, i), errors)
		}
	}
}

// --- Special validators ---

// IsValidExecSecretRefID validates an exec secret ref ID without regex.
// First char must be ASCII alphanumeric, remaining chars [A-Za-z0-9._:/-],
// total length 1-256, no "." or ".." path segments.
func IsValidExecSecretRefID(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	if !isASCIIAlphanumeric(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		b := s[i]
		if !(isASCIIAlphanumeric(b) || b == '.' || b == '_' || b == ':' || b == '/' || b == '-') {
			return false
		}
	}
	for _, segment := range strings.Split(s, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func isASCIIAlphanumeric(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// CheckExecSecretRefID checks that a string is a valid exec secret ref ID.
func CheckExecSecretRefID(value any, path string, errors *[]ValidationError) {
	s, ok := value.(string)
	if !ok {
		return
	}
	if !IsValidExecSecretRefID(s) {
		*errors = append(*errors, ValidationError{
			Path:    path,
			Message: fmt.Sprintf("must match pattern \"%s\"", ExecSecretRefIDPattern),
			Keyword: "pattern",
		})
	}
}

// --- Params validation entry point ---

// ValidateParams validates RPC parameters for a given method name.
// Returns (result, nil) on success, or (nil, error) for invalid JSON or unknown method.
func ValidateParams(method, jsonStr string) (*ValidationResult, error) {
	var value any
	if err := json.Unmarshal([]byte(jsonStr), &value); err != nil {
		return nil, &ValidateParamsError{Kind: "invalid_json", Message: "invalid JSON: " + err.Error()}
	}

	validator := lookupValidator(method)
	if validator == nil {
		return nil, &ValidateParamsError{Kind: "unknown_method", Message: "unknown method: " + method}
	}

	var errs []ValidationError
	validator(value, "", &errs)
	if len(errs) == 0 {
		return &ValidationResult{Valid: true}, nil
	}
	return &ValidationResult{Valid: false, Errors: errs}, nil
}

// --- Integer pointer helpers for min/max ---

func intPtr(v int64) *int64 { return &v }
