package chat

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

func TestShouldForceExternalDeliveryFailureNotice(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}
	toolActivities := []agent.ToolActivity{
		{Name: "message", IsError: true},
	}

	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", true) {
		t.Fatal("expected forced notice for silent failed external delivery")
	}
	if !shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "", false) {
		t.Fatal("expected forced notice for empty failed external delivery")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, toolActivities, "실패했습니다. 다시 시도해 주세요.", false) {
		t.Fatal("did not expect forced notice when assistant already produced a visible explanation")
	}
}

func TestShouldForceExternalDeliveryFailureNotice_IgnoresUnrelatedCases(t *testing.T) {
	delivery := &DeliveryContext{Channel: "telegram", To: "telegram:123"}

	if shouldForceExternalDeliveryFailureNotice(nil, []agent.ToolActivity{{Name: "message", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice without a delivery context")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "exec", IsError: true}}, "", true) {
		t.Fatal("did not expect forced notice for non-delivery tool errors")
	}
	if shouldForceExternalDeliveryFailureNotice(delivery, []agent.ToolActivity{{Name: "message", IsError: false}}, "", true) {
		t.Fatal("did not expect forced notice when delivery tool succeeded")
	}
}

// TestClassifyRunFailureReason_LegacyCoverage locks in the labels the
// pre-llmerr substring classifier would have produced, so the migration is
// strictly non-regressive for inputs the old code recognised.
func TestClassifyRunFailureReason_LegacyCoverage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		// Plain-string errors (no structured status). Exercise the legacy
		// bare-digit fallback and llmerr message-pattern pipeline.
		{"plain 429", errors.New("openai: 429 Too Many Requests"), "API 요청 한도 초과 (429)"},
		{"plain 401", errors.New("openai: 401 Unauthorized"), "API 인증 실패 (401)"},
		{"plain unauthorized word", errors.New("request unauthorized: bad key"), "API 인증 실패 (401)"},
		{"invalid_api_key code", errors.New("invalid_api_key provided"), "API 인증 실패 (401)"},
		{"billing word", errors.New("billing not active on account"), "결제 오류"},
		{"insufficient_quota code", errors.New("insufficient_quota: you exceeded your current quota"), "결제 오류"},
		{"plain 502", errors.New("HTTP 502 bad gateway"), "서버 일시 장애"},
		{"plain 503", errors.New("HTTP 503 service unavailable"), "서버 일시 장애"},
		{"plain 521", errors.New("HTTP 521 web server is down"), "서버 일시 장애"},
		{"plain 529", errors.New("HTTP 529 overloaded"), "서버 일시 장애"},
		{"context overflow phrase", errors.New("prompt is too long for this model"), "컨텍스트 초과"},
		{"context_length_exceeded code", errors.New("error: context_length_exceeded"), "컨텍스트 초과"},
		{"unrecognised generic", errors.New("totally unknown failure"), ""},
		{"nil error", nil, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRunFailureReason(tc.err); got != tc.want {
				t.Errorf("classifyRunFailureReason(%q) = %q, want %q", errString(tc.err), got, tc.want)
			}
		})
	}
}

// TestClassifyRunFailureReason_StructuredAPIError exercises the migration's
// new coverage: when the error is a wrapped *httpretry.APIError, the status
// drives the label even if the string does not contain a bare digit.
func TestClassifyRunFailureReason_StructuredAPIError(t *testing.T) {
	tests := []struct {
		name string
		err  *httpretry.APIError
		want string
	}{
		{
			name: "structured 429",
			err:  &httpretry.APIError{StatusCode: 429, Message: "Too Many Requests"},
			want: "API 요청 한도 초과 (429)",
		},
		{
			name: "structured 401",
			err:  &httpretry.APIError{StatusCode: 401, Message: "bad credentials"},
			want: "API 인증 실패 (401)",
		},
		{
			// New coverage: 402 is billing in llmerr; the old substring
			// classifier would have returned "" here.
			name: "structured 402 billing",
			err:  &httpretry.APIError{StatusCode: 402, Message: "payment required"},
			want: "결제 오류",
		},
		{
			name: "structured 500",
			err:  &httpretry.APIError{StatusCode: 500, Message: "internal error"},
			want: "서버 일시 장애",
		},
		{
			name: "structured 503",
			err:  &httpretry.APIError{StatusCode: 503, Message: "service unavailable"},
			want: "서버 일시 장애",
		},
		{
			// New coverage: structured context-overflow code.
			name: "structured 400 context_length_exceeded",
			err:  &httpretry.APIError{StatusCode: 400, Message: `{"error":{"code":"context_length_exceeded"}}`},
			want: "컨텍스트 초과",
		},
		{
			// New coverage: 413 → payload_too_large → 컨텍스트 초과 label
			// (same compress action, same user-facing label).
			name: "structured 413 payload too large",
			err:  &httpretry.APIError{StatusCode: 413, Message: "payload too large"},
			want: "컨텍스트 초과",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRunFailureReason(tc.err); got != tc.want {
				t.Errorf("classifyRunFailureReason(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
