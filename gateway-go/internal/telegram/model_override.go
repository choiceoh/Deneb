// Channel model override — resolves per-channel, per-account, and per-group
// model selection from the channels.modelByChannel configuration.
//
// Mirrors src/channels/model-overrides.ts.
package telegram

import (
	"encoding/json"
	"regexp"
	"strings"
)

var threadSuffixRegex = regexp.MustCompile(`(?i):(?:thread|topic):[^:]+$`)

// ChannelModelOverride is the result of resolving a channel-specific model.
type ChannelModelOverride struct {
	Channel  string // normalized channel name
	Model    string // resolved model ID
	MatchKey string // the config key that matched
}

// ChannelModelOverrideParams holds the inputs for model override resolution.
type ChannelModelOverrideParams struct {
	// RawChannelsConfig is the raw JSON of the "channels" config section.
	RawChannelsConfig json.RawMessage
	Channel           string
	GroupID           string
	GroupChannel      string
	GroupSubject      string
	ParentSessionKey  string
}

// ResolveChannelModelOverride resolves a channel-specific model override.
// Returns nil if no override is configured for this channel/group combination.
func ResolveChannelModelOverride(p ChannelModelOverrideParams) *ChannelModelOverride {
	channel := strings.TrimSpace(p.Channel)
	if channel == "" {
		return nil
	}

	modelByChannel := extractModelByChannel(p.RawChannelsConfig)
	if modelByChannel == nil {
		return nil
	}

	providerEntries := resolveProviderEntry(modelByChannel, channel)
	if providerEntries == nil {
		return nil
	}

	candidates := buildChannelKeyCandidates(p.GroupID, p.GroupChannel, p.GroupSubject, p.ParentSessionKey)
	if len(candidates) == 0 {
		return nil
	}

	// Try each candidate key, then fall back to wildcard.
	matchKey, model := matchEntry(providerEntries, candidates)
	if model == "" {
		// Try wildcard.
		if v, ok := providerEntries["*"]; ok {
			model = strings.TrimSpace(v)
			matchKey = "*"
		}
	}
	if model == "" {
		return nil
	}

	return &ChannelModelOverride{
		Channel:  normalizeMessageChannel(channel),
		Model:    model,
		MatchKey: matchKey,
	}
}

// --- internal helpers ---

func extractModelByChannel(rawChannels json.RawMessage) map[string]map[string]string {
	if len(rawChannels) == 0 {
		return nil
	}
	var channels struct {
		ModelByChannel map[string]map[string]string `json:"modelByChannel"`
	}
	if json.Unmarshal(rawChannels, &channels) != nil {
		return nil
	}
	return channels.ModelByChannel
}

func resolveProviderEntry(modelByChannel map[string]map[string]string, channel string) map[string]string {
	normalized := normalizeMessageChannel(channel)
	if entries, ok := modelByChannel[normalized]; ok {
		return entries
	}
	// Case-insensitive fallback.
	for key, entries := range modelByChannel {
		if normalizeMessageChannel(key) == normalized {
			return entries
		}
	}
	return nil
}

func normalizeMessageChannel(ch string) string {
	return strings.TrimSpace(strings.ToLower(ch))
}

func buildChannelKeyCandidates(groupID, groupChannel, groupSubject, parentSessionKey string) []string {
	groupID = strings.TrimSpace(groupID)
	groupChannel = strings.TrimSpace(groupChannel)
	groupSubject = strings.TrimSpace(groupSubject)

	parentGroupID := resolveParentGroupID(groupID)
	parentGroupIDFromSession := resolveGroupIDFromSessionKey(parentSessionKey)
	if parentGroupIDFromSession != "" && parentGroupID == "" {
		parentGroupID = resolveParentGroupID(parentGroupIDFromSession)
		if parentGroupID == "" {
			parentGroupID = parentGroupIDFromSession
		}
	}

	channelBare := strings.TrimPrefix(groupChannel, "#")
	subjectBare := strings.TrimPrefix(groupSubject, "#")
	channelSlug := normalizeSlug(channelBare)
	subjectSlug := normalizeSlug(subjectBare)

	var keys []string
	addUnique := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, existing := range keys {
			if strings.EqualFold(existing, v) {
				return
			}
		}
		keys = append(keys, v)
	}

	addUnique(groupID)
	addUnique(parentGroupID)
	addUnique(groupChannel)
	addUnique(channelBare)
	addUnique(channelSlug)
	addUnique(groupSubject)
	addUnique(subjectBare)
	addUnique(subjectSlug)

	return keys
}

func resolveParentGroupID(groupID string) string {
	raw := strings.TrimSpace(groupID)
	if raw == "" || !threadSuffixRegex.MatchString(raw) {
		return ""
	}
	parent := strings.TrimSpace(threadSuffixRegex.ReplaceAllString(raw, ""))
	if parent == "" || parent == raw {
		return ""
	}
	return parent
}

func resolveGroupIDFromSessionKey(sessionKey string) string {
	raw := strings.TrimSpace(sessionKey)
	if raw == "" {
		return ""
	}
	re := regexp.MustCompile(`(?i)(?:^|:)(?:group|channel):([^:]+)(?::|$)`)
	match := re.FindStringSubmatch(raw)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func normalizeSlug(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric except hyphens.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func matchEntry(entries map[string]string, candidates []string) (string, string) {
	for _, key := range candidates {
		lowered := strings.TrimSpace(strings.ToLower(key))
		for entryKey, model := range entries {
			if strings.TrimSpace(strings.ToLower(entryKey)) == lowered {
				return entryKey, strings.TrimSpace(model)
			}
		}
	}
	return "", ""
}
