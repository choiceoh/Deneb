package plugin

import (
	"testing"
)

func TestValidateConfig_NilSchema(t *testing.T) {
	errs := ValidateConfig(nil, map[string]any{"key": "value"})
	if errs != nil {
		t.Errorf("expected nil errors for nil schema, got %v", errs)
	}
}

func TestValidateConfig_RequiredFields(t *testing.T) {
	schema := &ConfigSchema{
		Required: []string{"apiKey", "region"},
		Properties: map[string]ConfigProperty{
			"apiKey": {Type: "string"},
			"region": {Type: "string"},
		},
	}

	// Missing both.
	errs := ValidateConfig(schema, map[string]any{})
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errs))
	}

	// Missing one.
	errs = ValidateConfig(schema, map[string]any{"apiKey": "key"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs[0].Field != "region" {
		t.Errorf("expected error on 'region', got %q", errs[0].Field)
	}

	// All present.
	errs = ValidateConfig(schema, map[string]any{"apiKey": "key", "region": "us-east-1"})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}
}

func TestValidateConfig_StringType(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"name": {Type: "string"},
		},
	}

	// Valid.
	errs := ValidateConfig(schema, map[string]any{"name": "hello"})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}

	// Wrong type.
	errs = ValidateConfig(schema, map[string]any{"name": 42})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_StringEnum(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"color": {Type: "string", Enum: []string{"red", "blue", "green"}},
		},
	}

	// Valid enum value.
	errs := ValidateConfig(schema, map[string]any{"color": "red"})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}

	// Invalid enum value.
	errs = ValidateConfig(schema, map[string]any{"color": "purple"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_NumberType(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"count": {Type: "number"},
		},
	}

	// float64 (JSON default).
	errs := ValidateConfig(schema, map[string]any{"count": float64(42)})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for float64, got %d", len(errs))
	}

	// int.
	errs = ValidateConfig(schema, map[string]any{"count": 42})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for int, got %d", len(errs))
	}

	// int64.
	errs = ValidateConfig(schema, map[string]any{"count": int64(42)})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for int64, got %d", len(errs))
	}

	// Wrong type.
	errs = ValidateConfig(schema, map[string]any{"count": "not a number"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_BooleanType(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"enabled": {Type: "boolean"},
		},
	}

	errs := ValidateConfig(schema, map[string]any{"enabled": true})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}

	errs = ValidateConfig(schema, map[string]any{"enabled": "true"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_ArrayType(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"tags": {Type: "array"},
		},
	}

	errs := ValidateConfig(schema, map[string]any{"tags": []any{"a", "b"}})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}

	errs = ValidateConfig(schema, map[string]any{"tags": "not array"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_ObjectType(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"settings": {Type: "object"},
		},
	}

	errs := ValidateConfig(schema, map[string]any{"settings": map[string]any{"key": "val"}})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}

	errs = ValidateConfig(schema, map[string]any{"settings": "not object"})
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestValidateConfig_UnknownPropertiesIgnored(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"name": {Type: "string"},
		},
	}

	// Extra property not in schema — should not cause error.
	errs := ValidateConfig(schema, map[string]any{"name": "test", "extra": 42})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors (extra props ignored), got %d", len(errs))
	}
}

func TestValidateConfig_MissingOptionalField(t *testing.T) {
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"optional": {Type: "string"},
		},
		// "optional" is not in Required.
	}

	errs := ValidateConfig(schema, map[string]any{})
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for missing optional field, got %d", len(errs))
	}
}

func TestAllowedConfigKeys(t *testing.T) {
	// Nil schema.
	keys := AllowedConfigKeys(nil)
	if keys != nil {
		t.Errorf("expected nil for nil schema, got %v", keys)
	}

	// With properties.
	schema := &ConfigSchema{
		Properties: map[string]ConfigProperty{
			"name":   {Type: "string"},
			"count":  {Type: "number"},
			"active": {Type: "boolean"},
		},
	}
	keys = AllowedConfigKeys(schema)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	for _, k := range []string{"name", "count", "active"} {
		if !keys[k] {
			t.Errorf("expected key %q in allowed keys", k)
		}
	}
}
