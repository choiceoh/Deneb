// miniapp_models_providers.go — provider catalog helpers shared by the
// model picker: display names, builtin provider specs, configured-provider
// loading, base-URL/local-URL resolution, and model list merging. Split
// from miniapp_models.go (RPC surface).
package server

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// --- Model-picker shared helpers (previously in inbound_models.go) ---

// providerDisplayName returns a human-friendly label for a provider key.
func providerDisplayName(name string) string {
	switch name {
	case "zai":
		return "Z.ai"
	case "vllm":
		return "vLLM"
	case "localai":
		return "LocalAI"
	case "openrouter":
		return "OpenRouter"
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	case "kimi":
		return "Kimi Code"
	case "mimo":
		return "MiMo"
	case "mimo-plan":
		return "MiMo Token Plan"
	}
	if name == "custom" || strings.HasPrefix(name, "custom-") {
		return "직접 추가"
	}
	return name
}

// builtinProviders lists the well-known providers Deneb ships with.
func builtinProviders() []providerSpec {
	return []providerSpec{
		{name: "zai", models: []string{"glm-5.2"}},
		{name: "openrouter", models: []string{
			"anthropic/claude-opus-4.7",
			"anthropic/claude-sonnet-4.6",
			"google/gemini-3.1-pro",
		}},
		{name: "vllm"},
		{name: "localai"},
		{name: "kimi", models: []string{"kimi-for-coding"}},
		{name: "mimo-plan", models: []string{"mimo-v2.5-pro"}},
	}
}

// appendBuiltinProviders merges built-in providers with operator-configured
// ones (explicit config wins).
func appendBuiltinProviders(configured []providerSpec) []providerSpec {
	builtin := builtinProviders()
	builtinByName := make(map[string]providerSpec, len(builtin))
	for _, b := range builtin {
		builtinByName[b.name] = b
	}
	have := make(map[string]struct{}, len(configured))
	for i := range configured {
		have[configured[i].name] = struct{}{}
		if len(configured[i].models) == 0 {
			if b, ok := builtinByName[configured[i].name]; ok && len(b.models) > 0 {
				configured[i].models = append([]string(nil), b.models...)
			}
		}
	}
	for _, b := range builtin {
		if _, ok := have[b.name]; !ok {
			configured = append(configured, b)
		}
	}
	sort.Slice(configured, func(i, j int) bool { return configured[i].name < configured[j].name })
	return configured
}

// isLocalURL reports whether a base URL points at a loopback host.
func isLocalURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// effectiveBaseURL returns the provider's base URL, falling back to known defaults.
func effectiveBaseURL(spec providerSpec) string {
	if spec.baseURL != "" {
		return spec.baseURL
	}
	switch spec.name {
	case "vllm":
		return modelrole.DefaultVllmBaseURL
	case "localai":
		return modelrole.DefaultLocalAIBaseURL
	case "zai":
		return modelrole.DefaultZaiBaseURL
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "kimi":
		return modelrole.DefaultKimiBaseURL
	case "mimo":
		return modelrole.DefaultMimoBaseURL
	case "mimo-plan":
		return modelrole.DefaultMimoPlanBaseURL
	}
	return ""
}

// loadConfiguredProviders reads models.providers from deneb.json.
func loadConfiguredProviders() []providerSpec {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}
	var root struct {
		Models struct {
			Providers map[string]struct {
				BaseURL string `json:"baseUrl"`
				APIKey  string `json:"apiKey"`
				Models  []struct {
					ID string `json:"id"`
				} `json:"models"`
			} `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return nil
	}
	specs := make([]providerSpec, 0, len(root.Models.Providers))
	for name, pc := range root.Models.Providers {
		spec := providerSpec{
			name:    name,
			baseURL: strings.TrimSpace(pc.BaseURL),
			apiKey:  os.ExpandEnv(strings.TrimSpace(pc.APIKey)), // expand ${ENV} like the chat path
		}
		for _, m := range pc.Models {
			if id := strings.TrimSpace(m.ID); id != "" {
				spec.models = append(spec.models, id)
			}
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].name < specs[j].name })
	return specs
}

// mergeModels concatenates configured + discovered model ids, de-duplicating.
func mergeModels(configured, discovered []string) []string {
	seen := make(map[string]struct{}, len(configured)+len(discovered))
	var out []string
	for _, group := range [][]string{configured, discovered} {
		for _, m := range group {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

// providerEntries builds the model entries for one provider section.
func providerEntries(spec providerSpec) []modelEntry {
	entries := make([]modelEntry, 0, len(spec.models))
	for _, m := range spec.models {
		short := shortModelName(m)
		entries = append(entries, modelEntry{
			provider: spec.name,
			label:    short,
			fullID:   spec.name + "/" + m,
			display:  short,
		})
	}
	return entries
}

// registryRoleEntries builds the role-based section (main/lightweight/fallback).
func registryRoleEntries(reg *modelrole.Registry, liveMain string) []modelEntry {
	if reg == nil {
		return nil
	}
	roles := []struct {
		role  modelrole.Role
		label string
	}{
		{modelrole.RoleMain, "main"},
		{modelrole.RoleLightweight, "lightweight"},
		{modelrole.RoleFallback, "fallback"},
	}
	var entries []modelEntry
	for _, r := range roles {
		if r.role == modelrole.RoleMain {
			if live := strings.TrimSpace(liveMain); live != "" {
				providerID, _ := modelrole.ParseModelID(live)
				entries = append(entries, modelEntry{
					provider: providerID,
					label:    r.label + ": " + shortModelName(live),
					fullID:   live,
					display:  shortModelName(live),
				})
				continue
			}
		}
		cfg := reg.Config(r.role)
		if cfg.Model == "" {
			continue
		}
		entries = append(entries, modelEntry{
			provider: cfg.ProviderID,
			label:    r.label + ": " + shortModelName(cfg.Model),
			fullID:   reg.FullModelID(r.role),
			display:  shortModelName(cfg.Model),
		})
	}
	return entries
}

// discoverProviderModels probes an OpenAI-compatible /models endpoint. apiKey is
// sent as a Bearer token for endpoints that gate /models (the wormhole router);
// empty for keyless local vLLM.
func discoverProviderModels(ctx context.Context, baseURL, apiKey string) []string {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	ids, err := modelrole.DiscoverServedVllmModels(ctx, baseURL, apiKey)
	if err != nil || len(ids) == 0 {
		return nil
	}
	if len(ids) > maxModelsPerProvider {
		ids = ids[:maxModelsPerProvider]
	}
	return ids
}
