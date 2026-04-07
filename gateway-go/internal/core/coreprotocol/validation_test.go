package coreprotocol

import (
	"encoding/json"
	"errors"
	"testing"
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
		t.Fatalf("expected unknown_method, got %v", err)
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
		t.Fatalf("expected invalid_json, got %v", err)
	}
}

func TestValidateParams_SessionsListEmpty(t *testing.T) {
	result, err := ValidateParams("sessions.list", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
}

func TestValidateParams_AdditionalProperties(t *testing.T) {
	result, err := ValidateParams("sessions.list", `{"unknownField": true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Fatal("expected invalid for unknown properties")
	}
	if result.Errors[0].Keyword != "additionalProperties" {
		t.Fatalf("expected additionalProperties, got %s", result.Errors[0].Keyword)
	}
}

func TestValidateParams_ResultSerialization(t *testing.T) {
	result := &ValidationResult{
		Valid: false,
		Errors: []ValidationError{{
			Path: "/key", Message: "must be non-empty", Keyword: "minLength",
		}},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
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
			result, err := ValidateParams(tt.method, tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Valid != tt.valid {
				t.Fatalf("expected valid=%v, got valid=%v errors=%v", tt.valid, result.Valid, result.Errors)
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
					t.Fatalf("expected keyword=%s, got errors=%v", tt.keyword, result.Errors)
				}
			}
		})
	}
}

// --- Per-domain tests ---

func TestSessions_SendMissingRequired(t *testing.T) {
	result, err := ValidateParams("sessions.send", `{}`)
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("expected required errors for key and message, got %v", result.Errors)
	}
}

func TestSessions_SendValid(t *testing.T) {
	result, err := ValidateParams("sessions.send", `{"key":"sess-1","message":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestSessions_PatchNullable(t *testing.T) {
	result, err := ValidateParams("sessions.patch",
		`{"key":"k","label":null,"fastMode":null,"responseUsage":null}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("null values should be accepted for nullable fields: %v", result.Errors)
	}
}

func TestSessions_PatchInvalidEnum(t *testing.T) {
	result, err := ValidateParams("sessions.patch",
		`{"key":"k","responseUsage":"invalid"}`)
	if err != nil {
		t.Fatal(err)
	}
	hasEnum := false
	for _, e := range result.Errors {
		if e.Keyword == "enum" {
			hasEnum = true
		}
	}
	if !hasEnum {
		t.Fatalf("expected enum error, got %v", result.Errors)
	}
}

func TestSessions_UsageDatePattern(t *testing.T) {
	result, err := ValidateParams("sessions.usage", `{"startDate":"2024-01-15"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid date, got %v", result.Errors)
	}

	result, err = ValidateParams("sessions.usage", `{"startDate":"not-a-date"}`)
	if err != nil {
		t.Fatal(err)
	}
	hasPattern := false
	for _, e := range result.Errors {
		if e.Keyword == "pattern" {
			hasPattern = true
		}
	}
	if !hasPattern {
		t.Fatalf("expected pattern error for bad date, got %v", result.Errors)
	}
}

func TestAgent_SendValid(t *testing.T) {
	result, err := ValidateParams("agent.send",
		`{"to":"user1","message":"hi","idempotencyKey":"k1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestAgent_WakeInvalidMode(t *testing.T) {
	result, err := ValidateParams("agent.wake",
		`{"mode":"invalid","text":"t"}`)
	if err != nil {
		t.Fatal(err)
	}
	hasEnum := false
	for _, e := range result.Errors {
		if e.Keyword == "enum" {
			hasEnum = true
		}
	}
	if !hasEnum {
		t.Fatalf("expected enum error, got %v", result.Errors)
	}
}

func TestCron_ListValid(t *testing.T) {
	result, err := ValidateParams("cron.list", `{"limit":50,"sortBy":"name"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestCron_AddValid(t *testing.T) {
	result, err := ValidateParams("cron.add",
		`{"name":"daily-check","schedule":{"kind":"cron","expr":"0 9 * * *"},"sessionTarget":"main","wakeMode":"now","payload":{"kind":"systemEvent","text":"check"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestCron_RemoveValid(t *testing.T) {
	result, err := ValidateParams("cron.remove", `{"id":"job-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestCron_RemoveWithJobId(t *testing.T) {
	result, err := ValidateParams("cron.remove", `{"jobId":"job-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestCron_RemoveMissingID(t *testing.T) {
	result, err := ValidateParams("cron.remove", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected invalid for missing id/jobId")
	}
}

func TestCron_SessionTargetCustom(t *testing.T) {
	result, err := ValidateParams("cron.add",
		`{"name":"t","schedule":{"kind":"at","at":"2024-01-01"},"sessionTarget":"session:my-key","wakeMode":"now","payload":{"kind":"systemEvent","text":"t"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestConfig_GetValid(t *testing.T) {
	result, err := ValidateParams("config.get", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestConfig_SetValid(t *testing.T) {
	result, err := ValidateParams("config.set", `{"raw":"yaml content"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestConfig_SchemaLookupValid(t *testing.T) {
	result, err := ValidateParams("config.schema.lookup", `{"path":"gateway.port"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestLogsTail_LimitTooHigh(t *testing.T) {
	result, err := ValidateParams("logs.tail", `{"limit":10000}`)
	if err != nil {
		t.Fatal(err)
	}
	hasMax := false
	for _, e := range result.Errors {
		if e.Keyword == "maximum" {
			hasMax = true
		}
	}
	if !hasMax {
		t.Fatalf("expected maximum error, got %v", result.Errors)
	}
}

func TestChatSend_Valid(t *testing.T) {
	result, err := ValidateParams("chat.send",
		`{"sessionKey":"sk","message":"hi","idempotencyKey":"idk1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestChatInject_Valid(t *testing.T) {
	result, err := ValidateParams("chat.inject",
		`{"sessionKey":"sk","message":"injected"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestChannels_StatusValid(t *testing.T) {
	result, err := ValidateParams("telegram.status", `{"probe":true,"timeoutMs":5000}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestAgents_CreateValid(t *testing.T) {
	result, err := ValidateParams("agents.create", `{"name":"bot","workspace":"/home/bot"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestSkills_InstallValid(t *testing.T) {
	result, err := ValidateParams("skills.install", `{"name":"weather","installId":"i1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestExec_ResolveValid(t *testing.T) {
	result, err := ValidateParams("exec.approval.resolve", `{"id":"req-1","decision":"allow"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestSecrets_ResolveValid(t *testing.T) {
	result, err := ValidateParams("secrets.resolve",
		`{"commandName":"cmd","targetIds":["a","b"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got %v", result.Errors)
	}
}

func TestSecrets_ResolveMissing(t *testing.T) {
	result, err := ValidateParams("secrets.resolve", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("expected invalid")
	}
	if len(result.Errors) < 2 {
		t.Fatalf("expected at least 2 errors, got %d", len(result.Errors))
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
