// activation_notice.go — the model-facing activation notices for deferred
// tools, and their parser.
//
// Activation evidence must round-trip through the transcript: a deferred tool
// activated this run should still be active on the NEXT run (and after a
// gateway restart), so the run pipeline re-derives activation state by
// scanning history for these exact notices (chat/deferred_replay.go) instead
// of keeping a separate store — the transcript is the single source of truth,
// the same discipline Pydantic AI uses for its load_capability replay. Writers
// (tools/fetch_tools.go, chat/tool_skill_required_tools.go) and the parser
// live in lockstep here; changing a format without updating the parser breaks
// replay for future turns (old transcripts keep working — parsers accept, they
// don't require).
package chatport

import (
	"strconv"
	"strings"
)

const (
	// fetchActivationPrefix/Suffix frame the fetch_tools activation sentence:
	// "Activated 2 tool(s): a, b. You can now call them directly."
	// The wording predates replay — existing transcripts already contain it,
	// so history from before this parser benefits immediately.
	fetchActivationPrefix = " tool(s): "
	fetchActivationSuffix = ". You can now call them directly."

	// skillActivationPrefix/Suffix frame the skill-consult activation notice:
	// "[스킬 필요 도구 활성화: a, b — 스키마가 로드되어 fetch_tools 없이 바로 호출할 수 있습니다.]"
	skillActivationPrefix = "[스킬 필요 도구 활성화: "
	skillActivationSuffix = " — 스키마가 로드되어 fetch_tools 없이 바로 호출할 수 있습니다.]"

	// alreadyActivePrefix/Suffix frame the fetch_tools short-circuit line for
	// tools that were active before the call. It counts as replay evidence
	// too: if the original activation was summarized away by the LLM
	// compaction tier and the model re-fetched, this line re-anchors the
	// evidence so the tool doesn't drop again a run later.
	alreadyActivePrefix = "Already active (schema loaded, no re-fetch needed): "
	alreadyActiveSuffix = ". Call them directly."
)

// FormatFetchActivationNotice renders the fetch_tools activation sentence.
func FormatFetchActivationNotice(names []string) string {
	return "Activated " + strconv.Itoa(len(names)) + fetchActivationPrefix +
		strings.Join(names, ", ") + fetchActivationSuffix
}

// FormatSkillActivationNotice renders the skill-consult activation notice
// appended to a SKILL.md read result.
func FormatSkillActivationNotice(names []string) string {
	return skillActivationPrefix + strings.Join(names, ", ") + skillActivationSuffix
}

// FormatAlreadyActiveNotice renders the fetch_tools short-circuit line for
// tools whose schemas are already loaded.
func FormatAlreadyActiveNotice(names []string) string {
	return alreadyActivePrefix + strings.Join(names, ", ") + alreadyActiveSuffix
}

// ParseActivationNotices extracts activated tool names from a tool result that
// may contain either notice form (a result carries at most one, but scanning
// both is harmless). Returns nil when no notice is present. Tolerant reader:
// it only trusts name-shaped tokens, so prose that happens to share a phrase
// can't inject arbitrary strings.
func ParseActivationNotices(content string) []string {
	var names []string
	for _, frame := range noticeFrames {
		if seg, ok := cutBetween(content, frame.pre, frame.post); ok {
			names = append(names, splitToolNames(seg)...)
		}
	}
	return names
}

// noticeFrames lists every recognized notice form, shared by the parser and
// the compaction extractor.
var noticeFrames = []struct {
	pre, post string
	format    func([]string) string
}{
	{fetchActivationPrefix, fetchActivationSuffix, FormatFetchActivationNotice},
	{skillActivationPrefix, skillActivationSuffix, FormatSkillActivationNotice},
	{alreadyActivePrefix, alreadyActiveSuffix, FormatAlreadyActiveNotice},
}

// ExtractActivationNotices returns the notices present in content re-rendered
// in canonical form (name-validated), at most one per format. Compaction stubs
// carry these forward when clearing old tool output, so replay evidence
// survives cheap pruning at one-line cost (same pattern as the read_spillover
// pointer).
func ExtractActivationNotices(content string) []string {
	var notices []string
	for _, frame := range noticeFrames {
		if seg, ok := cutBetween(content, frame.pre, frame.post); ok {
			if names := splitToolNames(seg); len(names) > 0 {
				notices = append(notices, frame.format(names))
			}
		}
	}
	return notices
}

// cutBetween returns the text between the first occurrence of pre and the
// next occurrence of post after it.
func cutBetween(s, pre, post string) (string, bool) {
	i := strings.Index(s, pre)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(pre):]
	j := strings.Index(rest, post)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// splitToolNames splits a ", "-joined name list, keeping only tool-name-shaped
// tokens (lowercase alphanumerics plus _ : -, the registry's naming universe).
func splitToolNames(seg string) []string {
	parts := strings.Split(seg, ", ")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && isToolName(p) {
			names = append(names, p)
		}
	}
	return names
}

func isToolName(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != ':' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}
