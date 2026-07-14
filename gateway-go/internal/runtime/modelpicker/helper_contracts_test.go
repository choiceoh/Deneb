package modelpicker

import (
	"fmt"
	"reflect"
	"testing"
)

func TestShortModelNameBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "plain",
			input: "model",
			want:  "model",
		},
		{
			name:  "single",
			input: "provider/model",
			want:  "model",
		},
		{
			name:  "nested",
			input: "openrouter/anthropic/model",
			want:  "model",
		},
		{
			name:  "trailing",
			input: "provider/",
			want:  "provider/",
		},
		{
			name:  "leading",
			input: "/model",
			want:  "model",
		},
		{
			name:  "double",
			input: "a//b",
			want:  "b",
		},
		{
			name:  "spaces",
			input: " provider/model ",
			want:  "model ",
		},
		{
			name:  "unicode",
			input: "공급자/모델",
			want:  "모델",
		},
		{
			name:  "slash-only",
			input: "/",
			want:  "/",
		},
		{
			name:  "many",
			input: "a/b/c/d",
			want:  "d",
		},
		{
			name:  "dot",
			input: "a/model.v2",
			want:  "model.v2",
		},
		{
			name:  "colon",
			input: "a/model:latest",
			want:  "model:latest",
		},
		{
			name:  "backslash",
			input: "a\\b",
			want:  "a\\b",
		},
		{
			name:  "url",
			input: "https://host/model",
			want:  "model",
		},
		{
			name:  "query",
			input: "a/model?q=1",
			want:  "model?q=1",
		},
		{
			name:  "hash",
			input: "a/model#x",
			want:  "model#x",
		},
		{
			name:  "two-char",
			input: "a/b",
			want:  "b",
		},
		{
			name:  "rooted",
			input: "///x",
			want:  "x",
		},
		{
			name:  "whitespace-slash",
			input: "a/ ",
			want:  " ",
		},
		{
			name:  "tab",
			input: "a/\tb",
			want:  "\tb",
		},
		{
			name:  "newline",
			input: "a/\nb",
			want:  "\nb",
		},
		{
			name:  "emoji",
			input: "a/😀",
			want:  "😀",
		},
		{
			name:  "provider-nested",
			input: "openrouter/google/gemini-3.1-pro",
			want:  "gemini-3.1-pro",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shortModelName(tc.input); got != tc.want {
				t.Fatalf("shortModelName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestProviderDisplayNameBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "zai",
			input: "zai",
			want:  "Z.ai",
		},
		{
			name:  "vllm",
			input: "vllm",
			want:  "vLLM",
		},
		{
			name:  "localai",
			input: "localai",
			want:  "LocalAI",
		},
		{
			name:  "openrouter",
			input: "openrouter",
			want:  "OpenRouter",
		},
		{
			name:  "anthropic",
			input: "anthropic",
			want:  "Anthropic",
		},
		{
			name:  "openai",
			input: "openai",
			want:  "OpenAI",
		},
		{
			name:  "google",
			input: "google",
			want:  "Google",
		},
		{
			name:  "kimi",
			input: "kimi",
			want:  "Kimi Code",
		},
		{
			name:  "mimo",
			input: "mimo",
			want:  "MiMo",
		},
		{
			name:  "mimo-plan",
			input: "mimo-plan",
			want:  "MiMo Token Plan",
		},
		{
			name:  "custom",
			input: "custom",
			want:  "직접 추가",
		},
		{
			name:  "custom-1",
			input: "custom-1",
			want:  "직접 추가",
		},
		{
			name:  "custom-provider",
			input: "custom-provider",
			want:  "직접 추가",
		},
		{
			name:  "customized",
			input: "customized",
			want:  "customized",
		},
		{
			name:  "unknown",
			input: "unknown",
			want:  "unknown",
		},
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "ZAI",
			input: "ZAI",
			want:  "ZAI",
		},
		{
			name:  "vLLM",
			input: "vLLM",
			want:  "vLLM",
		},
		{
			name:  "custom-",
			input: "custom-",
			want:  "직접 추가",
		},
		{
			name:  "custom--x",
			input: "custom--x",
			want:  "직접 추가",
		},
		{
			name:  "azure",
			input: "azure",
			want:  "azure",
		},
		{
			name:  "bedrock",
			input: "bedrock",
			want:  "bedrock",
		},
		{
			name:  "vertex",
			input: "vertex",
			want:  "vertex",
		},
		{
			name:  "ollama",
			input: "ollama",
			want:  "ollama",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := providerDisplayName(tc.input); got != tc.want {
				t.Fatalf("providerDisplayName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsLocalURLBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, input string
		want        bool
	}{
		{
			name:  "empty",
			input: "",
			want:  false,
		},
		{
			name:  "spaces",
			input: "   ",
			want:  false,
		},
		{
			name:  "localhost",
			input: "http://localhost:8000/v1",
			want:  true,
		},
		{
			name:  "localhost-https",
			input: "https://localhost",
			want:  true,
		},
		{
			name:  "ipv4",
			input: "http://127.0.0.1:8000",
			want:  true,
		},
		{
			name:  "ipv4-range",
			input: "http://127.12.34.56:9000/v1",
			want:  true,
		},
		{
			name:  "all",
			input: "http://0.0.0.0:8000",
			want:  true,
		},
		{
			name:  "ipv6",
			input: "http://[::1]:8000/v1",
			want:  true,
		},
		{
			name:  "remote",
			input: "https://api.example.com/v1",
			want:  false,
		},
		{
			name:  "private",
			input: "http://192.168.1.2:8000",
			want:  false,
		},
		{
			name:  "ten",
			input: "http://10.0.0.1:8000",
			want:  false,
		},
		{
			name:  "localhost-domain",
			input: "http://localhost.example.com",
			want:  false,
		},
		{
			name:  "lookalike",
			input: "http://127.example.com",
			want:  false,
		},
		{
			name:  "user-info",
			input: "http://user@localhost:8000",
			want:  true,
		},
		{
			name:  "path-only",
			input: "localhost:8000",
			want:  false,
		},
		{
			name:  "malformed",
			input: "://",
			want:  false,
		},
		{
			name:  "ftp",
			input: "ftp://localhost/models",
			want:  true,
		},
		{
			name:  "uppercase",
			input: "HTTP://LOCALHOST:8000",
			want:  true,
		},
		{
			name:  "ipv6-remote",
			input: "http://[2001:db8::1]:8000",
			want:  false,
		},
		{
			name:  "dns",
			input: "http://host.docker.internal:8000",
			want:  false,
		},
		{
			name:  "loopback-last",
			input: "http://127.255.255.254",
			want:  true,
		},
		{
			name:  "portless",
			input: "http://127.0.0.1",
			want:  true,
		},
		{
			name:  "query",
			input: "http://localhost?x=1",
			want:  true,
		},
		{
			name:  "fragment",
			input: "http://localhost/#x",
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLocalURL(tc.input); got != tc.want {
				t.Fatalf("isLocalURL(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestProviderClassificationWhenNameMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                     string
		custom, local, deletable bool
	}{
		{name: "", custom: false, local: false, deletable: false},
		{name: "custom", custom: true, local: false, deletable: true},
		{name: "custom-1", custom: true, local: false, deletable: true},
		{name: "customized", custom: false, local: false, deletable: true},
		{name: "vllm", custom: false, local: true, deletable: false},
		{name: "localai", custom: false, local: true, deletable: false},
		{name: "zai", custom: false, local: false, deletable: true},
		{name: "openrouter", custom: false, local: false, deletable: true},
		{name: "anthropic", custom: false, local: false, deletable: true},
		{name: "VLLM", custom: false, local: false, deletable: true},
		{name: " localai ", custom: false, local: false, deletable: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isMiniappCustomProvider(tc.name); got != tc.custom {
				t.Errorf("custom = %v, want %v", got, tc.custom)
			}
			if got := isMiniappLocalProvider(tc.name); got != tc.local {
				t.Errorf("local = %v, want %v", got, tc.local)
			}
			if got := isMiniappDeletableProvider(tc.name); got != tc.deletable {
				t.Errorf("deletable = %v, want %v", got, tc.deletable)
			}
		})
	}
}

func TestModelIDForProviderEntryBoundaryMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, provider, fullID, display, want string }{
		{
			name:     "strip",
			provider: "zai",
			fullID:   "zai/glm",
			display:  "glm",
			want:     "glm",
		},
		{
			name:     "nested",
			provider: "openrouter",
			fullID:   "openrouter/anthropic/opus",
			display:  "opus",
			want:     "anthropic/opus",
		},
		{
			name:     "mismatch",
			provider: "zai",
			fullID:   "openrouter/model",
			display:  "model",
			want:     "model",
		},
		{
			name:     "empty-provider",
			provider: "",
			fullID:   "raw",
			display:  "",
			want:     "",
		},
		{
			name:     "display-fallback",
			provider: "zai",
			fullID:   "",
			display:  "display",
			want:     "display",
		},
		{
			name:     "provider-only",
			provider: "zai",
			fullID:   "zai/",
			display:  "",
			want:     "",
		},
		{
			name:     "unicode",
			provider: "공급자",
			fullID:   "공급자/모델",
			display:  "모델",
			want:     "모델",
		},
		{
			name:     "prefix-lookalike",
			provider: "zai",
			fullID:   "zaix/model",
			display:  "model",
			want:     "model",
		},
		{
			name:     "custom",
			provider: "custom-1",
			fullID:   "custom-1/foo/bar",
			display:  "bar",
			want:     "foo/bar",
		},
		{
			name:     "spaces",
			provider: "zai",
			fullID:   "zai/ model ",
			display:  " model ",
			want:     " model ",
		},
		{
			name:     "case",
			provider: "ZAI",
			fullID:   "zai/model",
			display:  "model",
			want:     "model",
		},
		{
			name:     "exact-case",
			provider: "ZAI",
			fullID:   "ZAI/model",
			display:  "model",
			want:     "model",
		},
		{
			name:     "empty-all",
			provider: "",
			fullID:   "",
			display:  "",
			want:     "",
		},
		{
			name:     "slash-provider",
			provider: "a/b",
			fullID:   "a/b/model",
			display:  "model",
			want:     "model",
		},
		{
			name:     "double",
			provider: "a",
			fullID:   "a//model",
			display:  "model",
			want:     "/model",
		},
		{
			name:     "display",
			provider: "a",
			fullID:   "b/c",
			display:  "shown",
			want:     "shown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := modelEntry{provider: tc.provider, fullID: tc.fullID, display: tc.display}
			if got := modelIDForProviderEntry(entry); got != tc.want {
				t.Fatalf("modelIDForProviderEntry(%#v) = %q, want %q", entry, got, tc.want)
			}
		})
	}
}

func TestMergeModelsContractMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                         string
		configured, discovered, want []string
	}{
		{name: "empty", want: nil},
		{name: "configured", configured: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "discovered", discovered: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "stable-order", configured: []string{"b", "a"}, discovered: []string{"c"}, want: []string{"b", "a", "c"}},
		{name: "dedupe-across", configured: []string{"a"}, discovered: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "dedupe-within", configured: []string{"a", "a"}, discovered: []string{"b", "b"}, want: []string{"a", "b"}},
		{name: "trim", configured: []string{" a ", " "}, discovered: []string{" b "}, want: []string{"a", "b"}},
		{name: "case-sensitive", configured: []string{"A"}, discovered: []string{"a"}, want: []string{"A", "a"}},
		{name: "nested", configured: []string{"a/b"}, discovered: []string{"a/b", "a/c"}, want: []string{"a/b", "a/c"}},
		{name: "unicode", configured: []string{"모델"}, discovered: []string{"모델", "다른"}, want: []string{"모델", "다른"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mergeModels(tc.configured, tc.discovered)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mergeModels() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestCapMergedModelsDisplayBoundary(t *testing.T) {
	t.Parallel()
	makeModels := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s-%02d", prefix, i)
		}
		return out
	}
	cases := []struct {
		name                   string
		configured, discovered int
		want                   int
	}{
		{name: "none", want: 0},
		{name: "under", configured: 2, discovered: 3, want: 5},
		{name: "exact", configured: 2, discovered: maxModelsPerProvider - 2, want: maxModelsPerProvider},
		{name: "trim-discovered", configured: 2, discovered: 30, want: maxModelsPerProvider},
		{name: "declared-exempt", configured: maxModelsPerProvider + 5, discovered: 10, want: maxModelsPerProvider + 5},
		{name: "declared-only", configured: maxModelsPerProvider + 20, want: maxModelsPerProvider + 20},
		{name: "all-discovered", discovered: 50, want: maxModelsPerProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configured := makeModels("cfg", tc.configured)
			discovered := makeModels("found", tc.discovered)
			got := capMergedModels(configured, discovered)
			if len(got) != tc.want {
				t.Fatalf("len = %d, want %d: %#v", len(got), tc.want, got)
			}
			for i := range configured {
				if i >= len(got) || got[i] != configured[i] {
					t.Fatalf("configured model %d was not preserved", i)
				}
			}
		})
	}
}

func TestProviderEntriesCreatesModelEntries(t *testing.T) {
	t.Parallel()
	models := []string{
		"family/sub/model-0",
		"model-1",
		"model-2",
		"family/sub/model-3",
		"model-4",
		"model-5",
		"family/sub/model-6",
		"model-7",
		"model-8",
		"family/sub/model-9",
		"model-10",
		"model-11",
		"family/sub/model-12",
		"model-13",
		"model-14",
		"family/sub/model-15",
		"model-16",
		"model-17",
		"family/sub/model-18",
		"model-19",
		"model-20",
		"family/sub/model-21",
		"model-22",
		"model-23",
		"family/sub/model-24",
		"model-25",
		"model-26",
		"family/sub/model-27",
		"model-28",
		"model-29",
	}
	got := providerEntries(providerSpec{name: "provider-x", models: models})
	if len(got) != len(models) {
		t.Fatalf("len = %d, want %d", len(got), len(models))
	}
	for i, model := range models {
		wantShort := shortModelName(model)
		want := modelEntry{provider: "provider-x", label: wantShort, fullID: "provider-x/" + model, display: wantShort}
		if got[i] != want {
			t.Fatalf("entry %d = %#v, want %#v", i, got[i], want)
		}
	}
}
