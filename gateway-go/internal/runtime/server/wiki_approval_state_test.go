package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func approvalStateStore(t *testing.T, body string) (*wiki.Store, wiki.ProjectRef) {
	t.Helper()
	dir := t.TempDir()
	rel := filepath.Join("프로젝트", "pl2-dsv-epc-001", "대표.md")
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	store, err := wiki.NewStore(dir, "")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store, wiki.ProjectRef{Name: "당진 솔라빌리지", Path: rel}
}

// The approval prompt identified the project and then said nothing about it.
// 단가 이력 and 전례 answer whether the AMOUNT is right; only the project's own
// state answers whether the document should go out now.
func TestApprovalProjectStateCarriesSummaryAndStatus(t *testing.T) {
	store, ref := approvalStateStore(t, `---
title: 당진 솔라빌리지
summary: EPC 계약 체결 완료, 착공 대기
---

## 현재 상태
- 인허가 조건부 승인
- 기자재 조달 지연
`)
	got := approvalProjectStateContext(store, ref)
	for _, want := range []string{"당진 솔라빌리지", "착공 대기", "기자재 조달 지연"} {
		if !strings.Contains(got, want) {
			t.Fatalf("project state must carry %q:\n%s", want, got)
		}
	}
}

func TestApprovalProjectStateIsBestEffort(t *testing.T) {
	store, ref := approvalStateStore(t, "---\ntitle: t\n---\n본문만 있음\n")
	// No summary and no 현재 상태: emit nothing rather than an empty heading,
	// so the prompt keeps the plain project name exactly as before.
	if got := approvalProjectStateContext(store, ref); got != "" {
		t.Errorf("page without summary/status must yield empty, got %q", got)
	}
	if got := approvalProjectStateContext(nil, ref); got != "" {
		t.Errorf("nil store must yield empty, got %q", got)
	}
	if got := approvalProjectStateContext(store, wiki.ProjectRef{Name: "x"}); got != "" {
		t.Errorf("ref without a path must yield empty, got %q", got)
	}
	if got := approvalProjectStateContext(store, wiki.ProjectRef{Name: "x", Path: "없는/페이지.md"}); got != "" {
		t.Errorf("unreadable page must yield empty, got %q", got)
	}
}

func TestApprovalProjectStateIsBounded(t *testing.T) {
	store, ref := approvalStateStore(t, "---\ntitle: t\nsummary: "+
		strings.Repeat("가", 5000)+"\n---\n\n## 현재 상태\n"+
		strings.Repeat("나", 5000)+"\n")
	got := approvalProjectStateContext(store, ref)
	// truncateRunes cuts at the cap and then appends its own marker, so the
	// bound is cap + marker — assert the contract, not a guessed slack.
	const marker = "\n…(truncated)"
	if n := len([]rune(got)); n > approvalProjectStateMaxRune+len([]rune(marker)) {
		t.Fatalf("project state must stay bounded, got %d runes", n)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("truncation must be visible to the reader")
	}
}
