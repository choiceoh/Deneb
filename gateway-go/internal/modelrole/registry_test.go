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
		{"fallback", "google/" + DefaultFallbackModel, RoleFallback, true},
		{"image", "google/" + DefaultImageModel, RoleImage, true},
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
		{"google/" + DefaultFallbackModel, RoleFallback, true}, // fallback and image share the same model
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

func TestFallbackChain(t *testing.T) {
	reg := NewRegistry(slog.Default(), "zai/test-model")

	tests := []struct {
		role Role
		want []Role
	}{
		{RoleMain, []Role{RoleMain, RoleLightweight, RoleFallback}},
		{RoleLightweight, []Role{RoleLightweight, RoleFallback}},
		{RoleImage, []Role{RoleImage, RoleFallback}},
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
