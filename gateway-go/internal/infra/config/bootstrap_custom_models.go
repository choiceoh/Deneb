// bootstrap_custom_models.go — custom provider model management for the gateway
// config: persisting/deleting user-added OpenAI-compatible endpoint models under
// models.providers, plus the endpoint/ID normalization and role-clearing helpers.
// Split from bootstrap.go (pure move, no behavior change).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

var ErrInvalidCustomModel = errors.New("invalid custom model")

// PersistedCustomModel describes the provider/model entry written to config.
type PersistedCustomModel struct {
	ProviderID  string
	ModelID     string
	FullModelID string
	BaseURL     string
	Added       bool
}

// CustomModelMeta carries optional metadata written alongside a custom model
// entry so models added via the Mini App are complete (matching hand-authored
// entries) instead of bare {"id": ...} stubs. Zero/empty fields are omitted.
type CustomModelMeta struct {
	ContextWindow int    // advertised context length in tokens; 0 = omit
	Name          string // display label; "" = omit
}

// PersistCustomProviderModel writes an OpenAI-compatible endpoint + model into
// models.providers, preserving all other config fields. The newest direct model
// is kept first so it remains visible even when provider lists are capped.
// Optional meta (context window, display name) is written onto the entry and
// backfills any of those fields missing from an already-present entry.
func PersistCustomProviderModel(configPath, endpoint, model string, meta CustomModelMeta, logger *slog.Logger) (PersistedCustomModel, error) {
	baseURL, err := normalizeCustomModelEndpoint(endpoint)
	if err != nil {
		return PersistedCustomModel{}, err
	}
	modelID, err := normalizeCustomModelID(model)
	if err != nil {
		return PersistedCustomModel{}, err
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PersistedCustomModel{}, fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return PersistedCustomModel{}, fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return PersistedCustomModel{}, fmt.Errorf("parsing config: %w", err)
		}
	}

	models := ensureObject(raw, "models")
	providers := ensureObject(models, "providers")

	providerID, providerConfig := providerForCustomEndpoint(providers, baseURL)
	if providerConfig == nil {
		providerID = nextCustomProviderID(providers)
		providerConfig = map[string]any{
			"baseUrl": baseURL,
			"api":     "openai",
		}
		providers[providerID] = providerConfig
	}
	added := upsertCustomModel(providerConfig, modelID, meta)

	metaObj := ensureObject(raw, "meta")
	metaObj["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return PersistedCustomModel{}, fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return PersistedCustomModel{}, fmt.Errorf("writing config: %w", err)
	}

	result := PersistedCustomModel{
		ProviderID:  providerID,
		ModelID:     modelID,
		FullModelID: providerID + "/" + modelID,
		BaseURL:     baseURL,
		Added:       added,
	}
	if logger != nil {
		logger.Info("persisted custom model", "provider", providerID, "model", modelID, "path", configPath)
	}
	return result, nil
}

// DeletedCustomModel reports the outcome of removing a custom model entry.
type DeletedCustomModel struct {
	ProviderID      string   // provider the model belonged to
	ModelID         string   // model name removed
	FullModelID     string   // provider/model
	Removed         bool     // a matching model entry was found and removed
	ProviderDropped bool     // provider had no models left and was deleted
	ClearedRoles    []string // agents role fields cleared because they pointed here
}

