// validation.go — Provider validation and normalization for the Go gateway.
// Mirrors src/plugins/provider-validation.ts (200 LOC).
//
// Normalizes and validates registered provider definitions:
// - Text normalization (trim, dedup)
// - Auth method validation (required ID, duplicate detection)
// - Diagnostic collection for invalid configurations
package provider

import (
	"fmt"
	"strings"
)

// ProviderDiagnostic holds a diagnostic message from provider validation.
type ProviderDiagnostic struct {
	Level    string `json:"level"` // "error", "warn"
	PluginID string `json:"pluginId"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

// ProviderAuthMethodDef describes a provider auth method definition.
type ProviderAuthMethodDef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
	Kind  string `json:"kind"` // "oauth", "api_key", "token", "device_code", "custom"
}

// RegisteredProviderDef describes a provider registration to validate.
type RegisteredProviderDef struct {
	ID                   string                  `json:"id"`
	Label                string                  `json:"label"`
	DocsPath             string                  `json:"docsPath,omitempty"`
	Aliases              []string                `json:"aliases,omitempty"`
	EnvVars              []string                `json:"envVars,omitempty"`
	DeprecatedProfileIds []string                `json:"deprecatedProfileIds,omitempty"`
	Auth                 []ProviderAuthMethodDef `json:"auth"`
	HasCatalog           bool                    `json:"hasCatalog,omitempty"`
	HasDiscovery         bool                    `json:"hasDiscovery,omitempty"`
}

// --- Normalization helpers ---

// normalizeText trims a string. Returns empty string for whitespace-only input.
func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

// normalizeTextList deduplicates, trims, and filters empty strings.
// Returns nil if the result is empty.
func normalizeTextList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// --- Auth method validation ---

// NormalizeProviderAuthMethods validates and normalizes auth method definitions.
func NormalizeProviderAuthMethods(params NormalizeAuthParams) ([]ProviderAuthMethodDef, []ProviderDiagnostic) {
	var diagnostics []ProviderDiagnostic
	seenIDs := make(map[string]bool)
	var normalized []ProviderAuthMethodDef

	for _, method := range params.Auth {
		methodID := normalizeText(method.ID)
		if methodID == "" {
			diagnostics = append(diagnostics, ProviderDiagnostic{
				Level:    "error",
				PluginID: params.PluginID,
				Source:   params.Source,
				Message:  fmt.Sprintf("provider %q auth method missing id", params.ProviderID),
			})
			continue
		}
		if seenIDs[methodID] {
			diagnostics = append(diagnostics, ProviderDiagnostic{
				Level:    "error",
				PluginID: params.PluginID,
				Source:   params.Source,
				Message:  fmt.Sprintf("provider %q auth method duplicated id %q", params.ProviderID, methodID),
			})
			continue
		}
		seenIDs[methodID] = true

		label := normalizeText(method.Label)
		if label == "" {
			label = methodID
		}

		entry := ProviderAuthMethodDef{
			ID:    methodID,
			Label: label,
			Kind:  method.Kind,
		}
		if hint := normalizeText(method.Hint); hint != "" {
			entry.Hint = hint
		}

		normalized = append(normalized, entry)
	}

	return normalized, diagnostics
}

// NormalizeAuthParams holds parameters for NormalizeProviderAuthMethods.
type NormalizeAuthParams struct {
	ProviderID string
	PluginID   string
	Source     string
	Auth       []ProviderAuthMethodDef
}

// --- Top-level provider normalization ---

// NormalizeRegisteredProvider validates and normalizes a registered provider definition.
// Returns nil if the provider is invalid (missing ID).
func NormalizeRegisteredProvider(params NormalizeProviderParams) (*RegisteredProviderDef, []ProviderDiagnostic) {
	var diagnostics []ProviderDiagnostic

	id := normalizeText(params.Provider.ID)
	if id == "" {
		diagnostics = append(diagnostics, ProviderDiagnostic{
			Level:    "error",
			PluginID: params.PluginID,
			Source:   params.Source,
			Message:  "provider registration missing id",
		})
		return nil, diagnostics
	}

	// Normalize auth methods.
	auth, authDiags := NormalizeProviderAuthMethods(NormalizeAuthParams{
		ProviderID: id,
		PluginID:   params.PluginID,
		Source:     params.Source,
		Auth:       params.Provider.Auth,
	})
	diagnostics = append(diagnostics, authDiags...)

	// Warn if both catalog and discovery are present.
	if params.Provider.HasCatalog && params.Provider.HasDiscovery {
		diagnostics = append(diagnostics, ProviderDiagnostic{
			Level:    "warn",
			PluginID: params.PluginID,
			Source:   params.Source,
			Message:  fmt.Sprintf("provider %q registered both catalog and discovery; using catalog", id),
		})
	}

	label := normalizeText(params.Provider.Label)
	if label == "" {
		label = id
	}

	result := &RegisteredProviderDef{
		ID:    id,
		Label: label,
		Auth:  auth,
	}
	if v := normalizeText(params.Provider.DocsPath); v != "" {
		result.DocsPath = v
	}
	if v := normalizeTextList(params.Provider.Aliases); v != nil {
		result.Aliases = v
	}
	if v := normalizeTextList(params.Provider.DeprecatedProfileIds); v != nil {
		result.DeprecatedProfileIds = v
	}
	if v := normalizeTextList(params.Provider.EnvVars); v != nil {
		result.EnvVars = v
	}
	result.HasCatalog = params.Provider.HasCatalog
	result.HasDiscovery = params.Provider.HasDiscovery && !params.Provider.HasCatalog

	return result, diagnostics
}

// NormalizeProviderParams holds parameters for NormalizeRegisteredProvider.
type NormalizeProviderParams struct {
	PluginID string
	Source   string
	Provider RegisteredProviderDef
}

// --- Utility ---

func hasAuthMethod(auth []ProviderAuthMethodDef, methodID string) bool {
	for _, m := range auth {
		if m.ID == methodID {
			return true
		}
	}
	return false
}
