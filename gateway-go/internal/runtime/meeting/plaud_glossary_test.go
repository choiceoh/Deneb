package meeting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestExtractReportCorrectionPairsSkipsEstimates(t *testing.T) {
	report := `## 요약
- x
## 표기 교정
- 이마댐 → 임하댐
- 스티어 → 케이블 트레이 (추정)
- 비금도 → 비금도
관련프로젝트: 없음`
	pairs := ExtractReportCorrectionPairs(report)
	if len(pairs) != 1 || pairs[0].From != "이마댐" || pairs[0].To != "임하댐" {
		t.Fatalf("pairs = %#v", pairs)
	}
}

func TestForbiddenCorrectionNeqAndArrow(t *testing.T) {
	body := "- 오형석 ≠ 오선택\n- 매 → MW\n"
	if !ForbiddenCorrection(body, "오형석", "오선택") {
		t.Fatal("expected ≠ forbid")
	}
	if !ForbiddenCorrection(body, "매", "MW") {
		t.Fatal("expected arrow forbid")
	}
	if ForbiddenCorrection(body, "이마댐", "임하댐") {
		t.Fatal("unrelated pair must be allowed")
	}
}

func TestSlicePlaudGlossaryKeepsAlwaysAndHints(t *testing.T) {
	full := `## 1. 사용자 확인 교정
- 이마댐 → 임하댐
## 6. 공급
- ZTT
- 진코솔라
## 7. 진행 프로젝트
- 비금도 / 비금솔라
- 당진 솔라빌리지
`
	doNot := "- 오형석 ≠ 오선택\n"
	got := SlicePlaudGlossary(full, doNot, GlossaryHints{
		RecordingName: "비금 회의",
		Candidates:    []mailanalysis.ProjectCandidate{{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도"}},
	})
	for _, want := range []string{"교정 금지", "오형석", "이마댐", "비금도"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "진코솔라") && !strings.Contains(got, "비금") {
		t.Fatal("unexpected")
	}
	// Unrelated supplier line should drop when no hint
	if strings.Contains(got, "진코솔라") {
		t.Fatalf("unrelated term should be sliced out:\n%s", got)
	}
}

func TestPromotePlaudCorrectionsAppendsAndDedups(t *testing.T) {
	dir := t.TempDir()
	initial := "# gloss\n\n## 1. 사용자 확인\n- 이미있음 → 이미교정\n"
	if err := os.WriteFile(filepath.Join(dir, PlaudGlossaryFile), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PlaudDoNotCorrectFile), []byte("- 오형석 ≠ 오선택\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := PromotePlaudCorrections(dir, []CorrectionPair{
		{From: "이미있음", To: "이미교정"},
		{From: "오형석", To: "오선택"},
		{From: "당안리", To: "당암리"},
	}, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("promoted %d want 1", n)
	}
	body := LoadPlaudGlossary(dir)
	if !strings.Contains(body, plaudAutoPromoteHeading) || !strings.Contains(body, "당안리 → 당암리") {
		t.Fatalf("body:\n%s", body)
	}
	if strings.Contains(body, "오형석 → 오선택") {
		t.Fatal("forbidden pair must not promote")
	}
	n2, err := PromotePlaudCorrections(dir, []CorrectionPair{{From: "당안리", To: "당암리"}}, "abc123")
	if err != nil || n2 != 0 {
		t.Fatalf("dedup: n=%d err=%v", n2, err)
	}
}

func TestPromotablePairRejectsNoise(t *testing.T) {
	if promotablePair(CorrectionPair{From: "가", To: "나"}) {
		t.Fatal("single-rune pair must reject")
	}
	if !promotablePair(CorrectionPair{From: "이마댐", To: "임하댐"}) {
		t.Fatal("normal pair must accept")
	}
	if promotablePair(CorrectionPair{From: "foo", To: "a; b; c; d; e; f; g; h"}) {
		t.Fatal("explanation-like to must reject")
	}
}

func TestLoadPlaudGlossaryHotwords(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PlaudGlossaryFile), []byte("## 1\n- 이마댐 → 임하댐\n- 탑솔라\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadPlaudGlossaryHotwords(dir, 20)
	if !strings.Contains(got, "임하댐") || !strings.Contains(got, "탑솔라") {
		t.Fatalf("hotwords=%q", got)
	}
}
