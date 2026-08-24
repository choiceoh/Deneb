// recall_fact_lifecycle.go — the exposure boundary. Superseded and tombstoned
// values are stripped from federated evidence here, at the last point before
// evidence becomes prompt text.
package recall

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func filterRecallFactLifecycleEvidence(
	store *wiki.Store,
	query string,
	evidence []recallEvidence,
	snapshot wiki.FactRecallSnapshot,
) []recallEvidence {
	if store == nil || len(evidence) == 0 {
		return evidence
	}
	items := make([]wiki.FactLifecycleEvidence, len(evidence))
	for index, item := range evidence {
		items[index] = wiki.FactLifecycleEvidence{
			Query: query, Ref: item.Source, SubjectID: item.SubjectID, Text: item.Note,
		}
	}
	allowed := store.FactLifecycleEvidencesAllowed(items, snapshot)
	filtered := evidence[:0]
	for index, item := range evidence {
		if allowed[index] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterStaleFactEvidence(evidence []recallEvidence, staleValues []string) []recallEvidence {
	if len(evidence) == 0 || len(staleValues) == 0 {
		return evidence
	}
	stale := make([]string, 0, len(staleValues))
	for _, value := range staleValues {
		value = strings.ToLower(strings.Join(strings.Fields(value), " "))
		if value != "" && !structuralStaleValue(value) && wiki.IsFactLifecycleGlobalStalePhrase(value) {
			stale = append(stale, value)
		}
	}
	out := evidence[:0]
	for _, item := range evidence {
		blocked := false
		for _, value := range stale {
			if evidenceContainsNormalizedFactValue(item.Note, value) {
				blocked = true
				break
			}
		}
		if !blocked {
			out = append(out, item)
		}
	}
	return out
}

func stripSupersededPageMarkers(evidence []recallEvidence) ([]recallEvidence, []string) {
	if len(evidence) == 0 {
		return evidence, nil
	}
	out := evidence[:0]
	seen := make(map[string]struct{})
	var stale []string
	for _, item := range evidence {
		if item.Kind != "superseded" {
			out = append(out, item)
			continue
		}
		for _, line := range strings.Split(item.StaleValue, "\n") {
			line = strings.TrimSpace(line)
			// Markdown headings describe page structure/title, not retired claim
			// values. Treating "핵심 사실" or a shared page title as a deny phrase
			// can erase unrelated or replacement evidence.
			if strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimSpace(strings.TrimLeft(line, "-*`> "))
			if n := len([]rune(line)); n < 1 || n > 500 {
				continue
			}
			folded := strings.ToLower(strings.Join(strings.Fields(line), " "))
			if structuralStaleValue(folded) {
				continue
			}
			if _, ok := seen[folded]; ok {
				continue
			}
			seen[folded] = struct{}{}
			stale = append(stale, line)
		}
	}
	return out, stale
}

func structuralStaleValue(value string) bool {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	switch value {
	case "요약", "핵심 사실", "변경 이력", "summary", "key facts", "change history", "changelog":
		return true
	default:
		return false
	}
}
