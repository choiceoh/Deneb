package modelpanel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func modelPanelTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPanelModelFamilyBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{
			name:  "deepseek exact",
			model: "deepseek",
			want:  "deepseek",
		},
		{
			name:  "deepseek uppercase",
			model: "DEEPSEEK",
			want:  "deepseek",
		},
		{
			name:  "deepseek version",
			model: "deepseek-v3.1",
			want:  "deepseek",
		},
		{
			name:  "deepseek scoped",
			model: "org/deepseek-r1",
			want:  "deepseek",
		},
		{
			name:  "dsv short",
			model: "dsv3",
			want:  "deepseek",
		},
		{
			name:  "dsv uppercase",
			model: "DSV-V2",
			want:  "deepseek",
		},
		{
			name:  "qwen exact",
			model: "qwen",
			want:  "qwen",
		},
		{
			name:  "qwen uppercase",
			model: "QWEN",
			want:  "qwen",
		},
		{
			name:  "qwen version",
			model: "Qwen3.6-35B",
			want:  "qwen",
		},
		{
			name:  "qwen scoped",
			model: "org/qwen2.5",
			want:  "qwen",
		},
		{
			name:  "glm exact",
			model: "glm",
			want:  "glm",
		},
		{
			name:  "glm uppercase",
			model: "GLM-4.5",
			want:  "glm",
		},
		{
			name:  "glm scoped",
			model: "zai/glm-4",
			want:  "glm",
		},
		{
			name:  "mimo exact",
			model: "mimo",
			want:  "mimo",
		},
		{
			name:  "mimo uppercase",
			model: "MiMo-V2",
			want:  "mimo",
		},
		{
			name:  "mimo scoped",
			model: "xiaomi/mimo",
			want:  "mimo",
		},
		{
			name:  "kimi exact",
			model: "kimi",
			want:  "kimi",
		},
		{
			name:  "kimi uppercase",
			model: "KIMI-K2",
			want:  "kimi",
		},
		{
			name:  "kimi scoped",
			model: "moonshot/kimi-k2",
			want:  "kimi",
		},
		{
			name:  "gemma exact",
			model: "gemma",
			want:  "gemma",
		},
		{
			name:  "gemma uppercase",
			model: "GEMMA-3",
			want:  "gemma",
		},
		{
			name:  "gemma scoped",
			model: "google/gemma-3",
			want:  "gemma",
		},
		{
			name:  "gpt exact",
			model: "gpt",
			want:  "openai",
		},
		{
			name:  "gpt version",
			model: "gpt-5.2",
			want:  "openai",
		},
		{
			name:  "gpt uppercase",
			model: "GPT-4O",
			want:  "openai",
		},
		{
			name:  "openai exact",
			model: "openai",
			want:  "openai",
		},
		{
			name:  "openai scoped",
			model: "openai/o3",
			want:  "openai",
		},
		{
			name:  "claude exact",
			model: "claude",
			want:  "anthropic",
		},
		{
			name:  "claude version",
			model: "claude-sonnet-4.5",
			want:  "anthropic",
		},
		{
			name:  "claude uppercase",
			model: "CLAUDE-OPUS",
			want:  "anthropic",
		},
		{
			name:  "anthropic exact",
			model: "anthropic",
			want:  "anthropic",
		},
		{
			name:  "anthropic scoped",
			model: "anthropic/haiku",
			want:  "anthropic",
		},
		{
			name:  "gemini exact",
			model: "gemini",
			want:  "gemini",
		},
		{
			name:  "gemini version",
			model: "gemini-2.5-pro",
			want:  "gemini",
		},
		{
			name:  "gemini uppercase",
			model: "GEMINI-FLASH",
			want:  "gemini",
		},
		{
			name:  "empty",
			model: "",
			want:  "",
		},
		{
			name:  "spaces",
			model: "   ",
			want:  "   ",
		},
		{
			name:  "custom",
			model: "custom-router-model",
			want:  "custom-router-model",
		},
		{
			name:  "custom uppercase",
			model: "CUSTOM-MODEL",
			want:  "custom-model",
		},
		{
			name:  "scoped custom",
			model: "vendor/model-v1",
			want:  "vendor/model-v1",
		},
		{
			name:  "unicode",
			model: "모델-가",
			want:  "모델-가",
		},
		{
			name:  "emoji",
			model: "model-🚀",
			want:  "model-🚀",
		},
		{
			name:  "leading spaces",
			model: "  GPT-5",
			want:  "openai",
		},
		{
			name:  "trailing spaces",
			model: "GPT-5  ",
			want:  "openai",
		},
		{
			name:  "substring deepseek",
			model: "notdeepseekish",
			want:  "deepseek",
		},
		{
			name:  "substring qwen",
			model: "preqwenpost",
			want:  "qwen",
		},
		{
			name:  "substring glm",
			model: "xglmy",
			want:  "glm",
		},
		{
			name:  "substring mimo",
			model: "xmimoy",
			want:  "mimo",
		},
		{
			name:  "substring kimi",
			model: "xkimiy",
			want:  "kimi",
		},
		{
			name:  "substring gemma",
			model: "xgemmay",
			want:  "gemma",
		},
		{
			name:  "substring gpt",
			model: "xgpty",
			want:  "openai",
		},
		{
			name:  "substring openai",
			model: "xopenaiy",
			want:  "openai",
		},
		{
			name:  "substring claude",
			model: "xclaudey",
			want:  "anthropic",
		},
		{
			name:  "substring anthropic",
			model: "xanthropicy",
			want:  "anthropic",
		},
		{
			name:  "substring gemini",
			model: "xgeminiy",
			want:  "gemini",
		},
		{
			name:  "precedence deepseek qwen",
			model: "qwen-deepseek",
			want:  "deepseek",
		},
		{
			name:  "precedence qwen glm",
			model: "glm-qwen",
			want:  "qwen",
		},
		{
			name:  "precedence glm mimo",
			model: "mimo-glm",
			want:  "glm",
		},
		{
			name:  "precedence mimo kimi",
			model: "kimi-mimo",
			want:  "mimo",
		},
		{
			name:  "precedence kimi gemma",
			model: "gemma-kimi",
			want:  "kimi",
		},
		{
			name:  "precedence gemma gpt",
			model: "gpt-gemma",
			want:  "gemma",
		},
		{
			name:  "precedence openai anthropic",
			model: "claude-openai",
			want:  "openai",
		},
		{
			name:  "precedence anthropic gemini",
			model: "gemini-claude",
			want:  "anthropic",
		},
		{
			name:  "dsv before qwen",
			model: "qwen-dsv",
			want:  "deepseek",
		},
		{
			name:  "newline custom",
			model: "custom\nmodel",
			want:  "custom\nmodel",
		},
		{
			name:  "tab custom",
			model: "custom\tmodel",
			want:  "custom\tmodel",
		},
		{
			name:  "numeric",
			model: "12345",
			want:  "12345",
		},
		{
			name:  "punctuation",
			model: "model:v1.2",
			want:  "model:v1.2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := panelModelFamily(tc.model); got != tc.want {
				t.Fatalf("panelModelFamily(%q)=%q want=%q", tc.model, got, tc.want)
			}
		})
	}
}

