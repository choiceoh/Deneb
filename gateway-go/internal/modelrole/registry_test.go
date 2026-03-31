package modelrole

import (
	"log/slog"
	"testing"
)

func TestResolveModel(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model")

	tests := []struct {
		input    string
		wantID   string
		wantRole Role
		wantOK   bool
	}{
		// Role names resolve to full model IDs.
		{"main", "zai/test-model", RoleMain, true},
		{"lightweight", "sglang/" + DefaultSglangModel, RoleLightweight, true},
		{"pilot", "google/" + DefaultPilotModel, RolePilot, true},
		{"fallback", "google/" + DefaultFallbackModel, RoleFallback, true},
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
	reg := NewRegistry(slog.Default(), "zai/test-model")

	tests := []struct {
		fullModelID string
		wantRole    Role
		wantFound   bool
	}{
		{"zai/test-model", RoleMain, true},
		{"sglang/" + DefaultSglangModel, RoleLightweight, true},
		{"google/" + DefaultPilotModel, RolePilot, true},
		{"google/" + DefaultFallbackModel, RoleFallback, true},
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

func TestEmptyMainModelDefaultsToZai(t *testing.T) {
	reg := NewRegistry(slog.Default(), "")
	got := reg.FullModelID(RoleMain)
	want := "zai/" + DefaultZaiModel
	if got != want {
		t.Errorf("empty mainModel: FullModelID(RoleMain) = %q, want %q", got, want)
	}
}

func TestResolveBaseURLVllm(t *testing.T) {
	if got := resolveBaseURL("vllm"); got != DefaultVllmBaseURL {
		t.Fatalf("resolveBaseURL(%q) = %q, want %q", "vllm", got, DefaultVllmBaseURL)
	}
}

func TestFallbackChain(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model")

	tests := []struct {
		role Role
		want []Role
	}{
		{RoleMain, []Role{RoleMain, RoleLightweight, RoleFallback}},
		{RoleLightweight, []Role{RoleLightweight, RoleFallback}},
		{RolePilot, []Role{RolePilot, RoleFallback}},
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
			cfg:  ModelConfig{ProviderID: "sglang", Model: "Qwen/Qwen3.5-35B-A3B"},
			want: "Qwen3.5-35B-A3B",
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
