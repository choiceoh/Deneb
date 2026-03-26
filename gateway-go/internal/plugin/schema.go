package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConfigSchema describes a plugin's configuration schema.
type ConfigSchema struct {
	Properties map[string]ConfigProperty `json:"properties,omitempty"`
	Required   []string                  `json:"required,omitempty"`
}

// ConfigProperty describes a single configuration field.
type ConfigProperty struct {
	Type        string          `json:"type"` // "string", "number", "boolean", "array", "object"
	Description string          `json:"description,omitempty"`
	Default     json.RawMessage `json:"default,omitempty"`
	Enum        []string        `json:"enum,omitempty"`
	Sensitive   bool            `json:"sensitive,omitempty"`
	Advanced    bool            `json:"advanced,omitempty"`
}

// ValidateConfig checks if a config object matches the schema.
func ValidateConfig(schema *ConfigSchema, config map[string]any) []ConfigValidationError {
	if schema == nil {
		return nil
	}

	var errors []ConfigValidationError

	// Check required fields.
	for _, field := range schema.Required {
		if _, ok := config[field]; !ok {
			errors = append(errors, ConfigValidationError{
				Field:   field,
				Message: fmt.Sprintf("required field %q is missing", field),
			})
		}
	}

	// Check property types.
	for name, prop := range schema.Properties {
		value, exists := config[name]
		if !exists {
			continue
		}

		if err := validatePropertyType(name, prop, value); err != nil {
			errors = append(errors, *err)
		}
	}

	return errors
}

// ConfigValidationError describes a single validation failure.
type ConfigValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func validatePropertyType(name string, prop ConfigProperty, value any) *ConfigValidationError {
	switch prop.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return &ConfigValidationError{Field: name, Message: fmt.Sprintf("%q must be a string", name)}
		}
		if len(prop.Enum) > 0 {
			s := value.(string)
			found := false
			for _, e := range prop.Enum {
				if e == s {
					found = true
					break
				}
			}
			if !found {
				return &ConfigValidationError{
					Field:   name,
					Message: fmt.Sprintf("%q must be one of: %s", name, strings.Join(prop.Enum, ", ")),
				}
			}
		}
	case "number":
		switch value.(type) {
		case float64, int, int64:
			// ok
		default:
			return &ConfigValidationError{Field: name, Message: fmt.Sprintf("%q must be a number", name)}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return &ConfigValidationError{Field: name, Message: fmt.Sprintf("%q must be a boolean", name)}
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return &ConfigValidationError{Field: name, Message: fmt.Sprintf("%q must be an array", name)}
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return &ConfigValidationError{Field: name, Message: fmt.Sprintf("%q must be an object", name)}
		}
	}
	return nil
}

// AllowedConfigKeys returns the set of valid config keys from a schema.
func AllowedConfigKeys(schema *ConfigSchema) map[string]bool {
	if schema == nil {
		return nil
	}
	keys := make(map[string]bool, len(schema.Properties))
	for k := range schema.Properties {
		keys[k] = true
	}
	return keys
}
