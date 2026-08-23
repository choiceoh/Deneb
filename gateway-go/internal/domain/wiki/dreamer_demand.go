// dreamer_demand.go — Go-side demand gate and project-signal cadence.
//
// The synthesis prompt already says "프로젝트 우선", but a one-shot LLM still
// emits 기타-only batches when the diary mixed a YouTube browse with live
// deal facts (2026-08-23: two cycles wrote only 이란-미국-갈등 while the
// morning letter had five projects and three dues). Prompts are advisory;
// this file is enforcement.
package wiki

import (
	"strings"
	"unicode"
)

// projectDemandTerms are facts that mean the diary batch has live deal
// content. Broader than the 30-minute signal: used to refuse 기타-only writes.
var projectDemandTerms = []string{
	"견적", "재견적", "단가", "공급가액", "원/w", "원/wp", "moq",
	"납기", "마감", "결재", "기한", "카톡 보고", "카톡보고",
	"백만", "억원",
}

// projectSignalTerms are the high-precision cues that accelerate dreaming
// the way 신호:선호 already does. Asking "프로젝트 근황" must not fire this.
var projectSignalTerms = []string{
	"견적", "재견적", "단가", "moq", "공급가액", "원/w", "원/wp",
	"카톡 보고", "카톡보고",
}

// diaryHasProjectDemand reports whether the cycle input carries deal facts
// that must land on a 프로젝트 page, not only 기타.
func diaryHasProjectDemand(input string) bool {
	return containsFoldedTerms(input, projectDemandTerms) || containsProjectCode(input)
}

// diaryHasProjectSignal reports whether a turn voiced a deal-number fact
// that should consolidate within wikiDreamPrefMinInterval.
func diaryHasProjectSignal(input string) bool {
	return containsFoldedTerms(input, projectSignalTerms)
}

func containsFoldedTerms(input string, terms []string) bool {
	lower := strings.ToLower(input)
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// containsProjectCode reports a frozen project identity in the text
// (pl2-kia-epc-001, nde-sun-cbl-001, …).
func containsProjectCode(input string) bool {
	lower := strings.ToLower(input)
	for _, prefix := range []string{"pl0-", "pl1-", "pl2-", "pl3-", "nde-", "com-", "etc-"} {
		if !strings.Contains(lower, prefix) {
			continue
		}
		for i := 0; i <= len(lower)-len(prefix)-7; i++ {
			if !strings.HasPrefix(lower[i:], prefix) {
				continue
			}
			if projectCodeTail(lower[i+len(prefix):]) {
				return true
			}
		}
	}
	return false
}

func projectCodeTail(rest string) bool {
	// client(3) - dtype(3) - seq(3)
	if len(rest) < 11 {
		return false
	}
	if !alnumRun(rest[:3]) || rest[3] != '-' || !alnumRun(rest[4:7]) || rest[7] != '-' {
		return false
	}
	return digitRun(rest[8:11])
}

func alnumRun(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func digitRun(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func updateCategory(u wikiUpdate) string {
	if cat := categoryFromPath(u.Path); cat != "" {
		return cat
	}
	return strings.TrimSpace(u.Category)
}

// critiqueNeeded reports whether the offline critic should run. Batches of
// 3+ always run. Tiny batches skip UNLESS they create pages or write 기타 —
// those are the cycles that used to skip the filter and land junk
// (critiqueMinUpdates=3, observed 2026-08-23).
func critiqueNeeded(updates []wikiUpdate) bool {
	if len(updates) >= critiqueMinUpdates {
		return true
	}
	for _, u := range updates {
		if u.Action == "create" || updateCategory(u) == "기타" {
			return true
		}
	}
	return false
}

// applyDemandGate drops 기타 proposals when the diary had project demand but
// synthesis wrote no 프로젝트 page. 업무/시스템/사용자/인물 writes stay —
// those can be legitimate alongside a missed project (the miss is the 기타
// distraction, not every non-project page). Returns the kept updates and
// how many 기타 items were dropped.
func applyDemandGate(input string, updates []wikiUpdate) ([]wikiUpdate, int) {
	if len(updates) == 0 || !diaryHasProjectDemand(input) {
		return updates, 0
	}
	for _, u := range updates {
		if updateCategory(u) == "프로젝트" {
			return updates, 0
		}
	}
	kept := make([]wikiUpdate, 0, len(updates))
	dropped := 0
	for _, u := range updates {
		if updateCategory(u) == "기타" {
			dropped++
			continue
		}
		kept = append(kept, u)
	}
	return kept, dropped
}
