package configresolve

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func writeResolveConfig(t *testing.T, body string) *slog.Logger {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deneb.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_CONFIG_PATH", path)
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDefaultModelConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing",
			body: "{}",
			want: "",
		},
		{
			name: "agents empty",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "direct",
			body: "{\"agents\":{\"defaultModel\":\"main\"}}",
			want: "main",
		},
		{
			name: "direct scoped",
			body: "{\"agents\":{\"defaultModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "direct unicode",
			body: "{\"agents\":{\"defaultModel\":\"한글\"}}",
			want: "한글",
		},
		{
			name: "direct whitespace",
			body: "{\"agents\":{\"defaultModel\":\"  main  \"}}",
			want: "  main  ",
		},
		{
			name: "direct empty",
			body: "{\"agents\":{\"defaultModel\":\"\"}}",
			want: "",
		},
		{
			name: "direct null",
			body: "{\"agents\":{\"defaultModel\":null}}",
			want: "",
		},
		{
			name: "direct number",
			body: "{\"agents\":{\"defaultModel\":42}}",
			want: "",
		},
		{
			name: "defaults string",
			body: "{\"agents\":{\"defaults\":{\"model\":\"m\"}}}",
			want: "m",
		},
		{
			name: "defaults unicode",
			body: "{\"agents\":{\"defaults\":{\"model\":\"모델\"}}}",
			want: "모델",
		},
		{
			name: "defaults empty",
			body: "{\"agents\":{\"defaults\":{\"model\":\"\"}}}",
			want: "",
		},
		{
			name: "defaults spaces",
			body: "{\"agents\":{\"defaults\":{\"model\":\"  m  \"}}}",
			want: "  m  ",
		},
		{
			name: "defaults object",
			body: "{\"agents\":{\"defaults\":{\"model\":{\"primary\":\"p\"}}}}",
			want: "p",
		},
		{
			name: "defaults object scoped",
			body: "{\"agents\":{\"defaults\":{\"model\":{\"primary\":\"v/p\"}}}}",
			want: "v/p",
		},
		{
			name: "defaults object empty",
			body: "{\"agents\":{\"defaults\":{\"model\":{\"primary\":\"\"}}}}",
			want: "",
		},
		{
			name: "defaults fallbacks only",
			body: "{\"agents\":{\"defaults\":{\"model\":{\"fallbacks\":[\"f\"]}}}}",
			want: "",
		},
		{
			name: "defaults null",
			body: "{\"agents\":{\"defaults\":{\"model\":null}}}",
			want: "",
		},
		{
			name: "defaults number",
			body: "{\"agents\":{\"defaults\":{\"model\":1}}}",
			want: "",
		},
		{
			name: "direct wins",
			body: "{\"agents\":{\"defaultModel\":\"direct\",\"defaults\":{\"model\":\"nested\"}}}",
			want: "direct",
		},
		{
			name: "direct same",
			body: "{\"agents\":{\"defaultModel\":\"same\",\"defaults\":{\"model\":{\"primary\":\"same\"}}}}",
			want: "same",
		},
		{
			name: "extra fields",
			body: "{\"agents\":{\"x\":1,\"defaultModel\":\"m\",\"y\":2}}",
			want: "m",
		},
		{
			name: "models unrelated",
			body: "{\"models\":{\"providers\":{}}}",
			want: "",
		},
		{
			name: "malformed",
			body: "{",
			want: "",
		},
		{
			name: "array root",
			body: "[]",
			want: "",
		},
		{
			name: "null root",
			body: "null",
			want: "",
		},
		{
			name: "whitespace json",
			body: " \n {\"agents\":{\"defaultModel\":\"m\"}} \n",
			want: "m",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			if got := DefaultModel(logger); got != tc.want {
				t.Fatalf("DefaultModel()=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestRoleModelConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name string
		kind string
		body string
		want string
	}{
		{
			name: "lightweight missing",
			kind: "lightweight",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "lightweight plain",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":\"model-x\"}}",
			want: "model-x",
		},
		{
			name: "lightweight scoped",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "lightweight unicode",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":\"모델\"}}",
			want: "모델",
		},
		{
			name: "lightweight trimmed",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":\"  model-x  \"}}",
			want: "model-x",
		},
		{
			name: "lightweight empty",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":\"\"}}",
			want: "",
		},
		{
			name: "lightweight number",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":42}}",
			want: "",
		},
		{
			name: "lightweight null",
			kind: "lightweight",
			body: "{\"agents\":{\"lightweightModel\":null}}",
			want: "",
		},
		{
			name: "fallback missing",
			kind: "fallback",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "fallback plain",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":\"model-x\"}}",
			want: "model-x",
		},
		{
			name: "fallback scoped",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "fallback unicode",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":\"모델\"}}",
			want: "모델",
		},
		{
			name: "fallback trimmed",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":\"  model-x  \"}}",
			want: "model-x",
		},
		{
			name: "fallback empty",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":\"\"}}",
			want: "",
		},
		{
			name: "fallback number",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":42}}",
			want: "",
		},
		{
			name: "fallback null",
			kind: "fallback",
			body: "{\"agents\":{\"fallbackModel\":null}}",
			want: "",
		},
		{
			name: "coding missing",
			kind: "coding",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "coding plain",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":\"model-x\"}}",
			want: "model-x",
		},
		{
			name: "coding scoped",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "coding unicode",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":\"모델\"}}",
			want: "모델",
		},
		{
			name: "coding trimmed",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":\"  model-x  \"}}",
			want: "model-x",
		},
		{
			name: "coding empty",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":\"\"}}",
			want: "",
		},
		{
			name: "coding number",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":42}}",
			want: "",
		},
		{
			name: "coding null",
			kind: "coding",
			body: "{\"agents\":{\"codingModel\":null}}",
			want: "",
		},
		{
			name: "tiny missing",
			kind: "tiny",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "tiny plain",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":\"model-x\"}}",
			want: "model-x",
		},
		{
			name: "tiny scoped",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "tiny unicode",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":\"모델\"}}",
			want: "모델",
		},
		{
			name: "tiny trimmed",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":\"  model-x  \"}}",
			want: "model-x",
		},
		{
			name: "tiny empty",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":\"\"}}",
			want: "",
		},
		{
			name: "tiny number",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":42}}",
			want: "",
		},
		{
			name: "tiny null",
			kind: "tiny",
			body: "{\"agents\":{\"tinyModel\":null}}",
			want: "",
		},
		{
			name: "vision missing",
			kind: "vision",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "vision plain",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":\"model-x\"}}",
			want: "model-x",
		},
		{
			name: "vision scoped",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":\"vendor/model\"}}",
			want: "vendor/model",
		},
		{
			name: "vision unicode",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":\"모델\"}}",
			want: "모델",
		},
		{
			name: "vision trimmed",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":\"  model-x  \"}}",
			want: "model-x",
		},
		{
			name: "vision empty",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":\"\"}}",
			want: "",
		},
		{
			name: "vision number",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":42}}",
			want: "",
		},
		{
			name: "vision null",
			kind: "vision",
			body: "{\"agents\":{\"visionModel\":null}}",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			var got string
			switch tc.kind {
			case "lightweight":
				got = LightweightModel(logger)
			case "fallback":
				got = FallbackModel(logger)
			case "coding":
				got = CodingModel(logger)
			case "tiny":
				got = TinyModel(logger)
			case "vision":
				got = VisionModel(logger)
			default:
				t.Fatalf("unknown kind %q", tc.kind)
			}
			if got != tc.want {
				t.Fatalf("%s model=%q want=%q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestSubagentModelConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing",
			body: "{}",
			want: "",
		},
		{
			name: "agents empty",
			body: "{\"agents\":{}}",
			want: "",
		},
		{
			name: "defaults empty",
			body: "{\"agents\":{\"defaults\":{}}}",
			want: "",
		},
		{
			name: "subagents empty",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{}}}}",
			want: "",
		},
		{
			name: "string",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":\"sub\"}}}}",
			want: "sub",
		},
		{
			name: "scoped",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":\"v/sub\"}}}}",
			want: "v/sub",
		},
		{
			name: "unicode",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":\"하위\"}}}}",
			want: "하위",
		},
		{
			name: "spaces",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":\"  sub  \"}}}}",
			want: "  sub  ",
		},
		{
			name: "empty string",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":\"\"}}}}",
			want: "",
		},
		{
			name: "object",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":{\"primary\":\"primary\"}}}}}",
			want: "primary",
		},
		{
			name: "object scoped",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":{\"primary\":\"v/p\"}}}}}",
			want: "v/p",
		},
		{
			name: "object empty",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":{\"primary\":\"\"}}}}}",
			want: "",
		},
		{
			name: "fallback only",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":{\"fallbacks\":[\"f\"]}}}}}",
			want: "",
		},
		{
			name: "null",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":null}}}}",
			want: "",
		},
		{
			name: "number",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":42}}}}",
			want: "",
		},
		{
			name: "bool",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":true}}}}",
			want: "",
		},
		{
			name: "array",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"model\":[\"x\"]}}}}",
			want: "",
		},
		{
			name: "extra",
			body: "{\"agents\":{\"defaults\":{\"subagents\":{\"x\":1,\"model\":\"m\"}}}}",
			want: "m",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			if got := SubagentDefaultModel(logger); got != tc.want {
				t.Fatalf("SubagentDefaultModel()=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestLocalVLLMModelConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing",
			body: "{}",
			want: "",
		},
		{
			name: "models empty",
			body: "{\"models\":{}}",
			want: "",
		},
		{
			name: "providers empty",
			body: "{\"models\":{\"providers\":{}}}",
			want: "",
		},
		{
			name: "vllm empty",
			body: "{\"models\":{\"providers\":{\"vllm\":{}}}}",
			want: "",
		},
		{
			name: "models empty array",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[]}}}}",
			want: "",
		},
		{
			name: "one",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"m\"}]}}}}",
			want: "m",
		},
		{
			name: "scoped",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"v/m\"}]}}}}",
			want: "v/m",
		},
		{
			name: "unicode",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"모델\"}]}}}}",
			want: "모델",
		},
		{
			name: "spaces",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"  m  \"}]}}}}",
			want: "  m  ",
		},
		{
			name: "empty id",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"\"}]}}}}",
			want: "",
		},
		{
			name: "first wins",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"first\"},{\"id\":\"second\"}]}}}}",
			want: "first",
		},
		{
			name: "missing first id",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{},{\"id\":\"second\"}]}}}}",
			want: "",
		},
		{
			name: "extra model",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":\"m\",\"x\":1}]}}}}",
			want: "m",
		},
		{
			name: "wrong models object",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":{}}}}}",
			want: "",
		},
		{
			name: "wrong id number",
			body: "{\"models\":{\"providers\":{\"vllm\":{\"models\":[{\"id\":1}]}}}}",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			if got := LocalVLLMModel(logger); got != tc.want {
				t.Fatalf("LocalVLLMModel()=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestProactiveThresholdConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "missing",
			body: "{}",
			want: 0,
		},
		{
			name: "agents empty",
			body: "{\"agents\":{}}",
			want: 0,
		},
		{
			name: "zero",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":0}}",
			want: 0,
		},
		{
			name: "one",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":1}}",
			want: 1,
		},
		{
			name: "two",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":2}}",
			want: 2,
		},
		{
			name: "ten",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":10}}",
			want: 10,
		},
		{
			name: "large",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":2147483647}}",
			want: 2147483647,
		},
		{
			name: "negative one",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":-1}}",
			want: 0,
		},
		{
			name: "negative large",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":-999}}",
			want: 0,
		},
		{
			name: "null",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":null}}",
			want: 0,
		},
		{
			name: "string",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":\"3\"}}",
			want: 0,
		},
		{
			name: "float",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":1.5}}",
			want: 0,
		},
		{
			name: "bool",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":true}}",
			want: 0,
		},
		{
			name: "array",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":[]}}",
			want: 0,
		},
		{
			name: "object",
			body: "{\"agents\":{\"proactiveEscalateThreshold\":{}}}",
			want: 0,
		},
		{
			name: "extra",
			body: "{\"agents\":{\"x\":1,\"proactiveEscalateThreshold\":7}}",
			want: 7,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			if got := ProactiveEscalateThreshold(logger); got != tc.want {
				t.Fatalf("threshold=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestSessionThinkingConfigFileMatrix(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		wantLevel       string
		wantInterleaved *bool
	}{
		{
			name:            "missing",
			body:            "{}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "agents empty",
			body:            "{\"agents\":{}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "defaults empty",
			body:            "{\"agents\":{\"defaults\":{}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "thinking absent",
			body:            "{\"agents\":{\"defaults\":{\"model\":\"m\"}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "thinking null",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":null}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "empty object",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "low",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"low\"}}}}",
			wantLevel:       "low",
			wantInterleaved: nil,
		},
		{
			name:            "upper",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"HIGH\"}}}}",
			wantLevel:       "high",
			wantInterleaved: nil,
		},
		{
			name:            "mixed",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"MeDiUm\"}}}}",
			wantLevel:       "medium",
			wantInterleaved: nil,
		},
		{
			name:            "trimmed",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"  HIGH  \"}}}}",
			wantLevel:       "high",
			wantInterleaved: nil,
		},
		{
			name:            "off",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"off\"}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "off upper",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"OFF\"}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "empty level",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"\"}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "custom",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"custom\"}}}}",
			wantLevel:       "custom",
			wantInterleaved: nil,
		},
		{
			name:            "true",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"interleaved\":true}}}}",
			wantLevel:       "",
			wantInterleaved: boolPointer(true),
		},
		{
			name:            "false",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"interleaved\":false}}}}",
			wantLevel:       "",
			wantInterleaved: boolPointer(false),
		},
		{
			name:            "level true",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"high\",\"interleaved\":true}}}}",
			wantLevel:       "high",
			wantInterleaved: boolPointer(true),
		},
		{
			name:            "level false",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":\"low\",\"interleaved\":false}}}}",
			wantLevel:       "low",
			wantInterleaved: boolPointer(false),
		},
		{
			name:            "interleaved null",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"interleaved\":null}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "invalid level number",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"level\":1}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
		{
			name:            "invalid interleaved string",
			body:            "{\"agents\":{\"defaults\":{\"thinking\":{\"interleaved\":\"true\"}}}}",
			wantLevel:       "",
			wantInterleaved: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := writeResolveConfig(t, tc.body)
			got := SessionThinkingDefaults(logger)
			if got.ThinkingLevel != tc.wantLevel {
				t.Errorf("level=%q want=%q", got.ThinkingLevel, tc.wantLevel)
			}
			if (got.InterleavedThinking == nil) != (tc.wantInterleaved == nil) {
				t.Fatalf("interleaved=%v want=%v", got.InterleavedThinking, tc.wantInterleaved)
			}
			if got.InterleavedThinking != nil && *got.InterleavedThinking != *tc.wantInterleaved {
				t.Errorf("interleaved=%v want=%v", *got.InterleavedThinking, *tc.wantInterleaved)
			}
		})
	}
}

func boolPointer(v bool) *bool { return &v }
