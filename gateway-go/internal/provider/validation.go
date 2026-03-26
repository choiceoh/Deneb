// validation.go — Provider validation and normalization for the Go gateway.
// Mirrors src/plugins/provider-validation.ts (200 LOC).
//
// Normalizes and validates registered provider definitions:
// - Text normalization (trim, dedup)
// - Auth method validation (required ID, duplicate detection)
// - Wizard setup validation
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
	ID     string `json:"id"`
	Label  string `json:"label"`
	Hint   string `json:"hint,omitempty"`
	Kind   string `json:"kind"` // "oauth", "api_key", "token", "device_code", "custom"
	Wizard *WizardSetupDef `json:"wizard,omitempty"`
}

// WizardSetupDef describes wizard setup configuration.
type WizardSetupDef struct {
	ChoiceID       string            `json:"choiceId,omitempty"`
	ChoiceLabel    string            `json:"choiceLabel,omitempty"`
	ChoiceHint     string            `json:"choiceHint,omitempty"`
	GroupID        string            `json:"groupId,omitempty"`
	GroupLabel     string            `json:"groupLabel,omitempty"`
	GroupHint      string            `json:"groupHint,omitempty"`
	MethodID       string            `json:"methodId,omitempty"`
	ModelAllowlist *ModelAllowlistDef `json:"modelAllowlist,omitempty"`
}

// ModelAllowlistDef describes model filtering for wizard flows.
type ModelAllowlistDef struct {
	AllowedKeys       []string `json:"allowedKeys,omitempty"`
	InitialSelections []string `json:"initialSelections,omitempty"`
	Message           string   `json:"message,omitempty"`
}

// WizardModelPickerDef describes a model picker in wizards.
type WizardModelPickerDef struct {
	Label    string `json:"label,omitempty"`
	Hint     string `json:"hint,omitempty"`
	MethodID string `json:"methodId,omitempty"`
}

