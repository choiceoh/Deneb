package meeting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestLoadPlaudGlossaryReadsFile(t *testing.T) {
	dir := t.TempDir()
	body := "# 용어집\n\n- 이마댐 → 임하댐\n"
	if err := os.WriteFile(filepath.Join(dir, PlaudGlossaryFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadPlaudGlossary(dir)
	if !strings.Contains(got, "이마댐 → 임하댐") {
		t.Fatalf("glossary = %q", got)
	}
}

func TestLoadPlaudGlossaryMissingIsEmpty(t *testing.T) {
	if got := LoadPlaudGlossary(t.TempDir()); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestLoadPlaudCorrectionPromptFallsBackToDefault(t *testing.T) {
	got := LoadPlaudCorrectionPrompt(t.TempDir())
	if !strings.Contains(got, "ASR 교정기") {
		t.Fatalf("default prompt missing: %q", got)
	}
}

func TestLoadPlaudCorrectionPromptPrefersFile(t *testing.T) {
	dir := t.TempDir()
	custom := "커스텀 교정 프롬프트 본문"
	if err := os.WriteFile(filepath.Join(dir, PlaudCorrectionPromptFile), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadPlaudCorrectionPrompt(dir)
	if got != custom {
		t.Fatalf("got %q want %q", got, custom)
	}
}

func TestAnalyzeMeetingInjectsGlossaryAndCorrectionPrompt(t *testing.T) {
	var gotSystem string
	s := newPlaudRecordingsService(
		func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "", nil
		},
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			gotSystem = system
			return "## 요약\n- x\n## 결정사항\n- 없음\n## 액션 아이템\n- 없음\n## 리스크·미해결\n- 없음\n관련프로젝트: 없음", nil
		},
		nil,
		func() []mailanalysis.ProjectCandidate { return nil },
		func() string { return "배경: 탑솔라" },
		func() string {
			return "## 1. 사용자 확인\n- 이마댐 → 임하댐\n## 7. 프로젝트\n- 비금도 / 비금솔라"
		},
		func() string { return "교정지침커스텀" },
		"",
		func(paths []string) []ProjectEntityFacts {
			return []ProjectEntityFacts{{
				Path:   "프로젝트/비금도-154kv/대표.md",
				Title:  "비금도 154kV",
				Client: "ZTT",
				Sites:  []string{"전남 신안군 임자면"},
				People: []string{"오선택", "이시연"},
				Orgs:   []string{"남도에코에너지"},
			}}
		},
		func(relPath string, page *wiki.Page) error { return nil },
		nil,
		func(text string) (bool, error) { return true, nil },
		"",
		nil,
	)
	f := plaudFile{Name: "비금 케이블 회의", StartAt: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC), Duration: 10 * time.Minute}
	cands := []mailanalysis.ProjectCandidate{{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도"}}
	if _, err := s.analyzeMeeting(context.Background(), f, strings.Repeat("발언 내용입니다. ", 30), cands); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# 배경지식", "배경: 탑솔라",
		"# 프로젝트 연관 고유명", "ZTT", "이시연", "임자면",
		"# 용어집", "비금도",
		"# 교정 지침", "교정지침커스텀", "## 요약",
	} {
		if !strings.Contains(gotSystem, want) {
			t.Fatalf("system missing %q:\n%s", want, gotSystem)
		}
	}
}
