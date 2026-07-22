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

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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

func TestResolveModelReturnsRoleOrPassthrough(t *testing.T) {
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
		{"coding", "coding", "", false},
		{"submain", "submain", "", false},
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

func TestResolveModelResolvesCodingWhenConfigured(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:   "zai/test-model",
		CodingModel: "kimi/kimi-for-coding",
	})

	gotID, gotRole, gotOK := reg.ResolveModel("coding")
	if !gotOK {
		t.Fatal("ResolveModel(coding) ok = false, want true when coding role is configured")
	}
	if gotID != "kimi/kimi-for-coding" {
		t.Errorf("ResolveModel(coding) id = %q, want kimi/kimi-for-coding", gotID)
	}
	if gotRole != RoleCoding {
		t.Errorf("ResolveModel(coding) role = %q, want %q", gotRole, RoleCoding)
	}
}

// Submain is opt-in like coding/main2/vision: it resolves to a model ID only when
// agents.submainModel is configured; otherwise the literal "submain" passes through
// so an unconfigured deploy keeps autonomous work on the main role.
func TestResolveModelResolvesSubmainWhenConfigured(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:    "zai/test-model",
		SubmainModel: "zai/glm-5.2",
	})

	gotID, gotRole, gotOK := reg.ResolveModel("submain")
	if !gotOK {
		t.Fatal("ResolveModel(submain) ok = false, want true when submain role is configured")
	}
	if gotID != "zai/glm-5.2" {
		t.Errorf("ResolveModel(submain) id = %q, want zai/glm-5.2", gotID)
	}
	if gotRole != RoleSubmain {
		t.Errorf("ResolveModel(submain) role = %q, want %q", gotRole, RoleSubmain)
	}
}

// Unconfigured submain stays absent: "submain" falls through as a raw model name,
// the role has no config/client, and the fallback walk skips it — autonomous work
// runs on the main role exactly as before.
func TestNewRegistryWithOptions_SubmainAbsentByDefault(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "zai/main-model",
	})
	if cfg := reg.Config(RoleSubmain); cfg.Model != "" {
		t.Fatalf("unconfigured submain = %+v, want absent", cfg)
	}
	if _, _, ok := reg.ResolveModel("submain"); ok {
		t.Fatalf("ResolveModel(submain) resolved for unconfigured role")
	}
	if c := reg.Client(RoleSubmain); c != nil {
		t.Fatalf("unconfigured submain has a client")
	}
}

func TestRoleForModelReturnsFirstMatchingRole(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:   "zai/test-model",
		CodingModel: "kimi/kimi-for-coding",
	})

	tests := []struct {
		fullModelID string
		wantRole    Role
		wantFound   bool
	}{
		{"zai/test-model", RoleMain, true},
		{"kimi/kimi-for-coding", RoleCoding, true},
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

func TestNewRegistryWithOptions_PerRoleAndCatalog(t *testing.T) {
	const googleURL = "https://gen.googleapis.example/v1beta/openai"
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "zai/main-model",
		LightweightModel: "google/gemini-3.5-flash", // catalog provider
		FallbackModel:    "openrouter/some-model",   // built-in switch provider
		Providers: map[string]ProviderResolved{
			"google": {BaseURL: googleURL, APIKey: "gkey", APIMode: "openai"},
		},
	})

	// Lightweight resolves its endpoint from the provider catalog — NOT the
	// zai default that the built-in switch would return for "google".
	lw := reg.Config(RoleLightweight)
	if lw.ProviderID != "google" || lw.Model != "gemini-3.5-flash" {
		t.Errorf("lightweight = %s/%s, want google/gemini-3.5-flash", lw.ProviderID, lw.Model)
	}
	if lw.BaseURL != googleURL {
		t.Errorf("lightweight baseURL = %q, want catalog URL %q (regression: fell back to zai?)", lw.BaseURL, googleURL)
	}
	if lw.APIKey != "gkey" {
		t.Errorf("lightweight apiKey = %q, want catalog key", lw.APIKey)
	}

	// Fallback's provider isn't in the catalog → built-in switch resolves it.
	fb := reg.Config(RoleFallback)
	if fb.ProviderID != "openrouter" || fb.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("fallback = %s @ %q, want openrouter @ built-in URL", fb.ProviderID, fb.BaseURL)
	}

	// SetRoleModelID re-resolves at runtime.
	reg.SetRoleModelID(RoleLightweight, "openrouter/swapped")
	if got := reg.FullModelID(RoleLightweight); got != "openrouter/swapped" {
		t.Errorf("after SetRoleModelID: FullModelID(RoleLightweight) = %q, want openrouter/swapped", got)
	}
	if got := reg.Config(RoleLightweight).BaseURL; got != "https://openrouter.ai/api/v1" {
		t.Errorf("after SetRoleModelID: baseURL = %q, want built-in openrouter URL", got)
	}
}

