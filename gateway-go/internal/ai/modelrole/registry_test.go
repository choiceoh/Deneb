package modelrole

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestMain forces the vLLM discovery probe to fail fast so registry tests
// stay independent of any vLLM server that happens to be running on the
// test machine. Reconcile leaves the configured model untouched on probe
// failure, which is what these tests assert. The discovery-specific tests
// in vllm_discovery_test.go install their own client per-case via
// httptest.NewServer so they are unaffected.
func TestMain(m *testing.M) {
	vllmDiscoveryClient = &http.Client{
		Timeout: 50 * time.Millisecond,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return nil, errors.New("vllm probe disabled in registry_test")
			},
		},
	}
	os.Exit(m.Run())
}

func TestResolveModel(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model", "")

	tests := []struct {
		input    string
		wantID   string
		wantRole Role
		wantOK   bool
	}{
		// Role names resolve to full model IDs.
		{"main", "zai/test-model", RoleMain, true},
		{"lightweight", "vllm/" + DefaultVllmModel, RoleLightweight, true},
		{"fallback", "vllm/" + DefaultVllmModel, RoleFallback, true},
		// Actual model names pass through unchanged.
		{"google/gemini-3.1-pro", "google/gemini-3.1-pro", "", false},
		{"some-unknown-model", "some-unknown-model", "", false},
		{"", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotID, gotRole, gotOK := reg.ResolveModel(tt.input)
			if gotID != tt.wantID {
				t.Errorf("ResolveModel(%q) id = %q, want %q", tt.input, gotID, tt.wantID)
			}
			if gotRole != tt.wantRole {
				t.Errorf("ResolveModel(%q) role = %q, want %q", tt.input, gotRole, tt.wantRole)
			}
			if gotOK != tt.wantOK {
				t.Errorf("ResolveModel(%q) ok = %v, want %v", tt.input, gotOK, tt.wantOK)
			}
		})
	}
}

func TestRoleForModel(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model", "")

	tests := []struct {
		fullModelID string
		wantRole    Role
		wantFound   bool
	}{
		{"zai/test-model", RoleMain, true},
		// Lightweight and Fallback both use vllm/qwen36; RoleForModel returns the
		// first match (Lightweight) when iterating roles in order.
		{"vllm/" + DefaultVllmModel, RoleLightweight, true},
		{"unknown/model", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.fullModelID, func(t *testing.T) {
			gotRole, gotFound := reg.RoleForModel(tt.fullModelID)
			if gotFound != tt.wantFound {
				t.Errorf("RoleForModel(%q) found = %v, want %v", tt.fullModelID, gotFound, tt.wantFound)
			}
			if gotFound && gotRole != tt.wantRole {
				t.Errorf("RoleForModel(%q) role = %q, want %q", tt.fullModelID, gotRole, tt.wantRole)
			}
		})
	}
}

func TestEmptyMainModelDefaultsToVllm(t *testing.T) {
	reg := NewRegistry(slog.Default(), "", "")
	got := reg.FullModelID(RoleMain)
	want := "vllm/" + DefaultVllmModel
	if got != want {
		t.Errorf("empty mainModel: FullModelID(RoleMain) = %q, want %q", got, want)
	}
}

func TestFallbackChain(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model", "")

	tests := []struct {
		role Role
		want []Role
	}{
		{RoleMain, []Role{RoleMain, RoleLightweight, RoleFallback}},
		{RoleLightweight, []Role{RoleLightweight, RoleFallback}},
		{RoleFallback, []Role{RoleFallback}},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			got := reg.FallbackChain(tt.role)
			if len(got) != len(tt.want) {
				t.Fatalf("FallbackChain(%q) len = %d, want %d", tt.role, len(got), len(tt.want))
			}
			for i, r := range got {
				if r != tt.want[i] {
					t.Errorf("FallbackChain(%q)[%d] = %q, want %q", tt.role, i, r, tt.want[i])
				}
			}
		})
	}
}

func TestResolveLocalAIAPIKey(t *testing.T) {
	// Default: returns "local" when env var is not set.
	t.Setenv("LOCAL_AI_API_KEY", "")
	t.Setenv("SGLANG_API_KEY", "")
	if got := resolveLocalAIAPIKey(); got != "local" {
		t.Errorf("resolveLocalAIAPIKey() = %q, want %q", got, "local")
	}

	// Custom key from environment.
	t.Setenv("LOCAL_AI_API_KEY", "my-secret-key")
	if got := resolveLocalAIAPIKey(); got != "my-secret-key" {
		t.Errorf("resolveLocalAIAPIKey() = %q, want %q", got, "my-secret-key")
	}
}

