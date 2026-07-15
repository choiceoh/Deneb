package toolwire

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"

// Preset re-exports used by the chat parent so it does not import toolpreset.
const (
	PresetSelfReview = string(toolpreset.PresetSelfReview)
	PresetBriefcase  = string(toolpreset.PresetBriefcase)
)

// AllowedTools returns the allow-list for a named session tool preset.
func AllowedTools(presetName string) map[string]struct{} {
	return toolpreset.AllowedTools(toolpreset.Preset(presetName))
}

// PreloadedDeferredTools returns deferred tools to activate from turn 1.
func PreloadedDeferredTools(presetName string) []string {
	return toolpreset.PreloadedDeferredTools(toolpreset.Preset(presetName))
}