// TestNewRegistryWithOptions_KimiBehindWormhole pins the config-only contract
// that lets the anthropic-only Kimi endpoint ride the wormhole /v1/messages
// front (sidecar-models.md § 클라우드 호출 통합): the deneb.json provider keeps
// the builtin id "kimi" — so RejectsCacheControl marker-stripping and the
// builtin anthropic API mode keep working — while the catalog overrides only
// the endpoint (local wormhole, no /v1: the anthropic client appends
// /v1/messages itself) and the key (wormhole token). If catalog resolution
// ever stops layering over builtin defaults this way, the runbook breaks here
// first.
func TestNewRegistryWithOptions_KimiBehindWormhole(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:   "zai/main-model",
		CodingModel: "kimi/kimi-for-coding",
		Providers: map[string]ProviderResolved{
			"kimi": {BaseURL: "http://127.0.0.1:18800", APIKey: "wormhole-token"},
		},
	})
	cfg := reg.Config(RoleCoding)
	if cfg.ProviderID != "kimi" || cfg.Model != "kimi-for-coding" {
		t.Fatalf("coding = %s/%s, want kimi/kimi-for-coding", cfg.ProviderID, cfg.Model)
	}
	if cfg.BaseURL != "http://127.0.0.1:18800" {
		t.Errorf("baseURL = %q, want the wormhole front (catalog override)", cfg.BaseURL)
	}
	if cfg.APIKey != "wormhole-token" {
		t.Errorf("apiKey = %q, want the wormhole token (catalog override)", cfg.APIKey)
	}
	if cfg.APIMode != llm.APIModeAnthropic {
		t.Errorf("apiMode = %q, want the builtin anthropic default to survive a catalog entry without an explicit api field", cfg.APIMode)
	}
}

// Main1/main2 mutual failover pair: main2 is opt-in, resolves like other
// roles, and each main's chain starts with the other main (same-tier quality
// before degrading to lightweight).
func TestNewRegistryWithOptions_Main2MutualFailoverPair(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:  "kimi/kimi-for-coding",
		Main2Model: "zai/glm-5.2",
		Providers: map[string]ProviderResolved{
			"kimi": {BaseURL: "http://127.0.0.1:18800", APIKey: "t", APIMode: "anthropic"},
			"zai":  {BaseURL: "https://api.z.ai/api/anthropic", APIKey: "k", APIMode: "anthropic"},
		},
	})

	if cfg := reg.Config(RoleMain2); cfg.Model != "glm-5.2" || cfg.ProviderID != "zai" {
		t.Fatalf("main2 config = %+v", cfg)
	}
	// Role-name resolution: "main2" resolves when configured.
	if id, role, ok := reg.ResolveModel("main2"); !ok || role != RoleMain2 || id != "zai/glm-5.2" {
		t.Fatalf("ResolveModel(main2) = (%s, %s, %v)", id, role, ok)
	}
	// Chain heads: main → main2 first, main2 → main first.
	if chain := reg.FallbackChain(RoleMain); chain[1] != RoleMain2 {
		t.Fatalf("main chain = %v", chain)
	}
	if chain := reg.FallbackChain(RoleMain2); chain[1] != RoleMain {
		t.Fatalf("main2 chain = %v", chain)
	}
}

// Unconfigured main2 stays absent: "main2" falls through as a raw model name
// and the role has no config/client, so the fallback walk skips it.
func TestNewRegistryWithOptions_Main2AbsentByDefault(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel: "zai/main-model",
	})
	if cfg := reg.Config(RoleMain2); cfg.Model != "" {
		t.Fatalf("unconfigured main2 = %+v, want absent", cfg)
	}
	if _, _, ok := reg.ResolveModel("main2"); ok {
		t.Fatalf("ResolveModel(main2) resolved for unconfigured role")
	}
	if c := reg.Client(RoleMain2); c != nil {
		t.Fatalf("unconfigured main2 has a client")
	}
}

func TestNewRegistryWithOptions_UnsetRolesDefaultToVllm(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:      "zai/main-model",
		LocalVllmModel: "qwen-test",
	})
	for _, role := range []Role{RoleLightweight, RoleFallback} {
		cfg := reg.Config(role)
		if cfg.ProviderID != "vllm" || cfg.Model != "qwen-test" {
			t.Errorf("%s = %s/%s, want vllm/qwen-test (default)", role, cfg.ProviderID, cfg.Model)
		}
	}
}