func TestMimoProviderResolution(t *testing.T) {
	// The base provider uses the global API; the Token Plan variant uses
	// the Singapore subscription endpoint. Both speak Anthropic and share
	// one API key env var.
	tests := []struct {
		providerID string
		baseURL    string
	}{
		{"mimo", DefaultMimoBaseURL},
		{"mimo-plan", DefaultMimoPlanBaseURL},
	}
	for _, tt := range tests {
		if got := resolveBaseURL(tt.providerID); got != tt.baseURL {
			t.Errorf("resolveBaseURL(%q) = %q, want %q", tt.providerID, got, tt.baseURL)
		}
		if got := resolveAPIMode(tt.providerID); got != "anthropic" {
			t.Errorf("resolveAPIMode(%q) = %q, want %q", tt.providerID, got, "anthropic")
		}

		t.Setenv("XIAOMI_MIMO_API_KEY", "")
		if got := resolveAPIKey(tt.providerID); got != "" {
			t.Errorf("resolveAPIKey(%q) without env = %q, want empty", tt.providerID, got)
		}
		t.Setenv("XIAOMI_MIMO_API_KEY", "tp-secret")
		if got := resolveAPIKey(tt.providerID); got != "tp-secret" {
			t.Errorf("resolveAPIKey(%q) = %q, want %q", tt.providerID, got, "tp-secret")
		}
	}
}

func TestKimiProviderResolution(t *testing.T) {
	// Kimi Code resolves to Moonshot's Anthropic-compatible endpoint.
	if got := resolveBaseURL("kimi"); got != DefaultKimiBaseURL {
		t.Errorf("resolveBaseURL(kimi) = %q, want %q", got, DefaultKimiBaseURL)
	}
	if got := resolveAPIMode("kimi"); got != "anthropic" {
		t.Errorf("resolveAPIMode(kimi) = %q, want %q", got, "anthropic")
	}

	t.Setenv("KIMI_API_KEY", "")
	if got := resolveAPIKey("kimi"); got != "" {
		t.Errorf("resolveAPIKey(kimi) without env = %q, want empty", got)
	}
	t.Setenv("KIMI_API_KEY", "sk-kimi")
	if got := resolveAPIKey("kimi"); got != "sk-kimi" {
		t.Errorf("resolveAPIKey(kimi) = %q, want %q", got, "sk-kimi")
	}
}

func TestDefaultHeaders(t *testing.T) {
	// Coding-subscription providers get a coding-agent User-Agent.
	for _, providerID := range []string{"kimi", "mimo-plan"} {
		h := DefaultHeaders(providerID)
		if h["User-Agent"] != codingAgentUserAgent {
			t.Errorf("DefaultHeaders(%q)[User-Agent] = %q, want %q",
				providerID, h["User-Agent"], codingAgentUserAgent)
		}
	}
	// Non-subscription providers (incl. the MiMo global API) get nothing.
	for _, providerID := range []string{"mimo", "zai", "vllm", "openrouter"} {
		if h := DefaultHeaders(providerID); h != nil {
			t.Errorf("DefaultHeaders(%q) = %v, want nil", providerID, h)
		}
	}
	// The returned map is a fresh copy — mutating it must not affect the
	// next call.
	DefaultHeaders("kimi")["User-Agent"] = "tampered"
	if got := DefaultHeaders("kimi")["User-Agent"]; got != codingAgentUserAgent {
		t.Errorf("DefaultHeaders not isolated: got %q after mutation", got)
	}
}

func TestLogModelAlias(t *testing.T) {
	tests := []struct {
		name string
		cfg  ModelConfig
		want string
	}{
		{
			name: "plain model",
			cfg:  ModelConfig{ProviderID: "zai", Model: "glm-5-turbo"},
			want: "glm-5-turbo",
		},
		{
			name: "nested model path",
			cfg:  ModelConfig{ProviderID: "localai", Model: "google/gemma-4-26B-A4B-it"},
			want: "gemma-4-26B-A4B-it",
		},
		{
			name: "empty model falls back to provider",
			cfg:  ModelConfig{ProviderID: "google"},
			want: "google",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logModelAlias(tt.cfg); got != tt.want {
				t.Errorf("logModelAlias(%+v) = %q, want %q", tt.cfg, got, tt.want)
			}
		})
	}
}
