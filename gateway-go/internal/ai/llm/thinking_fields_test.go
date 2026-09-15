package llm

import (
	"encoding/json"
	"testing"
)

func TestThinkingOffFields(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kwarg          string
		reasoningParam bool
		want           string
	}{
		{"template kwarg", "enable_thinking", false, `{"chat_template_kwargs":{"enable_thinking":false}}`},
		{"dual-mode kwarg", "thinking", false, `{"chat_template_kwargs":{"thinking":false}}`},
		{"openrouter reasoning field", "", true, `{"reasoning":{"enabled":false}}`},
		{"kwarg wins when both", "enable_thinking", true, `{"chat_template_kwargs":{"enable_thinking":false}}`},
		{"neither", "", false, `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(ThinkingOffFields(tc.kwarg, tc.reasoningParam))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("ThinkingOffFields = %s, want %s", got, tc.want)
			}
		})
	}
}

// OpenRouter reports an upstream failure as HTTP 200 plus an in-stream error
// object. The code is the only status the caller ever sees, so the repacked
// error event must keep it (the helper path decides retry vs. surface on it).
func TestProbeOpenAIErrorKeepsNumericCode(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{
			"numeric code", `{"error":{"code":502,"message":"Upstream error from Nvidia: Service temporarily overloaded"}}`,
			`{"code":502,"message":"Upstream error from Nvidia: Service temporarily overloaded","type":""}`,
		},
		{
			"no code", `{"error":{"message":"generation failed","type":"server_error"}}`,
			`{"message":"generation failed","type":"server_error"}`,
		},
		{
			"string code is not a status", `{"error":{"code":"1302","message":"quota"}}`,
			`{"message":"quota","type":""}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := probeOpenAIError(FlexibleFromRaw([]byte(tc.raw)))
			if !ok {
				t.Fatal("probeOpenAIError did not recognize the error body")
			}
			if got.String() != tc.want {
				t.Fatalf("repacked = %s, want %s", got.String(), tc.want)
			}
		})
	}
}