func TestPanelConstructionAndNilRegistryBoundaries(t *testing.T) {
	logger := modelPanelTestLogger()
	panel := New(nil, logger)
	if panel == nil {
		t.Fatal("New returned nil")
	}
	if panel.modelRegistry != nil {
		t.Error("nil registry changed")
	}
	if panel.logger != logger {
		t.Error("logger boundary not retained")
	}
	for _, tc := range []struct {
		system, prompt string
		models         []string
	}{
		{system: "", prompt: "", models: nil},
		{system: "system", prompt: "question", models: nil},
		{system: "system", prompt: "question", models: []string{}},
		{system: "system", prompt: "question", models: []string{"qwen"}},
		{system: strings.Repeat("s", 4096), prompt: strings.Repeat("p", 4096), models: []string{"a", "b"}},
	} {
		if got := panel.Consult(context.Background(), tc.system, tc.prompt, tc.models); got != nil {
			t.Errorf("Consult=%#v want nil", got)
		}
		if got := panel.panelHealthyModels(context.Background()); got != nil {
			t.Errorf("healthy=%#v want nil", got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := panel.Consult(ctx, "system", "prompt", []string{"qwen"}); got != nil {
		t.Errorf("canceled Consult=%#v", got)
	}
}

func TestPanelConstantsBoundary(t *testing.T) {
	tests := []struct {
		name      string
		got, want int
	}{
		{name: "max concurrency", got: panelMaxConcurrency, want: 6},
		{name: "max models", got: panelMaxModels, want: 8},
		{name: "max tokens", got: panelMaxTokens, want: 2048},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got=%d want=%d", tc.got, tc.want)
			}
			if tc.got <= 0 {
				t.Errorf("constant must be positive: %d", tc.got)
			}
		})
	}
	if panelPerModelTimeout != 90*time.Second {
		t.Errorf("timeout=%s", panelPerModelTimeout)
	}
	if panelMaxConcurrency > panelMaxModels {
		t.Errorf("concurrency %d exceeds model cap %d", panelMaxConcurrency, panelMaxModels)
	}
}

func TestPanelModelFamilyIdempotence(t *testing.T) {
	models := []string{"deepseek", "qwen", "glm", "mimo", "kimi", "gemma", "openai", "anthropic", "gemini", "custom"}
	for _, model := range models {
		once := panelModelFamily(model)
		twice := panelModelFamily(once)
		if twice != once {
			t.Errorf("family not idempotent: %q -> %q -> %q", model, once, twice)
		}
	}
}

func TestPanelModelFamilyParsesLongInputs(t *testing.T) {
	tests := []struct{ name, model, want string }{
		{name: "deepseek at start", model: "deepseek" + strings.Repeat("x", 1<<16), want: "deepseek"},
		{name: "qwen at middle", model: strings.Repeat("x", 1<<15) + "qwen" + strings.Repeat("y", 1<<15), want: "qwen"},
		{name: "gemini at end", model: strings.Repeat("z", 1<<16) + "gemini", want: "gemini"},
		{name: "custom long", model: strings.Repeat("c", 1<<16), want: strings.Repeat("c", 1<<16)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := panelModelFamily(tc.model); got != tc.want {
				t.Errorf("family len=%d want=%q got-prefix=%q", len(tc.model), tc.want, got[:min(len(got), 20)])
			}
		})
	}
}

func TestPanelPureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	const iterations = 100
	panel := New(nil, nil)
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				model := fmt.Sprintf("QWEN-%d-%d", worker, i)
				if got := panelModelFamily(model); got != "qwen" {
					errs <- fmt.Errorf("family=%q", got)
					return
				}
				if got := panel.Consult(context.Background(), "s", "p", []string{model}); got != nil {
					errs <- fmt.Errorf("consult=%#v", got)
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
