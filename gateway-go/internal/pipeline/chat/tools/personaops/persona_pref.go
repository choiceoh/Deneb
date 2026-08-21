// persona_pref.go implements the `preference` agent tool: an append-only way
// for the agent to persist a durable standing preference / behavior rule about
// how it should act for this user, into the workspace SOUL.md persona file.
//
// Contract (modeled on waku-agent's update_soul, adapted to Deneb's SOUL.md):
//   - APPEND-ONLY. The tool can only ADD a rule; it exposes no delete or
//     rewrite path. The agent therefore cannot erase its own standing
//     constraints — only the human operator can, by editing SOUL.md directly
//     (or via the native UI). That asymmetry is the whole point: it stops the
//     agent from quietly deleting a preference the user set (and, in the RSI
//     loop, from eroding its own guardrails while self-improving).
//   - Rules land under a "## Learned rules" section appended to SOUL.md, kept
//     separate from the human-authored persona above it so a curator can see
//     (and rewrite) exactly what the agent added.
//   - Size-capped to the SOUL.md context budget so appended rules never blow
//     the prompt budget (past the cap the loader head/tail-truncates and would
//     silently drop rules).
//   - Deferred visibility: SOUL.md is a session-frozen context file, so a new
//     rule takes effect from the NEXT session (matching the prompt-cache
//     doctrine). The tool says so in its reply.
package personaops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// personaSoulFile is the workspace persona file the preference tool appends to.
	personaSoulFile = "SOUL.md"
	// personaLearnedRulesHeading segregates agent-appended rules from the
	// human-authored persona above it.
	personaLearnedRulesHeading = "## Learned rules"
	// personaSoulMaxBytes caps SOUL.md so appended rules never exceed the context
	// loader's per-file budget (prompt/context_files.go maxContextFileChars) — past
	// it the loader head/tail-truncates and silently drops rules.
	personaSoulMaxBytes = 8_000
)

// ToolPersonaPref returns the `preference` tool: append a durable standing
// behavior rule to the workspace SOUL.md. Append-only by contract.
func ToolPersonaPref(workspaceDir string) toolport.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Rule string `json:"rule"`
		}
		if err := jsonutil.UnmarshalInto("preference params", input, &p); err != nil {
			return "", err
		}
		rule := strings.TrimSpace(p.Rule)
		if rule == "" {
			return "rule는 필수입니다 — 저장할 서 있는 선호/행동 규칙을 한 줄로 적으세요.", nil
		}
		// Collapse to a single line so the bullet stays a clean, curator-readable
		// one-liner (multi-line persona prose belongs in SOUL.md, hand-authored).
		rule = strings.Join(strings.Fields(rule), " ")

		if workspaceDir == "" {
			return "선호 저장 실패: 워크스페이스가 설정되지 않았습니다.", nil
		}
		soulPath := resolvePersonaSoulPath(workspaceDir)

		existing, err := os.ReadFile(soulPath)
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("read SOUL.md: %w", err)
		}
		current := string(existing)

		// Idempotent: the rule is already present. SOUL.md is session-frozen, so
		// the agent can't "see" a rule it appended this session — dedup keeps it
		// from stacking the same rule every turn. Match the exact bullet LINE, not
		// a raw substring: a Contains check would false-positive when the rule text
		// is a substring of a longer existing rule (or of the persona prose).
		if hasBulletLine(current, "- "+rule) {
			return "이미 저장된 선호입니다 (중복 저장 안 함).", nil
		}

		chunk := buildPreferenceChunk(current, rule)
		if len(current)+len(chunk) > personaSoulMaxBytes {
			return fmt.Sprintf("선호 저장 실패: SOUL.md가 한도(%dB)에 도달해 더 추가할 수 없습니다. 사용자에게 SOUL.md의 오래된 규칙 정리를 요청하세요(축약은 사람만 가능).", personaSoulMaxBytes), nil
		}

		if err := os.MkdirAll(filepath.Dir(soulPath), 0o755); err != nil {
			return "", fmt.Errorf("create workspace dir: %w", err)
		}
		// True append: open O_APPEND and add only the new chunk. The tool never
		// rewrites or removes existing content — that asymmetry is the contract.
		f, err := os.OpenFile(soulPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("open SOUL.md: %w", err)
		}
		if _, werr := f.WriteString(chunk); werr != nil {
			f.Close()
			return "", fmt.Errorf("append SOUL.md: %w", werr)
		}
		if cerr := f.Close(); cerr != nil {
			return "", fmt.Errorf("close SOUL.md: %w", cerr)
		}

		return fmt.Sprintf("선호를 SOUL.md에 저장했습니다: %q. 이 규칙은 다음 세션부터 페르소나에 반영됩니다 (append-only — 삭제·수정은 사용자만 SOUL.md 편집으로 가능).", rule), nil
	}
}

// resolvePersonaSoulPath returns the SOUL.md the prompt would actually load: the
// closest existing SOUL.md walking from workspaceDir up its ancestors, so an
// append never shadows a human-authored ancestor persona with a new
// workspace-root file. Falls back to <workspaceDir>/SOUL.md when none exists.
//
// The ancestor walk mirrors prompt/context_files.go collectSearchDirs (workspace
// first, then up to 6 parents, stopping at $HOME/root). tools/ cannot import
// prompt/, so the small walk is duplicated here — keep the two in sync.
func resolvePersonaSoulPath(workspaceDir string) string {
	home, _ := os.UserHomeDir()
	dirs := []string{workspaceDir}
	current := workspaceDir
	for range 6 {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		dirs = append(dirs, parent)
		if home != "" && parent == home {
			break // include $HOME but never search above it
		}
		current = parent
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, personaSoulFile)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p // closest existing persona file wins (loader precedence)
		}
	}
	return filepath.Join(workspaceDir, personaSoulFile)
}

// hasBulletLine reports whether content already has bullet as a standalone line
// (trailing whitespace/CR ignored). Line-based so it matches regardless of the
// file's trailing newline and never false-positives on a substring.
func hasBulletLine(content, bullet string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimRight(line, " \t\r") == bullet {
			return true
		}
	}
	return false
}

// buildPreferenceChunk returns the text to append to SOUL.md for a new rule: a
// "## Learned rules" heading is added once (when absent), then the rule as a
// bullet. Leading newlines are chosen from the current content so the append
// never glues onto a previous line.
func buildPreferenceChunk(current, rule string) string {
	var b strings.Builder
	// Start on a fresh line relative to whatever is already there.
	if current != "" && !strings.HasSuffix(current, "\n") {
		b.WriteString("\n")
	}
	if !strings.Contains(current, personaLearnedRulesHeading) {
		if current != "" {
			b.WriteString("\n") // blank line before a new section
		}
		b.WriteString(personaLearnedRulesHeading)
		b.WriteString("\n\n")
	}
	b.WriteString("- ")
	b.WriteString(rule)
	b.WriteString("\n")
	return b.String()
}
