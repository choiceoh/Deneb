package configresolve

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestExtractModelFromDefaultsBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw, want string }{
		{
			name: "empty defaults",
			raw:  "",
			want: "",
		},
		{
			name: "empty object",
			raw:  "{}",
			want: "",
		},
		{
			name: "missing model",
			raw:  "{\"thinking\":{}}",
			want: "",
		},
		{
			name: "string",
			raw:  "{\"model\":\"main\"}",
			want: "main",
		},
		{
			name: "empty string",
			raw:  "{\"model\":\"\"}",
			want: "",
		},
		{
			name: "space string",
			raw:  "{\"model\":\"  model  \"}",
			want: "  model  ",
		},
		{
			name: "object primary",
			raw:  "{\"model\":{\"primary\":\"primary\"}}",
			want: "primary",
		},
		{
			name: "object empty",
			raw:  "{\"model\":{\"primary\":\"\"}}",
			want: "",
		},
		{
			name: "object fallbacks only",
			raw:  "{\"model\":{\"fallbacks\":[\"x\"]}}",
			want: "",
		},
		{
			name: "object extra",
			raw:  "{\"model\":{\"primary\":\"p\",\"fallbacks\":[\"f\"]}}",
			want: "p",
		},
		{
			name: "model null",
			raw:  "{\"model\":null}",
			want: "",
		},
		{
			name: "model number",
			raw:  "{\"model\":42}",
			want: "",
		},
		{
			name: "model bool",
			raw:  "{\"model\":true}",
			want: "",
		},
		{
			name: "model array",
			raw:  "{\"model\":[\"x\"]}",
			want: "",
		},
		{
			name: "invalid open",
			raw:  "{",
			want: "",
		},
		{
			name: "invalid model",
			raw:  "{\"model\":",
			want: "",
		},
		{
			name: "nested wrong",
			raw:  "{\"agents\":{\"model\":\"x\"}}",
			want: "",
		},
		{
			name: "unicode string",
			raw:  "{\"model\":\"한글모델\"}",
			want: "한글모델",
		},
		{
			name: "scoped string",
			raw:  "{\"model\":\"vendor/model\"}",
			want: "vendor/model",
		},
		{
			name: "duplicate last string",
			raw:  "{\"model\":\"first\",\"model\":\"last\"}",
			want: "last",
		},
		{
			name: "duplicate last object",
			raw:  "{\"model\":\"first\",\"model\":{\"primary\":\"last\"}}",
			want: "last",
		},
		{
			name: "object primary number",
			raw:  "{\"model\":{\"primary\":1}}",
			want: "",
		},
		{
			name: "object primary null",
			raw:  "{\"model\":{\"primary\":null}}",
			want: "",
		},
		{
			name: "unknown fields",
			raw:  "{\"x\":1,\"model\":\"m\",\"y\":2}",
			want: "m",
		},
		{
			name: "whitespace json",
			raw:  " \n {\"model\":\"m\"} \t",
			want: "m",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractModelFromDefaults(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("extract(%q)=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestConvertRoutingConfigBoundaryMatrix(t *testing.T) {
	if got := convertRoutingConfig(nil); got != nil {
		t.Fatalf("nil converted=%#v", got)
	}
	tests := []struct{ name, raw string }{
		{name: "empty", raw: `{}`},
		{name: "enabled true", raw: `{"enabled":true}`},
		{name: "enabled false", raw: `{"enabled":false}`},
		{name: "toggle", raw: `{"toggleKwarg":"thinking"}`},
		{name: "simple", raw: `{"maxSimpleRunes":100}`},
		{name: "ceiling", raw: `{"stepCeilingTurn":4}`},
		{name: "observation", raw: `{"observationRunes":200}`},
		{name: "cumulative", raw: `{"cumulativeRunes":300}`},
		{name: "history", raw: `{"heavyHistoryRunes":400}`},
		{name: "all", raw: `{"enabled":true,"toggleKwarg":"think","maxSimpleRunes":1,"stepCeilingTurn":2,"observationRunes":3,"cumulativeRunes":4,"heavyHistoryRunes":5}`},
		{name: "zeros", raw: `{"maxSimpleRunes":0,"stepCeilingTurn":0}`},
		{name: "negative", raw: `{"maxSimpleRunes":-1,"stepCeilingTurn":-2}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var input chatport.RoutingConfig
			if err := json.Unmarshal([]byte(tc.raw), &input); err != nil {
				t.Fatal(err)
			}
			got := convertRoutingConfig(&input)
			if got == nil {
				t.Fatal("non-nil input converted to nil")
			}
			if !reflect.DeepEqual(input.Enabled, got.Enabled) ||
				!reflect.DeepEqual(input.ToggleKwarg, got.ToggleKwarg) ||
				!reflect.DeepEqual(input.MaxSimpleRunes, got.MaxSimpleRunes) ||
				!reflect.DeepEqual(input.StepCeilingTurn, got.StepCeilingTurn) ||
				!reflect.DeepEqual(input.ObservationRunes, got.ObservationRunes) ||
				!reflect.DeepEqual(input.CumulativeRunes, got.CumulativeRunes) ||
				!reflect.DeepEqual(input.HeavyHistoryRunes, got.HeavyHistoryRunes) {
				t.Errorf("routing fields changed: input=%+v output=%+v", input, got)
			}
		})
	}
}

func TestConvertRoutingConfigPreservesOutputAfterInputMutation(t *testing.T) {
	var input chatport.RoutingConfig
	if err := json.Unmarshal([]byte(`{"enabled":true,"maxSimpleRunes":10}`), &input); err != nil {
		t.Fatal(err)
	}
	got := convertRoutingConfig(&input)
	if got == nil {
		t.Fatal("nil conversion")
	}
	before, _ := json.Marshal(got)
	input = chatport.RoutingConfig{}
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Errorf("conversion container aliased input: before=%s after=%s", before, after)
	}
}

func TestConfigResolveMissingConfigDegrades(t *testing.T) {
	t.Setenv("DENEB_CONFIG_PATH", t.TempDir()+"/missing.json")
	if got := LoadProviderConfigs(nil); got != nil {
		t.Errorf("providers=%v", got)
	}
	if got := ProviderCatalog(nil); got != nil {
		t.Errorf("catalog=%v", got)
	}
	if got := DefaultModel(nil); got != "" {
		t.Errorf("default=%q", got)
	}
	if got := ProactiveEscalateThreshold(nil); got != 0 {
		t.Errorf("threshold=%d", got)
	}
	if got := LocalVLLMModel(nil); got != "" {
		t.Errorf("vllm=%q", got)
	}
	if got := LightweightModel(nil); got != "" {
		t.Errorf("lightweight=%q", got)
	}
	if got := FallbackModel(nil); got != "" {
		t.Errorf("fallback=%q", got)
	}
	if got := CodingModel(nil); got != "" {
		t.Errorf("coding=%q", got)
	}
	if got := TinyModel(nil); got != "" {
		t.Errorf("tiny=%q", got)
	}
	if got := VisionModel(nil); got != "" {
		t.Errorf("vision=%q", got)
	}
	if got := SubagentDefaultModel(nil); got != "" {
		t.Errorf("subagent=%q", got)
	}
	if got := CurrentTopicKey(); got != "" {
		t.Errorf("topic=%q", got)
	}
	if got := TopicsDir(); got != "" {
		t.Errorf("topics dir=%q", got)
	}
}

func TestConfigResolvePureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				raw := json.RawMessage(fmt.Sprintf(`{"model":{"primary":"m-%d-%d"}}`, worker, i))
				want := fmt.Sprintf("m-%d-%d", worker, i)
				if got := extractModelFromDefaults(raw); got != want {
					errs <- fmt.Errorf("got=%q want=%q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
