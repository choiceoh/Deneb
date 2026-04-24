package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// newSkillManageHarness spins up a ToolSkills function with an isolated
// workspaceDir + a counter so tests can assert whether the cache
// invalidation callback was invoked.
func newSkillManageHarness(t *testing.T) (fn ToolFunc, workspaceDir string, invalidateCount *int32) {
	t.Helper()
	workspaceDir = t.TempDir()
	// Pre-create the skills root so discovery paths work.
	if err := os.MkdirAll(filepath.Join(workspaceDir, "skills"), 0o755); err != nil {
		t.Fatalf("prep: %v", err)
	}
	invalidateCount = new(int32)
	invalidate := SkillManageInvalidateFn(func() {
		atomic.AddInt32(invalidateCount, 1)
	})
	// getSnapshot returns nil — list falls through gracefully in
	// toolSkillsList when no snapshot is available.
	snapshotFn := func() *skills.FullSkillSnapshot { return nil }
	fn = ToolSkills(snapshotFn, workspaceDir, invalidate)
	return fn, workspaceDir, invalidateCount
}

// validSkillContent returns a minimal SKILL.md with frontmatter that
// ExtractFrontmatterBlock will accept.
func validSkillContent(name string) string {
	return "---\n" +
		"name: " + name + "\n" +
		"version: \"0.1.0\"\n" +
		"category: coding\n" +
		"description: \"Test skill for " + name + "\"\n" +
		"---\n\n" +
		"# " + name + "\n\n" +
		"## When to Use\n- 테스트 목적\n"
}

// callSkillTool is a thin test helper that marshals args and invokes
// the skill_manage tool. The package already has a shared `callTool`
// in fs_test.go; we keep this distinct so signature tweaks in either
// test file stay isolated.
func callSkillTool(t *testing.T, fn ToolFunc, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return fn(context.Background(), raw)
}

func TestSkillManage_Create_DefersCacheByDefault(t *testing.T) {
	fn, workspace, invalidateCount := newSkillManageHarness(t)
	out, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "my-skill",
		"category": "coding",
		"content":  validSkillContent("my-skill"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "Created skill") {
		t.Errorf("expected success message, got: %s", out)
	}
	if !strings.Contains(out, "다음 세션부터") {
		t.Errorf("expected deferred-cache notice, got: %s", out)
	}
	if got := atomic.LoadInt32(invalidateCount); got != 0 {
		t.Errorf("expected invalidate NOT called when apply=false, got %d calls", got)
	}
	// File should exist on disk.
	skillPath := filepath.Join(workspace, "skills", "coding", "my-skill", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill file missing: %v", err)
	}
}

func TestSkillManage_Create_ApplyInvalidatesCache(t *testing.T) {
	fn, _, invalidateCount := newSkillManageHarness(t)
	out, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "apply-now",
		"category": "coding",
		"content":  validSkillContent("apply-now"),
		"apply":    true,
	})
	if err != nil {
		t.Fatalf("create apply: %v", err)
	}
	if !strings.Contains(out, "즉시 반영") {
		t.Errorf("expected apply-now notice, got: %s", out)
	}
	if got := atomic.LoadInt32(invalidateCount); got != 1 {
		t.Errorf("expected invalidate called once, got %d", got)
	}
}

func TestSkillManage_Create_RejectsMissingName(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"category": "coding",
		"content":  validSkillContent("x"),
	})
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestSkillManage_Create_RejectsInvalidCategory(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "bad-cat",
		"category": "made-up",
		"content":  validSkillContent("bad-cat"),
	})
	if err == nil {
		t.Error("expected error for invalid category")
	}
}

func TestSkillManage_Create_RejectsEmptyContent(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "nothing",
		"category": "coding",
		"content":  "",
	})
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestSkillManage_Create_RejectsMissingFrontmatter(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "no-fm",
		"category": "coding",
		"content":  "just a plain body no frontmatter",
	})
	if err == nil {
		t.Error("expected error when frontmatter is missing")
	}
}

func TestSkillManage_Read_AfterCreate(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "readme",
		"category": "coding",
		"content":  validSkillContent("readme"),
	})
	if err != nil {
		t.Fatalf("precondition create: %v", err)
	}

	out, err := callSkillTool(t, fn, map[string]any{
		"action": "read",
		"name":   "readme",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "name: readme") {
		t.Errorf("read did not return content, got: %s", out)
	}
}