// DeleteCustomProviderModel removes a user-added model from models.providers,
// preserving every other config field. Only custom providers (custom, custom-N)
// are user-managed and therefore deletable; built-in/role model IDs return
// ErrInvalidCustomModel. When the provider's model list becomes empty the whole
// provider entry is dropped. Any agents.{default,lightweight,fallback}Model that
// referenced the deleted model is cleared so the role falls back to its default
// on the next registry build (the live registry is reset separately by the
// caller). This is the inverse of PersistCustomProviderModel.
func DeleteCustomProviderModel(configPath, fullModelID string, logger *slog.Logger) (DeletedCustomModel, error) {
	providerID, modelID, err := splitCustomModelID(fullModelID)
	if err != nil {
		return DeletedCustomModel{}, err
	}

	result := DeletedCustomModel{
		ProviderID:  providerID,
		ModelID:     modelID,
		FullModelID: providerID + "/" + modelID,
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil // nothing persisted yet — idempotent no-op
		}
		return DeletedCustomModel{}, fmt.Errorf("reading config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return DeletedCustomModel{}, fmt.Errorf("parsing config: %w", err)
	}

	// Remove the model from its provider, dropping the provider when empty.
	if models, ok := raw["models"].(map[string]any); ok {
		if providers, ok := models["providers"].(map[string]any); ok {
			if providerConfig, ok := providers[providerID].(map[string]any); ok {
				if removeCustomModel(providerConfig, modelID) {
					result.Removed = true
					if customModelCount(providerConfig) == 0 {
						delete(providers, providerID)
						result.ProviderDropped = true
					}
				}
			}
		}
	}

	// Clear any role binding that pointed at the deleted model so the role
	// resolves to its default again instead of a now-missing model.
	result.ClearedRoles = clearRolesReferencingModel(raw, result.FullModelID)

	if !result.Removed && len(result.ClearedRoles) == 0 {
		return result, nil // nothing changed — skip the write
	}

	meta := ensureObject(raw, "meta")
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return DeletedCustomModel{}, fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return DeletedCustomModel{}, fmt.Errorf("writing config: %w", err)
	}

	if logger != nil {
		logger.Info("deleted custom model",
			"provider", providerID, "model", modelID,
			"providerDropped", result.ProviderDropped,
			"clearedRoles", result.ClearedRoles, "path", configPath)
	}
	return result, nil
}

func normalizeCustomModelEndpoint(endpoint string) (string, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return "", fmt.Errorf("%w: endpoint is required", ErrInvalidCustomModel)
	}
	if !strings.Contains(raw, "://") {
		return "", fmt.Errorf("%w: endpoint must include http:// or https://", ErrInvalidCustomModel)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: endpoint URL is invalid", ErrInvalidCustomModel)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("%w: endpoint must use http or https", ErrInvalidCustomModel)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%w: endpoint host is required", ErrInvalidCustomModel)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: endpoint must not include query or fragment", ErrInvalidCustomModel)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func normalizeCustomModelID(model string) (string, error) {
	id := strings.TrimSpace(model)
	if id == "" {
		return "", fmt.Errorf("%w: model name is required", ErrInvalidCustomModel)
	}
	if strings.HasPrefix(id, "/") || strings.HasSuffix(id, "/") {
		return "", fmt.Errorf("%w: model name must not start or end with /", ErrInvalidCustomModel)
	}
	if strings.ContainsFunc(id, unicode.IsSpace) {
		return "", fmt.Errorf("%w: model name must not contain whitespace", ErrInvalidCustomModel)
	}
	return id, nil
}

func ensureObject(parent map[string]any, key string) map[string]any {
	if obj, ok := parent[key].(map[string]any); ok {
		return obj
	}
	obj := make(map[string]any)
	parent[key] = obj
	return obj
}

func providerForCustomEndpoint(providers map[string]any, baseURL string) (string, map[string]any) {
	keys := make([]string, 0, len(providers))
	for id := range providers {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		rawProvider := providers[id]
		providerConfig, ok := rawProvider.(map[string]any)
		if !ok {
			continue
		}
		existingBase, ok := providerConfig["baseUrl"].(string)
		if !ok {
			continue
		}
		normalized, err := normalizeCustomModelEndpoint(existingBase)
		if err != nil {
			continue
		}
		if normalized == baseURL {
			return id, providerConfig
		}
	}
	return "", nil
}

