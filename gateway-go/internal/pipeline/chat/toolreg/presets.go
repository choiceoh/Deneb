package toolreg

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"

// Preset constants re-exported so chat parent can avoid importing toolpreset
// directly (fanout reduction). Values match toolpreset.Preset*.
const (
	PresetNone         = string(toolpreset.PresetNone)
	PresetConversation = string(toolpreset.PresetConversation)
	PresetBoot         = string(toolpreset.PresetBoot)
	PresetSelfReview   = string(toolpreset.PresetSelfReview)
	PresetResearcher   = string(toolpreset.PresetResearcher)
	PresetImplementer  = string(toolpreset.PresetImplementer)
	PresetVerifier     = string(toolpreset.PresetVerifier)
	PresetWikiResearch = string(toolpreset.PresetWikiResearch)
	PresetWikiScout    = string(toolpreset.PresetWikiScout)
	PresetNotiDigest   = string(toolpreset.PresetNotiDigest)
	PresetCoding       = string(toolpreset.PresetCoding)
	PresetBriefcase    = string(toolpreset.PresetBriefcase)
)

// AllowedTools returns the allow-list for a named session tool preset.
// nil means unrestricted.
func AllowedTools(presetName string) map[string]struct{} {
	return toolpreset.AllowedTools(toolpreset.Preset(presetName))
}

// PreloadedDeferredTools returns deferred tools to activate from turn 1 for a preset.
func PreloadedDeferredTools(presetName string) []string {
	return toolpreset.PreloadedDeferredTools(toolpreset.Preset(presetName))
}
