package chat

import (
	"fmt"
	"strings"
	"sync"
)

// pinned_facts.go — per-session "pinned facts" the user marks as always-remember.
//
// Research basis: docs/research/claw-anything-always-on-assistant.md, finding C
// (long-horizon degradation — even with the history available, models fail to keep
// using it; user-stated anchors get out-competed and lost). Pinned facts are
// injected into the system prompt's *Dynamic* block (no cache_control marker, per
// .claude/rules/prompt-cache.md), so they are inevictable by compaction and
// re-asserted every turn — the user's explicit "never forget this" channel.
//
// State is a small in-memory per-session store (mirrors recall_cache.go), cleared
// on /reset. Single-user deployment: no expiry needed. The store + formatting are
// pure and unit-tested; wiring is /pin /unpin /pins (slash_commands.go +
// slash_dispatch.go) and a PinnedFacts field threaded into SystemPromptParams.

const (
	maxPinnedFacts     = 7   // keep the dynamic block small; over-pinning dilutes signal
	maxPinnedFactRunes = 240 // a fact is a sentence, not a document (CJK rune count)
)

var pinnedFactsStore = struct {
	mu    sync.Mutex
	facts map[string][]string
}{facts: make(map[string][]string)}

// pinFact appends a fact to the session's list. Returns ok=false with a Korean
// reason when it can't be added (empty, too long, duplicate, or at capacity).
func pinFact(sessionKey, fact string) (ok bool, reason string) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return false, "고정할 내용을 입력하세요. 예: /pin 거래처 X 담당자는 김부장"
	}
	if n := len([]rune(fact)); n > maxPinnedFactRunes {
		return false, fmt.Sprintf("너무 깁니다 (%d자). %d자 이내로 줄여주세요.", n, maxPinnedFactRunes)
	}
	if sessionKey == "" {
		return false, "세션이 없어 고정할 수 없습니다."
	}
	pinnedFactsStore.mu.Lock()
	defer pinnedFactsStore.mu.Unlock()
	cur := pinnedFactsStore.facts[sessionKey]
	for _, f := range cur {
		if f == fact {
			return false, "이미 고정된 사실입니다."
		}
	}
	if len(cur) >= maxPinnedFacts {
		return false, fmt.Sprintf("고정 사실은 최대 %d개입니다. /unpin <번호>로 하나 제거한 뒤 추가하세요.", maxPinnedFacts)
	}
	pinnedFactsStore.facts[sessionKey] = append(cur, fact)
	return true, ""
}

// unpinFact removes the 1-based index from the session's list.
func unpinFact(sessionKey string, index int) (removed string, ok bool) {
	pinnedFactsStore.mu.Lock()
	defer pinnedFactsStore.mu.Unlock()
	cur := pinnedFactsStore.facts[sessionKey]
	if index < 1 || index > len(cur) {
		return "", false
	}
	removed = cur[index-1]
	next := make([]string, 0, len(cur)-1)
	next = append(next, cur[:index-1]...)
	next = append(next, cur[index:]...)
	if len(next) == 0 {
		delete(pinnedFactsStore.facts, sessionKey)
	} else {
		pinnedFactsStore.facts[sessionKey] = next
	}
	return removed, true
}

// listPinnedFacts returns a copy of the session's pinned facts (nil when none).
func listPinnedFacts(sessionKey string) []string {
	pinnedFactsStore.mu.Lock()
	defer pinnedFactsStore.mu.Unlock()
	cur := pinnedFactsStore.facts[sessionKey]
	if len(cur) == 0 {
		return nil
	}
	out := make([]string, len(cur))
	copy(out, cur)
	return out
}

// clearPinnedFacts drops all pinned facts for the session (called on /reset).
func clearPinnedFacts(sessionKey string) {
	if sessionKey == "" {
		return
	}
	pinnedFactsStore.mu.Lock()
	delete(pinnedFactsStore.facts, sessionKey)
	pinnedFactsStore.mu.Unlock()
}

// formatPinnedFactsBlock renders the facts as a numbered list for the dynamic
// system-prompt block. Returns "" when there are none (caller omits the section).
func formatPinnedFactsBlock(facts []string) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range facts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderPinnedFactsReply renders the user-facing list for /pins and confirmations.
func renderPinnedFactsReply(facts []string) string {
	if len(facts) == 0 {
		return "고정된 사실이 없습니다. /pin <내용> 으로 추가하세요."
	}
	var b strings.Builder
	b.WriteString("📌 고정된 사실:\n")
	for i, f := range facts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	return strings.TrimRight(b.String(), "\n")
}

// parsePinIndex parses the 1-based index argument for /unpin.
func parsePinIndex(s string) (int, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "번"))
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("index must be positive")
	}
	return n, nil
}
