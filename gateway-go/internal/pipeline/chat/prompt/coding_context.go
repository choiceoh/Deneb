// coding_context.go — the 코드모드 prompt profile: the implementer persona that
// replaces the Nev chief-of-staff identity for code: sessions, and the
// session-frozen loader for the target repo's root rule docs (CLAUDE.md /
// AGENTS.md inside the worktree).
//
// Why a full swap (not an add-on block): a coding turn inherits nothing useful
// from the 업무 secretary framing — the proactive mail/calendar work loop, the
// wiki write-back doctrine, and the personal work context are all noise that
// pushed code: sessions to behave like "a chatbot with a different directory".
// The coding profile mirrors the 챗봇 profile mechanically (SystemPromptParams
// flag + its own Static cache-key suffix → its own vLLM APC prefix family) but
// swaps in an implementer contract instead of a neutral assistant.
package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// CodingPersona is the identity + working contract rendered at the top of the
// Static block for 코드모드 (code: sessions), replacing DefaultPersona wholesale.
// It encodes the three interview-settled contracts: adaptive autonomy keyed on
// instruction AMBIGUITY (clear → finish the turn regardless of scale; ambiguous
// → short Korean plan first), the fixed per-turn report structure (the vibe
// coder cannot read diffs — the report IS the screen), and the worktree
// lifecycle rules absorbed from the old implementer-preset dynamic section.
const CodingPersona = "You are the Deneb coding agent (코드모드 구현자) — you read, change, and verify code inside an isolated git worktree. 사용자는 diff를 읽지 않는 바이브코더다: 너의 한국어 보고가 곧 화면이다.\n\n" +
	"## 작업 방식\n" +
	"- 지시가 명확하면 규모와 무관하게 **이 턴 안에서 구현 → 검증까지 완주**하라. 계획만 말하고 끝내는 턴은 미완성이다.\n" +
	"- 지시가 모호할 때만 — 구현 방향이 둘 이상이고 방향에 따라 결과물이 크게 달라질 때 — 한국어 3~5불릿 계획을 제시하고 \"진행할까요?\"로 승인을 받아라. 그 외의 작은 불확실성은 합리적으로 가정하고 진행하되, 그 가정을 보고에 명시하라.\n" +
	"- 수정 전에 관련 코드를 먼저 읽고 기존 관례(네이밍·구조·스타일·주석 밀도)를 따르라.\n" +
	"- 파일을 수정했으면 끝내기 전에 프로젝트의 빌드/테스트를 exec로 직접 돌려 통과를 확인하라. 실패하면 고치고 재검증하라 — 실패한 채 완료라고 보고하지 마라.\n\n" +
	"## 보고 형식 (매 턴)\n" +
	"1. **한 줄 결론** — 무엇이 됐는지 / 어디서 막혔는지.\n" +
	"2. **바꾼 것** — 파일·요지 2~5불릿 (경로 포함).\n" +
	"3. **검증** — 실행한 빌드/테스트 명령과 통과 여부.\n" +
	"4. **가정·다음 제안** — 있을 때만.\n" +
	"코드 diff나 긴 코드 블록을 통째로 붙이지 마라. 무엇을 왜 바꿨는지 한국어로 설명하는 것이 답이다. 응답은 항상 한국어, 코드·식별자·커밋 메시지는 영어.\n\n" +
	"## 워크트리 라이프사이클\n" +
	"- read/write/edit/exec가 동작하는 디렉토리가 지금 작업 중인 격리된 git worktree다. 사용자의 main 체크아웃에는 영향이 없다.\n" +
	"- 매 턴의 체크포인트 커밋과 빌드/테스트 검증은 시스템이 자동으로 남긴다 — 평소엔 직접 커밋하지 마라.\n" +
	"- **완료 시 PR**: 요청이 다 끝났다고 판단되면 빌드/테스트 통과를 확인한 뒤 변경을 커밋하고 `git push -u origin HEAD` → `gh pr create --fill` 로 PR을 열어 링크를 알려라 (PR이 이미 있으면 push만으로 갱신된다).\n" +
	"- **main에 직접 머지하지 마라** — PR 브랜치까지만. 머지는 사용자가 검토 후 결정한다.\n" +
	"- 잘못된 변경은 `git reset` / `git checkout -- <file>` 등으로 직접 되돌려라.\n" +
	"- 미완성이거나 빌드가 깨진 상태로 push하지 마라.\n\n"

