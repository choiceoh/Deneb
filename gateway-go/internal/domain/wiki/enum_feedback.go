// enum_feedback.go — write-time feedback for silently normalized enum fields.
//
// normalizeStage/normalizeKinds deliberately DROP out-of-vocabulary values at
// render (enum discipline keeps aggregation clean), which is right for the
// store but invisible to the agent: a chat write that sets stage "검토중"
// reports success while the value quietly disappears, so the model never
// learns to correct it. These helpers let tool surfaces detect the drop and
// return Korean guidance in the tool result, closing the feedback loop
// (adopted from OpenWiki's post-write frontmatter-warning pattern, 2026-07-20).
package wiki

import (
	"fmt"
	"sort"
	"strings"
)

// DroppedEnumNotes reports requested stage/kinds values that render-time
// normalization will silently drop, as Korean guidance lines for a tool
// result. Synonym folding (모듈→기자재/모듈), stage-word folding (개발→태양광),
// and parent-refinement drops are normalization, not errors — only values
// with no canonical mapping at all are reported.
func DroppedEnumNotes(stage string, kinds []string) []string {
	var notes []string
	if s := strings.TrimSpace(stage); s != "" && normalizeStage(s) == "" {
		notes = append(notes, fmt.Sprintf("stage %q는 유효 어휘가 아니어서 저장되지 않았습니다 — 유효값: %s",
			s, strings.Join(projectStages, "·")))
	}
	if dropped := droppedKinds(kinds); len(dropped) > 0 {
		notes = append(notes, fmt.Sprintf("kinds [%s]은(는) 유효 어휘가 아니어서 저장되지 않았습니다 — 유효값: %s",
			strings.Join(dropped, ", "), strings.Join(kindsVocabulary(), "·")))
	}
	return notes
}

// droppedKinds returns the requested kinds values that map to nothing in the
// canonical vocabulary (neither a projectKinds key nor a stage word).
func droppedKinds(kinds []string) []string {
	var out []string
	for _, k := range kinds {
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		if _, ok := projectKinds[key]; ok {
			continue
		}
		if kindStageWords[key] {
			continue
		}
		out = append(out, strings.TrimSpace(k))
	}
	return out
}

// kindsVocabulary returns the sorted canonical kinds values, derived from the
// synonym map so guidance stays current as the vocabulary evolves.
func kindsVocabulary() []string {
	seen := map[string]bool{}
	var out []string
	for _, canon := range projectKinds {
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	sort.Strings(out)
	return out
}
