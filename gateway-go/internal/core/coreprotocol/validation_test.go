package coreprotocol

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- ValidateParams dispatch tests ---

func TestValidateParams_UnknownMethod(t *testing.T) {
	_, err := ValidateParams("nonexistent.method", "{}")
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	vpe := &ValidateParamsError{}
	ok := errors.As(err, &vpe)
	if !ok || vpe.Kind != "unknown_method" {
		t.Fatalf("got %v, want unknown_method", err)
	}
}

func TestValidateParams_InvalidJSON(t *testing.T) {
	_, err := ValidateParams("sessions.list", "{not json}")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	vpe := &ValidateParamsError{}
	ok := errors.As(err, &vpe)
	if !ok || vpe.Kind != "invalid_json" {
		t.Fatalf("got %v, want invalid_json", err)
	}
}

func TestValidateParams_SessionsListEmpty(t *testing.T) {
	result := testutil.Must(ValidateParams("sessions.list", "{}"))
	if !result.Valid {
		t.Fatalf("got errors: %v, want valid", result.Errors)
	}
}

func TestValidateParams_AdditionalProperties(t *testing.T) {
	result := testutil.Must(ValidateParams("sessions.list", `{"unknownField": true}`))
	if result.Valid {
		t.Fatal("expected invalid for unknown properties")
	}
	if result.Errors[0].Keyword != "additionalProperties" {
		t.Fatalf("got %s, want additionalProperties", result.Errors[0].Keyword)
	}
}

func TestValidateParams_ResultSerialization(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{{
			Path: "/key", Message: "must be non-empty", Keyword: "minLength",
		}},
	}
	data := testutil.Must(json.Marshal(result))
	s := string(data)
	if !contains(s, `"valid":false`) || !contains(s, `"minLength"`) {
		t.Fatalf("unexpected JSON: %s", s)
	}
}

// --- Golden shape tests (matching Rust) ---

func TestValidateParams_GoldenShapes(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		params  string
		valid   bool
		keyword string // expected keyword if invalid
	}{
		{
			name:   "sessions.create minimal",
			method: "sessions.create",
			params: `{"key": "team-chat"}`,
			valid:  true,
		},
		{
			name:    "chat.send requires sessionKey",
			method:  "chat.send",
			params:  `{"message": "hello"}`,
			valid:   false,
			keyword: "required",
		},
		{
			name:   "cron.add full",
			method: "cron.add",
			params: `{"name":"every-five","enabled":true,"schedule":{"kind":"cron","expr":"*/5 * * * *"},"sessionTarget":"main","wakeMode":"now","payload":{"kind":"systemEvent","text":"tick"}}`,
			valid:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := testutil.Must(ValidateParams(tt.method, tt.params))
			if result.Valid != tt.valid {
				t.Fatalf("got valid=%v errors=%v, want valid=%v", result.Valid, result.Errors, tt.valid)
			}
			if tt.keyword != "" {
				found := false
				for _, e := range result.Errors {
					if e.Keyword == tt.keyword {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("got errors=%v, want keyword=%s", result.Errors, tt.keyword)
				}
			}
		})
	}
}

// --- Per-domain tests ---

func TestSessions_SendMissingRequired(t *testing.T) {
	result := testutil.Must(ValidateParams("sessions.send", `{}`))
	if result.Valid {
		t.Fatal("expected invalid")
	}
	hasKey, hasMsg := false, false
	for _, e := range result.Errors {
		if e.Keyword == "required" && contains(e.Path, "key") {
			hasKey = true
		}
		if e.Keyword == "required" && contains(e.Path, "message") {
			hasMsg = true
		}
	}
	if !hasKey || !hasMsg {
		t.Fatalf("got %v, want required errors for key and message", result.Errors)
	}
}


func TestSessions_PatchNullable(t *testing.T) {
	result, err := ValidateParams("sessions.patch",
		`{"key":"k","label":null,"fastMode":null,"responseUsage":null}`)
	testutil.NoError(t, err)
	if !result.Valid {
		t.Fatalf("null values should be accepted for nullable fields: %v", result.Errors)
	}
}

func TestSessions_PatchInvalidEnum(t *testing.T) {
	result, err := ValidateParams("sessions.patch",
		`{"key":"k","responseUsage":"invalid"}`)
	testutil.NoError(t, err)
	hasEnum := false
	for _, e := range result.Errors {
		if e.Keyword == "enum" {
			hasEnum = true
		}
	}
	if !hasEnum {
		t.Fatalf("got %v, want enum error", result.Errors)
	}
}





func TestCron_RemoveMissingID(t *testing.T) {
	result := testutil.Must(ValidateParams("cron.remove", `{}`))
	if result.Valid {
		t.Fatal("expected invalid for missing id/jobId")
	}
}





func TestLogsTail_LimitTooHigh(t *testing.T) {
	result := testutil.Must(ValidateParams("logs.tail", `{"limit":10000}`))
	hasMax := false
	for _, e := range result.Errors {
		if e.Keyword == "maximum" {
			hasMax = true
		}
	}
	if !hasMax {
		t.Fatalf("got %v, want maximum error", result.Errors)
	}
}







func TestSecrets_ResolveValid(t *testing.T) {
	result, err := ValidateParams("secrets.resolve",
		`{"commandName":"cmd","targetIds":["a","b"]}`)
	testutil.NoError(t, err)
	if !result.Valid {
		t.Fatalf("got %v, want valid", result.Errors)
	}
}

func TestSecrets_ResolveMissing(t *testing.T) {
	result := testutil.Must(ValidateParams("secrets.resolve", `{}`))
	if result.Valid {
		t.Fatal("expected invalid")
	}
	if len(result.Errors) < 2 {
		t.Fatalf("got %d, want at least 2 errors", len(result.Errors))
	}
}

// --- Validation helpers tests ---

func TestIsValidExecSecretRefID(t *testing.T) {
	valid := []string{"a", "A0", "cmd/run", "my.secret:v1/path-name"}
	for _, s := range valid {
		if !IsValidExecSecretRefID(s) {
			t.Errorf("expected valid: %q", s)
		}
	}
	invalid := []string{"", ".hidden", "a/../b", "a/./b", "a/b/..", "../a"}
	for _, s := range invalid {
		if IsValidExecSecretRefID(s) {
			t.Errorf("expected invalid: %q", s)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