func TestSkillManage_Delete(t *testing.T) {
	fn, workspace, invalidateCount := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "doomed",
		"category": "coding",
		"content":  validSkillContent("doomed"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := callSkillTool(t, fn, map[string]any{
		"action": "delete",
		"name":   "doomed",
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Deleted skill") {
		t.Errorf("missing delete confirmation, got: %s", out)
	}
	// Default apply=false so invalidate should NOT have been called.
	if got := atomic.LoadInt32(invalidateCount); got != 0 {
		t.Errorf("expected 0 invalidate calls on delete w/o apply, got %d", got)
	}
	if _, err := os.Stat(filepath.Join(workspace, "skills", "coding", "doomed")); !os.IsNotExist(err) {
		t.Errorf("skill directory not removed: %v", err)
	}
}

func TestSkillManage_Delete_NotFound(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action": "delete",
		"name":   "nonexistent",
	})
	if err == nil {
		t.Error("expected error when deleting non-existent skill")
	}
}

func TestSkillManage_Patch(t *testing.T) {
	fn, _, invalidateCount := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "patchme",
		"category": "coding",
		"content":  validSkillContent("patchme"),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := callSkillTool(t, fn, map[string]any{
		"action":   "patch",
		"name":     "patchme",
		"old_text": "# patchme",
		"new_text": "# patchme\n\nUpdated.",
		"apply":    true,
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !strings.Contains(out, "Patched skill") {
		t.Errorf("missing patch confirmation: %s", out)
	}
	if got := atomic.LoadInt32(invalidateCount); got != 1 {
		t.Errorf("expected invalidate called once (apply=true), got %d", got)
	}
}

func TestSkillManage_RejectsDuplicateCreate(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "dup",
		"category": "coding",
		"content":  validSkillContent("dup"),
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "dup",
		"category": "coding",
		"content":  validSkillContent("dup"),
	})
	if err == nil {
		t.Error("expected error on duplicate create")
	}
}

func TestSkillManage_KoreanBodyRoundtrips(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	body := "---\nname: 한글-테스트\nversion: \"0.1.0\"\ncategory: coding\ndescription: \"한국어 스킬\"\n---\n\n# 한글 스킬\n\n한국어 내용도 잘 저장되어야 합니다.\n"
	// Name is sanitized to ASCII-only, so it will become "" after sanitize
	// (Korean chars stripped) — we expect create to reject since the
	// name-after-sanitize is empty. Use an ASCII name instead.
	_ = body
	asciiBody := validSkillContent("korean-body")
	// Inject Korean content into the body portion.
	asciiBody = strings.Replace(asciiBody, "## When to Use\n- 테스트 목적\n",
		"## When to Use\n- 한국어 내용 정상 저장 검증\n", 1)

	_, err := callSkillTool(t, fn, map[string]any{
		"action":   "create",
		"name":     "korean-body",
		"category": "coding",
		"content":  asciiBody,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := callSkillTool(t, fn, map[string]any{
		"action": "read",
		"name":   "korean-body",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "한국어 내용 정상 저장 검증") {
		t.Errorf("Korean content not preserved: %s", out)
	}
}

func TestSkillManage_UnknownAction(t *testing.T) {
	fn, _, _ := newSkillManageHarness(t)
	out, err := callSkillTool(t, fn, map[string]any{
		"action": "bogus",
		"name":   "x",
	})
	// ToolSkills dispatches to the unknown branch with a helpful message,
	// not an error.
	if err != nil {
		t.Fatalf("unexpected error for unknown action: %v", err)
	}
	if !strings.Contains(out, "list, create, patch, delete, read, list_files") {
		t.Errorf("expected action-list hint, got: %s", out)
	}
}

func TestCacheAwareInvalidate_ApplyTrueCallsInner(t *testing.T) {
	var count int32
	inner := SkillManageInvalidateFn(func() { atomic.AddInt32(&count, 1) })
	effective := cacheAwareInvalidate(inner, true)
	effective()
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("apply=true: expected 1 inner call, got %d", got)
	}
}

func TestCacheAwareInvalidate_ApplyFalseSkipsInner(t *testing.T) {
	var count int32
	inner := SkillManageInvalidateFn(func() { atomic.AddInt32(&count, 1) })
	effective := cacheAwareInvalidate(inner, false)
	effective()
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("apply=false: expected 0 inner calls, got %d", got)
	}
}