// Coding repo-context budgets. Root rule docs are contracts (commit format,
// build gates, prohibitions) — worth always-in-context injection — but repos
// like Deneb split most rules into subtree files, which the persona tells the
// agent to read on demand instead of inflating the prompt.
const (
	// maxCodingRepoFileChars caps one root doc (mirrors context_files.go's 8K).
	maxCodingRepoFileChars = 8_000
	// maxCodingRepoTotalChars caps the combined injection (CLAUDE.md + AGENTS.md).
	maxCodingRepoTotalChars = 16_000
)

// codingRepoContextFiles are the worktree-root rule docs injected into the
// coding Static block, in load order.
var codingRepoContextFiles = []string{"CLAUDE.md", "AGENTS.md"}

// CodingRepoContext holds the rendered repo rule docs for a coding session.
// Both fields are empty when the worktree has no (non-empty) root docs.
type CodingRepoContext struct {
	Content string // rendered "### <file>" sections, truncated to budget
	Hash    string // sha256 hex (12 chars) of Content; folds into the Static cache key
}

// LoadCodingRepoContext reads the worktree root's CLAUDE.md/AGENTS.md and
// freezes the result per session, mirroring LoadTopicKnowledge's semantics:
// the first call for a sessionKey caches the value (including empty results,
// so a doc-less repo is not re-stat'd every turn) and every later call returns
// it unchanged — keeping the coding Static block byte-stable for the session
// even when the agent itself edits CLAUDE.md mid-session (the edit takes
// effect from the next session). Never errors: a missing/unreadable doc
// degrades to "no injection".
//
// Not persisted across gateway restarts (unlike prompt_snapshot_persist.go's
// client:main set): code sessions are not in the restart-wake set, and root
// rule docs are stable enough that a post-restart re-freeze is byte-identical
// in practice.
func LoadCodingRepoContext(worktreeDir, sessionKey string) CodingRepoContext {
	if worktreeDir == "" {
		return CodingRepoContext{}
	}
	if sessionKey != "" {
		if frozen, ok := Cache.CodingRepoSnapshot(sessionKey); ok {
			return frozen
		}
	}
	rc := loadCodingRepoContextFromDisk(worktreeDir)
	if sessionKey != "" {
		Cache.SetCodingRepoSnapshot(sessionKey, rc)
	}
	return rc
}

// loadCodingRepoContextFromDisk performs the actual read + truncation + hash.
func loadCodingRepoContextFromDisk(worktreeDir string) CodingRepoContext {
	var b strings.Builder
	total := 0
	for _, name := range codingRepoContextFiles {
		data, err := os.ReadFile(filepath.Join(worktreeDir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		budget := maxCodingRepoFileChars
		if remaining := maxCodingRepoTotalChars - total; remaining < budget {
			if remaining <= 0 {
				break
			}
			budget = remaining
		}
		if len(content) > budget {
			// Same head+tail strategy as context files: rule docs put hard
			// gates at both ends, so dropping the middle beats a hard cut.
			slog.Warn("coding repo context file exceeds budget; head/tail truncated",
				"file", name, "sizeBytes", len(content), "budgetBytes", budget)
			content = strings.TrimSpace(truncateContent(content, budget))
		}
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", name, content)
		total += len(content)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return CodingRepoContext{}
	}
	sum := sha256.Sum256([]byte(out))
	return CodingRepoContext{
		Content: out,
		Hash:    hex.EncodeToString(sum[:])[:12],
	}
}
