package modelrole

import (
	"log/slog"
	"testing"
)

// TestRoleForModelAcceptsBareModelID is the usage-label regression. The agent
// log records requestedModel without a provider prefix, so a registry that only
// matched the qualified form mapped no chat run back to its role and the usage
// screen fell back to printing raw model ids.
func TestRoleForModelAcceptsBareModelID(t *testing.T) {
	reg := NewRegistryWithOptions(slog.Default(), RegistryOptions{
		MainModel:     "kimi/k3",
		FallbackModel: "wormhole/deepseek-v4-flash",
		TinyModel:     "wormhole/dsv4-nothink",
		SubmainModel:  "openrouter/anthropic/claude-opus-4.7",
	})

	for _, tc := range []struct {
		name  string
		id    string
		want  Role
		found bool
	}{
		{"bare fallback", "deepseek-v4-flash", RoleFallback, true},
		{"qualified fallback", "wormhole/deepseek-v4-flash", RoleFallback, true},
		{"bare main", "k3", RoleMain, true},
		{"bare tiny", "dsv4-nothink", RoleTiny, true},
		{"unknown bare", "not-a-model", "", false},
		// A qualified id naming a real model under the wrong provider must stay
		// unmatched: it equals neither that role's qualified id nor its bare
		// model name.
		{"wrong provider", "openrouter/deepseek-v4-flash", "", false},
		// HF/vLLM-style model names carry their own slash: after ParseModelID
		// strips the provider, the logged id still contains one and the old
		// slash gate skipped the bare comparison entirely.
		{"model name with its own slash", "anthropic/claude-opus-4.7", RoleSubmain, true},
		{"fully qualified slash-model", "openrouter/anthropic/claude-opus-4.7", RoleSubmain, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			role, ok := reg.RoleForModel(tc.id)
			if ok != tc.found || role != tc.want {
				t.Fatalf("RoleForModel(%q) = (%q, %v), want (%q, %v)", tc.id, role, ok, tc.want, tc.found)
			}
		})
	}
}
