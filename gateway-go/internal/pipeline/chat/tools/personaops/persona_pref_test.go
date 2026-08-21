package personaops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runPreference(t *testing.T, workspaceDir, rule string) string {
	t.Helper()
	tool := ToolPersonaPref(workspaceDir)
	in, _ := json.Marshal(map[string]string{"rule": rule})
	out, err := tool(context.Background(), in)
	if err != nil {
		t.Fatalf("preference(%q): %v", rule, err)
	}
	return out
}

func TestPreferenceCreatesLearnedRulesSectionAndAppends(t *testing.T) {
	dir := t.TempDir()
	soul := filepath.Join(dir, "SOUL.md")

	// First rule: creates SOUL.md with the "## Learned rules" section.
	out := runPreference(t, dir, "보고는 결론부터 3줄로")
	if !strings.Contains(out, "저장했습니다") {
		t.Fatalf("unexpected reply: %s", out)
	}
	body, err := os.ReadFile(soul)
	if err != nil {
		t.Fatalf("read SOUL.md: %v", err)
	}
	if !strings.Contains(string(body), personaLearnedRulesHeading) {
		t.Fatalf("heading missing:\n%s", body)
	}
	if !strings.Contains(string(body), "- 보고는 결론부터 3줄로") {
		t.Fatalf("rule bullet missing:\n%s", body)
	}

	// Second rule: appended under the SAME heading (only one heading total).
	runPreference(t, dir, "주말엔 알림 최소화")
	body2, _ := os.ReadFile(soul)
	if got := strings.Count(string(body2), personaLearnedRulesHeading); got != 1 {
		t.Fatalf("heading count = %d, want 1:\n%s", got, body2)
	}
	if !strings.Contains(string(body2), "- 주말엔 알림 최소화") {
		t.Fatalf("second rule missing:\n%s", body2)
	}
}

func TestPreferenceIsAppendOnlyPreservingExistingPersona(t *testing.T) {
	dir := t.TempDir()
	soul := filepath.Join(dir, "SOUL.md")
	// Hand-authored persona already present (no trailing newline, no heading).
	const persona = "# 페르소나\n\n담백하고 정확하게 답한다."
	if err := os.WriteFile(soul, []byte(persona), 0o644); err != nil {
		t.Fatalf("seed SOUL.md: %v", err)
	}

	runPreference(t, dir, "숫자는 천단위 구분")

	body, _ := os.ReadFile(soul)
	// The human-authored persona must survive verbatim — the tool only appends.
	if !strings.HasPrefix(string(body), persona) {
		t.Fatalf("existing persona not preserved:\n%s", body)
	}
	if !strings.Contains(string(body), personaLearnedRulesHeading) {
		t.Fatalf("heading not appended:\n%s", body)
	}
	if !strings.Contains(string(body), "- 숫자는 천단위 구분") {
		t.Fatalf("rule not appended:\n%s", body)
	}
}

func TestPreferenceDedupesRepeatedRule(t *testing.T) {
	dir := t.TempDir()
	runPreference(t, dir, "이모지 쓰지 마")
	out := runPreference(t, dir, "이모지 쓰지 마")
	if !strings.Contains(out, "이미 저장된") {
		t.Fatalf("expected dedup notice, got: %s", out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "SOUL.md"))
	if got := strings.Count(string(body), "- 이모지 쓰지 마"); got != 1 {
		t.Fatalf("rule appended %d times, want 1:\n%s", got, body)
	}
}

func TestPreferenceDedupDoesNotFalsePositiveOnSubstring(t *testing.T) {
	dir := t.TempDir()
	// A longer rule that CONTAINS the shorter one as a substring.
	runPreference(t, dir, "이모지 쓰지 마세요 항상")
	// The shorter rule is a distinct preference — must still be appended, not
	// swallowed by a raw substring dedup.
	out := runPreference(t, dir, "이모지 쓰지 마")
	if !strings.Contains(out, "저장했습니다") {
		t.Fatalf("distinct substring rule was wrongly deduped: %s", out)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "SOUL.md"))
	if !strings.Contains(string(body), "- 이모지 쓰지 마\n") {
		t.Fatalf("shorter rule bullet missing:\n%s", body)
	}
	// But an exact repeat of the shorter rule IS deduped.
	out = runPreference(t, dir, "이모지 쓰지 마")
	if !strings.Contains(out, "이미 저장된") {
		t.Fatalf("exact repeat should dedup: %s", out)
	}
}

func TestPreferenceAppendsToAncestorSoulInsteadOfShadowing(t *testing.T) {
	root := t.TempDir()
	// Persona lives in an ANCESTOR of the workspace (mirrors LoadContextFiles'
	// ancestor inheritance). The workspace itself has no SOUL.md.
	ancestorSoul := filepath.Join(root, "SOUL.md")
	const persona = "# 페르소나\n\n담백하게.\n"
	if err := os.WriteFile(ancestorSoul, []byte(persona), 0o644); err != nil {
		t.Fatalf("seed ancestor SOUL.md: %v", err)
	}
	workspace := filepath.Join(root, "sub", "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runPreference(t, workspace, "항상 존댓말")

	// The rule must be appended to the ANCESTOR file the prompt actually loads,
	// not written into a new workspace-root SOUL.md that would shadow it.
	if _, err := os.Stat(filepath.Join(workspace, "SOUL.md")); !os.IsNotExist(err) {
		t.Fatalf("a shadowing workspace-root SOUL.md was created")
	}
	body, _ := os.ReadFile(ancestorSoul)
	if !strings.HasPrefix(string(body), persona) {
		t.Fatalf("ancestor persona not preserved:\n%s", body)
	}
	if !strings.Contains(string(body), "- 항상 존댓말") {
		t.Fatalf("rule not appended to ancestor SOUL.md:\n%s", body)
	}
}

func TestPreferenceRejectsEmptyRule(t *testing.T) {
	dir := t.TempDir()
	out := runPreference(t, dir, "   ")
	if !strings.Contains(out, "필수") {
		t.Fatalf("expected required-field guidance, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "SOUL.md")); !os.IsNotExist(err) {
		t.Fatalf("empty rule should not create SOUL.md")
	}
}

func TestPreferenceEnforcesSizeCap(t *testing.T) {
	dir := t.TempDir()
	soul := filepath.Join(dir, "SOUL.md")
	// Seed SOUL.md just under the cap so one more rule would exceed it.
	filler := strings.Repeat("x", personaSoulMaxBytes-20)
	if err := os.WriteFile(soul, []byte(filler), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := runPreference(t, dir, "이 규칙은 한도를 넘어서 저장되면 안 된다")
	if !strings.Contains(out, "한도") {
		t.Fatalf("expected size-cap guidance, got: %s", out)
	}
	body, _ := os.ReadFile(soul)
	if len(body) != len(filler) {
		t.Fatalf("SOUL.md was modified past the cap: len=%d", len(body))
	}
}
