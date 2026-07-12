// brief.go — operator wiki brief (WIKI.md): externalized steering for the
// autonomous wiki-maintenance prompts (OpenWiki's "wiki brief" pattern,
// github.com/langchain-ai/openwiki).
//
// The dreamer's synthesis rules and the research task's refresh rules are
// code-embedded, so "focus more on X, stop recording Y" used to require a
// code change and a deploy. WIKI.md in the agent workspace (next to USER.md
// and MEMORY.md) is the operator-owned steering file: both background
// prompts inject it verbatim each cycle, so editing the file redirects the
// next cycle immediately — a concrete RSI-P1 (procedure externalization)
// step for the wiki maintenance loop.
//
// Precedence: the brief steers *what to focus on / what to ignore*; it must
// not override layout invariants (slot paths, category set, no-guess rules).
// The rendered section states this explicitly so a conflicting brief loses.
//
// Cache safety: WIKI.md is NOT a chat context file (prompt/context_files.go
// contextFileNames does not include it), so it never enters the session
// prompt or its cache path — background maintenance prompts only.
package wiki

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// WikiBriefFileName is the operator-edited steering file in the agent
	// workspace. Missing file = no steering (all consumers fall back to
	// their built-in rules alone).
	WikiBriefFileName = "WIKI.md"
	// maxWikiBriefRunes bounds how much of the file is injected per prompt —
	// a brief is a paragraph of steering, not a rulebook. Oversize content is
	// truncated with a visible marker so the operator notices.
	maxWikiBriefRunes = 2000
)

// LoadWikiBrief reads the workspace WIKI.md and returns its trimmed content,
// truncated to the brief budget. Returns "" when the workspace is unset, the
// file is missing/unreadable, or the content is empty — callers treat "" as
// "no operator steering".
func LoadWikiBrief(workspaceDir string) string {
	if strings.TrimSpace(workspaceDir) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(workspaceDir, WikiBriefFileName))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	if runes := []rune(s); len(runes) > maxWikiBriefRunes {
		s = strings.TrimSpace(string(runes[:maxWikiBriefRunes])) +
			"\n(…이하 생략 — WIKI.md가 브리프 예산을 초과했습니다. 브리프는 짧게 유지하세요.)"
	}
	return s
}

// WikiBriefSection renders the operator brief as a self-contained prompt
// section, "" when the brief is empty. Both the dreamer synthesis prompt and
// the wiki-research prompt use this single renderer so the framing (and the
// invariant-precedence line) never drifts between consumers.
func WikiBriefSection(brief string) string {
	if strings.TrimSpace(brief) == "" {
		return ""
	}
	return "\n## 운영자 위키 지침 (워크스페이스 WIKI.md — 운영자가 직접 관리)\n" +
		brief +
		"\n(위 지침은 무엇에 집중하고 무엇을 무시할지의 조향입니다. 카테고리·경로·슬롯 등 레이아웃 불변식이나 추측 금지 규칙과 충돌하면 불변식이 우선합니다.)\n"
}