// WizardDef holds wizard configuration for a provider.
type WizardDef struct {
	Setup       *WizardSetupDef       `json:"setup,omitempty"`
	ModelPicker *WizardModelPickerDef `json:"modelPicker,omitempty"`
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
	Wizard               *WizardDef              `json:"wizard,omitempty"`
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

		// Normalize method-level wizard.
		if method.Wizard != nil {
			wizardSetup, wizDiags := normalizeWizardSetup(normalizeWizardSetupParams{
				providerID: params.ProviderID,
				pluginID:   params.PluginID,
				source:     params.Source,
				auth:       []ProviderAuthMethodDef{{ID: methodID}},
				setup:      method.Wizard,
			})
			diagnostics = append(diagnostics, wizDiags...)
			if wizardSetup != nil {
				entry.Wizard = wizardSetup
			}
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

// --- Wizard validation ---

type normalizeWizardSetupParams struct {
	providerID string
	pluginID   string
	source     string
	auth       []ProviderAuthMethodDef
	setup      *WizardSetupDef
}

func normalizeWizardSetup(params normalizeWizardSetupParams) (*WizardSetupDef, []ProviderDiagnostic) {
	var diagnostics []ProviderDiagnostic
	if params.setup == nil {
		return nil, nil
	}
	if len(params.auth) == 0 {
		diagnostics = append(diagnostics, ProviderDiagnostic{
			Level:    "warn",
			PluginID: params.pluginID,
			Source:   params.source,
			Message:  fmt.Sprintf("provider %q setup metadata ignored because it has no auth methods", params.providerID),
		})
		return nil, diagnostics
	}

	methodID := normalizeText(params.setup.MethodID)
	if methodID != "" && !hasAuthMethod(params.auth, methodID) {
		diagnostics = append(diagnostics, ProviderDiagnostic{
			Level:    "warn",
			PluginID: params.pluginID,
			Source:   params.source,
			Message:  fmt.Sprintf("provider %q setup method %q not found; falling back to available methods", params.providerID, methodID),
		})
		methodID = ""
	}

	result := &WizardSetupDef{}
	if v := normalizeText(params.setup.ChoiceID); v != "" {
		result.ChoiceID = v
	}
	if v := normalizeText(params.setup.ChoiceLabel); v != "" {
		result.ChoiceLabel = v
	}
	if v := normalizeText(params.setup.ChoiceHint); v != "" {
		result.ChoiceHint = v
	}
	if v := normalizeText(params.setup.GroupID); v != "" {
		result.GroupID = v
	}
	if v := normalizeText(params.setup.GroupLabel); v != "" {
		result.GroupLabel = v
	}
	if v := normalizeText(params.setup.GroupHint); v != "" {
		result.GroupHint = v
	}
	if methodID != "" {
		result.MethodID = methodID
	}

	if params.setup.ModelAllowlist != nil {
		al := &ModelAllowlistDef{}
		if keys := normalizeTextList(params.setup.ModelAllowlist.AllowedKeys); keys != nil {
			al.AllowedKeys = keys
		}
		if sels := normalizeTextList(params.setup.ModelAllowlist.InitialSelections); sels != nil {
			al.InitialSelections = sels
		}
		if msg := normalizeText(params.setup.ModelAllowlist.Message); msg != "" {
			al.Message = msg
		}
		result.ModelAllowlist = al
	}

	return result, diagnostics
}

// NormalizeProviderWizard validates and normalizes a provider wizard definition.
func NormalizeProviderWizard(params NormalizeWizardParams) (*WizardDef, []ProviderDiagnostic) {
	var diagnostics []ProviderDiagnostic
	if params.Wizard == nil {
		return nil, nil
	}

	hasAuth := len(params.Auth) > 0

	// Normalize setup.
	var setup *WizardSetupDef
	if params.Wizard.Setup != nil {
		s, diags := normalizeWizardSetup(normalizeWizardSetupParams{
			providerID: params.ProviderID,
			pluginID:   params.PluginID,
			source:     params.Source,
			auth:       params.Auth,
			setup:      params.Wizard.Setup,
		})
		diagnostics = append(diagnostics, diags...)
		setup = s
	}

	// Normalize model picker.
	var modelPicker *WizardModelPickerDef
	if params.Wizard.ModelPicker != nil {
		if !hasAuth {
			diagnostics = append(diagnostics, ProviderDiagnostic{
				Level:    "warn",
				PluginID: params.PluginID,
				Source:   params.Source,
				Message:  fmt.Sprintf("provider %q model-picker metadata ignored because it has no auth methods", params.ProviderID),
			})
		} else {
			mp := &WizardModelPickerDef{}
			if v := normalizeText(params.Wizard.ModelPicker.Label); v != "" {
				mp.Label = v
			}
			if v := normalizeText(params.Wizard.ModelPicker.Hint); v != "" {
				mp.Hint = v
			}
			methodID := normalizeText(params.Wizard.ModelPicker.MethodID)
			if methodID != "" {
				if hasAuthMethod(params.Auth, methodID) {
					mp.MethodID = methodID
				} else {
					diagnostics = append(diagnostics, ProviderDiagnostic{
						Level:    "warn",
						PluginID: params.PluginID,
						Source:   params.Source,
						Message:  fmt.Sprintf("provider %q model-picker method %q not found; falling back to available methods", params.ProviderID, methodID),
					})
				}
			}
			modelPicker = mp
		}
	}

	if setup == nil && modelPicker == nil {
		return nil, diagnostics
	}
	return &WizardDef{Setup: setup, ModelPicker: modelPicker}, diagnostics
}

// NormalizeWizardParams holds parameters for NormalizeProviderWizard.
type NormalizeWizardParams struct {
	ProviderID string
	PluginID   string
	Source     string
	Auth       []ProviderAuthMethodDef
	Wizard     *WizardDef
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

	// Normalize wizard.
	wizard, wizDiags := NormalizeProviderWizard(NormalizeWizardParams{
		ProviderID: id,
		PluginID:   params.PluginID,
		Source:     params.Source,
		Auth:       auth,
		Wizard:     params.Provider.Wizard,
	})
	diagnostics = append(diagnostics, wizDiags...)

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
	if wizard != nil {
		result.Wizard = wizard
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