func nextCustomProviderID(providers map[string]any) string {
	for i := 1; ; i++ {
		id := "custom"
		if i > 1 {
			id = fmt.Sprintf("custom-%d", i)
		}
		if _, exists := providers[id]; !exists {
			return id
		}
	}
}

func upsertCustomModel(providerConfig map[string]any, modelID string, meta CustomModelMeta) bool {
	var existing []any
	if arr, ok := providerConfig["models"].([]any); ok {
		existing = arr
	}

	added := true
	front := any(buildCustomModelEntry(modelID, meta))
	next := []any{}
	for _, item := range existing {
		if existingID := customModelID(item); existingID == modelID {
			added = false
			front = enrichCustomModelEntry(item, modelID, meta)
			continue
		}
		next = append(next, item)
	}
	next = append([]any{front}, next...)
	providerConfig["models"] = next
	return added
}

// buildCustomModelEntry creates a model entry including any provided metadata,
// so the persisted shape matches hand-authored entries ({contextWindow, id,
// name}) instead of a bare {"id": ...} stub. Empty/zero meta fields are omitted.
func buildCustomModelEntry(modelID string, meta CustomModelMeta) map[string]any {
	entry := map[string]any{"id": modelID}
	if name := strings.TrimSpace(meta.Name); name != "" {
		entry["name"] = name
	}
	if meta.ContextWindow > 0 {
		entry["contextWindow"] = meta.ContextWindow
	}
	return entry
}

// enrichCustomModelEntry backfills newly-detected metadata onto an existing
// entry without clobbering values the operator already set by hand. A bare
// string or otherwise non-object entry is replaced with a full entry.
func enrichCustomModelEntry(existing any, modelID string, meta CustomModelMeta) map[string]any {
	obj, ok := existing.(map[string]any)
	if !ok {
		return buildCustomModelEntry(modelID, meta)
	}
	if _, has := obj["name"]; !has {
		if name := strings.TrimSpace(meta.Name); name != "" {
			obj["name"] = name
		}
	}
	if _, has := obj["contextWindow"]; !has && meta.ContextWindow > 0 {
		obj["contextWindow"] = meta.ContextWindow
	}
	return obj
}