func TestFallbackChain(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model", "")

	tests := []struct {
		role Role
		want []Role
	}{
		// Main2 sits in the chain unconditionally; the fallback walk skips it
		// when unconfigured (nil client), preserving single-main behavior.
		// Same-tier quality ladder before local degradation: main2 (opt-in),
		// then the coding subscription, then lightweight (operator call
		// 2026-07-17: kimi outage must land on glm coding, not local).
		{RoleMain, []Role{RoleMain, RoleMain2, RoleCoding, RoleLightweight, RoleFallback}},
		{RoleMain2, []Role{RoleMain2, RoleMain, RoleCoding, RoleLightweight, RoleFallback}},
		{RoleCoding, []Role{RoleCoding, RoleMain, RoleFallback}},
		{RoleSubmain, []Role{RoleSubmain, RoleMain, RoleMain2, RoleFallback}},
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

func TestResolveLocalAIAPIKeyDefaultsOrLoadsEnv(t *testing.T) {
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

func TestMimoProviderResolutionReturnsPerVariantConfig(t *testing.T) {
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

func TestKimiProviderResolutionReturnsAnthropicEndpoint(t *testing.T) {
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

func TestDefaultHeadersReturnsIsolatedCopyPerProvider(t *testing.T) {
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

func TestResolveAuthSchemeReturnsBearerForSubscriptions(t *testing.T) {
	// Coding-subscription providers authenticate with Bearer tokens.
	for _, providerID := range []string{"kimi", "mimo", "mimo-plan"} {
		if got := ResolveAuthScheme(providerID); got != "bearer" {
			t.Errorf("ResolveAuthScheme(%q) = %q, want bearer", providerID, got)
		}
	}
	// Other Anthropic-mode providers keep the default x-api-key scheme.
	for _, providerID := range []string{"zai", "vllm", "openrouter", "localai"} {
		if got := ResolveAuthScheme(providerID); got != "" {
			t.Errorf("ResolveAuthScheme(%q) = %q, want empty", providerID, got)
		}
	}
}

func TestClientForProviderReturnsClientOrNil(t *testing.T) {
	reg := NewRegistry(nil, "zai/glm-5-turbo", "")

	// Known built-in providers build a client on demand.
	for _, providerID := range []string{"zai", "vllm", "openrouter", "mimo", "mimo-plan", "kimi"} {
		if reg.ClientForProvider(providerID) == nil {
			t.Errorf("ClientForProvider(%q) = nil, want a client", providerID)
		}
	}

	// Unknown providers and the empty string return nil.
	for _, providerID := range []string{"nonexistent", ""} {
		if reg.ClientForProvider(providerID) != nil {
			t.Errorf("ClientForProvider(%q) != nil, want nil", providerID)
		}
	}
}

func TestLogModelAliasFormatsShortName(t *testing.T) {
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

// TestVllmBaseURLsReturnsDedupedVllmEndpoints feeds the observation plane's
// /metrics scrape: vllm-provider roles only, deduped, main first,
// anthropic-mode endpoints excluded.
func TestVllmBaseURLsReturnsDedupedVllmEndpoints(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "vllm/dsv4-flash",
		LightweightModel: "vllm/qwen-lite",   // same catalog endpoint → deduped
		FallbackModel:    "mimo-plan/mimo-x", // not the vllm provider → skipped
		Providers: map[string]ProviderResolved{
			"vllm": {BaseURL: "http://10.77.0.2:8000/v1"},
		},
	})
	got := reg.VllmBaseURLs()
	if len(got) != 1 || got[0] != "http://10.77.0.2:8000/v1" {
		t.Errorf("VllmBaseURLs = %v, want exactly the deduped catalog URL", got)
	}

	// No vllm-provider role anywhere → nil (the scrape is silently disabled).
	noVllm := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "zai/a",
		LightweightModel: "zai/b",
		FallbackModel:    "zai/c",
	})
	if urls := noVllm.VllmBaseURLs(); len(urls) != 0 {
		t.Errorf("VllmBaseURLs on a vllm-free registry = %v, want none", urls)
	}

	// A vllm provider entry forced to anthropic mode (some proxy) is not a
	// scrape target — /metrics is the OpenAI-server surface.
	proxied := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:        "vllm/x",
		LightweightModel: "zai/b",
		FallbackModel:    "zai/c",
		Providers: map[string]ProviderResolved{
			"vllm": {BaseURL: "http://10.77.0.3:8000/v1", APIMode: llm.APIModeAnthropic},
		},
	})
	if urls := proxied.VllmBaseURLs(); len(urls) != 0 {
		t.Errorf("VllmBaseURLs with anthropic-mode vllm provider = %v, want none", urls)
	}
}
