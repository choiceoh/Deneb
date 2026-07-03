package wiki

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSplitProgressLogSection(t *testing.T) {
	t.Run("cuts the section and keeps surrounding body", func(t *testing.T) {
		content := "요약 문단입니다.\n\n## 진행 로그\n- 2026-07-02: 회의 결과 연장 수용\n- 2026-07-03: 공정표 제출\n\n## 관련 문서\n- [[a]]"
		body, logLines := splitProgressLogSection(content)
		if !strings.Contains(logLines, "연장 수용") || strings.Contains(logLines, "관련 문서") {
			t.Fatalf("logLines = %q", logLines)
		}
		if strings.Contains(body, "진행 로그") || !strings.Contains(body, "관련 문서") || !strings.Contains(body, "요약 문단") {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("section at end of content", func(t *testing.T) {
		body, logLines := splitProgressLogSection("본문\n\n### 진행 로그\n- 사건 하나")
		if body != "본문" || logLines != "- 사건 하나" {
			t.Fatalf("got body=%q log=%q", body, logLines)
		}
	})

	t.Run("no section is a no-op", func(t *testing.T) {
		body, logLines := splitProgressLogSection("그냥 본문")
		if body != "그냥 본문" || logLines != "" {
			t.Fatalf("got body=%q log=%q", body, logLines)
		}
	})
}

func TestIsDailyMailDigestPage(t *testing.T) {
	positives := []struct{ title, path string }{
		{"daily mail analysis", "기타/daily mail analysis.md"},
		{"2026-07-02 일일 메일 분석", "기타/2026-07-02-일일-메일-분석.md"},
		{"메일 분석 (7/2)", "업무/메일-분석-0702.md"},
	}
	for _, p := range positives {
		if !isDailyMailDigestPage(p.title, p.path) {
			t.Errorf("(%q,%q) should be flagged as a daily digest", p.title, p.path)
		}
	}
	negatives := []struct{ title, path string }{
		{"당진 솔라빌리지 EPC 태양광", "프로젝트/당진-솔라빌리지/대표.md"},
		{"기아 AL 화성 PG국유지 모듈 RFx — 2026.06.15", "프로젝트/기아-화성/기아-al-화성.md"},
		{"진코솔라 계약 메일 분석 방법론", "업무/메일-분석-방법론.md"},              // no date
		{"발주 확인", "프로젝트/a/메일분석/TY7PR01MB1776@outlook.com.md"}, // system page: base is a msg id
	}
	for _, n := range negatives {
		if isDailyMailDigestPage(n.title, n.path) {
			t.Errorf("(%q,%q) must NOT be flagged", n.title, n.path)
		}
	}
}

func TestApplyUpdates_Guards(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	created, updated, _ := wd.applyUpdates(context.Background(), []wikiUpdate{
		{
			Action: "create", Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지",
			Category: "프로젝트", Type: "entity", Confidence: "high",
			Content: "개요 본문.\n\n## 진행 로그\n- 2026-07-03: 공정표 제출 기한",
		},
		{
			Action: "create", Path: "기타/daily mail analysis.md", Title: "2026-07-03 일일 메일 분석",
			Category: "기타", Content: "다이제스트",
		},
	})
	if created != 1 || updated != 0 {
		t.Fatalf("created=%d updated=%d, want 1/0 (digest skipped)", created, updated)
	}

	rep, err := store.ReadPage("프로젝트/당진-솔라빌리지/대표.md")
	if err != nil || rep == nil {
		t.Fatalf("rep page missing: %v", err)
	}
	if strings.Contains(rep.Body, "진행 로그") {
		t.Errorf("대표.md must not carry the 진행 로그 section, body=%q", rep.Body)
	}
	logPage, err := store.ReadPage(LogPagePath("당진-솔라빌리지"))
	if err != nil || logPage == nil {
		t.Fatalf("로그.md should be created by the reroute: %v", err)
	}
	if !strings.Contains(logPage.Body, "공정표 제출 기한") {
		t.Errorf("로그.md missing rerouted entry, body=%q", logPage.Body)
	}
	if _, err := store.ReadPage("기타/daily mail analysis.md"); err == nil {
		t.Error("daily digest page must not be created")
	}
}