func customModelID(item any) string {
	switch v := item.(type) {
	case map[string]any:
		if id, ok := v["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

// splitCustomModelID parses "provider/model" and verifies the provider is a
// user-managed custom provider. Built-in/role model IDs are rejected so the
// delete path can never remove a model the user did not add by hand.
func splitCustomModelID(fullModelID string) (providerID, modelID string, err error) {
	full := strings.TrimSpace(fullModelID)
	if full == "" {
		return "", "", fmt.Errorf("%w: model id is required", ErrInvalidCustomModel)
	}
	slash := strings.IndexByte(full, '/')
	if slash <= 0 || slash >= len(full)-1 {
		return "", "", fmt.Errorf("%w: model id must be provider/model", ErrInvalidCustomModel)
	}
	providerID, modelID = full[:slash], full[slash+1:]
	if !isCustomProviderID(providerID) {
		return "", "", fmt.Errorf("%w: only directly-added custom models can be deleted", ErrInvalidCustomModel)
	}
	return providerID, modelID, nil
}

// isCustomProviderID reports whether a provider key is one created by the Mini
// App's "직접 추가" flow (custom, custom-2, ...).
func isCustomProviderID(id string) bool {
	return id == "custom" || strings.HasPrefix(id, "custom-")
}

// removeCustomModel drops modelID from providerConfig["models"], reporting
// whether an entry was actually removed.
func removeCustomModel(providerConfig map[string]any, modelID string) bool {
	existing, ok := providerConfig["models"].([]any)
	if !ok {
		return false
	}
	next := make([]any, 0, len(existing))
	removed := false
	for _, item := range existing {
		if customModelID(item) == modelID {
			removed = true
			continue
		}
		next = append(next, item)
	}
	if removed {
		providerConfig["models"] = next
	}
	return removed
}

// customModelCount returns how many model entries remain on a provider.
func customModelCount(providerConfig map[string]any) int {
	if arr, ok := providerConfig["models"].([]any); ok {
		return len(arr)
	}
	return 0
}

// clearRolesReferencingModel deletes any agents.{default,tiny,lightweight,analysis,
// fallback}Model field equal to fullModelID and returns the affected modelrole role
// names, so the caller can reset the live registry. Mirrors PersistRoleModel's mapping.
func clearRolesReferencingModel(raw map[string]any, fullModelID string) []string {
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return nil
	}
	fields := []struct{ field, role string }{
		{"defaultModel", "main"},
		{"tinyModel", "tiny"},
		{"lightweightModel", "lightweight"},
		{"analysisModel", "analysis"},
		{"fallbackModel", "fallback"},
	}
	var cleared []string
	for _, f := range fields {
		if cur, ok := agents[f.field].(string); ok && strings.TrimSpace(cur) == fullModelID {
			delete(agents, f.field)
			cleared = append(cleared, f.role)
		}
	}
	return cleared
}

// HiddenModel reports the outcome of soft-hiding a built-in/cloud catalog model.
type HiddenModel struct {
	FullModelID  string   // provider/model that was hidden
	Hidden       bool     // present in models.hiddenModels after the call
	ClearedRoles []string // roles cleared because they pointed at the hidden model
}

// HideModel soft-hides a model id by adding it to models.hiddenModels so the
// picker filters it out. Built-in cloud-catalog models (openrouter/zai/kimi/…)
// can't be removed from config the way custom models can — they are re-merged
// from the shipped catalog on every build (appendBuiltinProviders) — so
// "deleting" one persists as a hide entry instead. Idempotent: re-hiding an
// already-hidden id writes only if a stale role binding still needs clearing.
// Roles pointing at the hidden model are cleared so they fall back to default.
func HideModel(configPath, fullModelID string, logger *slog.Logger) (HiddenModel, error) {
	id := strings.TrimSpace(fullModelID)
	if slash := strings.IndexByte(id, '/'); id == "" || slash <= 0 || slash >= len(id)-1 {
		return HiddenModel{}, fmt.Errorf("%w: model id must be provider/model", ErrInvalidCustomModel)
	}
	result := HiddenModel{FullModelID: id}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return HiddenModel{}, fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else if err := json.Unmarshal(data, &raw); err != nil {
		return HiddenModel{}, fmt.Errorf("parsing config: %w", err)
	}

	models := ensureObject(raw, "models")
	hidden, _ := models["hiddenModels"].([]any)
	already := false
	for _, h := range hidden {
		if s, ok := h.(string); ok && strings.TrimSpace(s) == id {
			already = true
			break
		}
	}
	if !already {
		models["hiddenModels"] = append(hidden, id)
	}
	result.Hidden = true
	result.ClearedRoles = clearRolesReferencingModel(raw, id)

	if already && len(result.ClearedRoles) == 0 {
		return result, nil // already hidden, no role to clear — skip the write
	}

	ensureObject(raw, "meta")["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return HiddenModel{}, fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return HiddenModel{}, fmt.Errorf("writing config: %w", err)
	}
	if logger != nil {
		logger.Info("hid model", "model", id, "clearedRoles", result.ClearedRoles, "path", configPath)
	}
	return result, nil
}

// LoadHiddenModels reads models.hiddenModels into a lookup set of full model IDs
// the picker must omit. Missing/empty/unreadable → nil (callers treat nil as an
// empty set). Read per models.list call; the file is small.
func LoadHiddenModels(configPath string) map[string]bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	models, ok := raw["models"].(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := models["hiddenModels"].([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	set := make(map[string]bool, len(arr))
	for _, h := range arr {
		if s, ok := h.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				set[t] = true
			}
		}
	}
	return set
}
