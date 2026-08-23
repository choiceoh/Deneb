package wiki

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenQuestionsIn(t *testing.T) {
	body := `개요 문단.

## 현재 상태
- 7월 1일 착공

## 미해결 질문
- 2026-06-25 JA Solar 단가가 트리나 대비 어느 정도인가
- 납기가 하반기 착공 일정과 맞는지
- 2026-07-03
- 2026-07-03 IEC 61701 Severity 6 호환 여부

## 관련 문서
- [[프로젝트/x]]
`
	items := OpenQuestionsIn(body)
	if len(items) != 3 {
		t.Fatalf("items = %+v, want 3 (dated, undated, dated; date-only bullet dropped)", items)
	}
	if items[0].Asked != "2026-06-25" || items[0].Question != "JA Solar 단가가 트리나 대비 어느 정도인가" {
		t.Errorf("item[0] = %+v", items[0])
	}
	if items[1].Asked != "" || items[1].Question != "납기가 하반기 착공 일정과 맞는지" {
		t.Errorf("item[1] = %+v", items[1])
	}
	if items[2].Asked != "2026-07-03" {
		t.Errorf("item[2] = %+v", items[2])
	}

	if got := OpenQuestionsIn("섹션 없는 본문"); len(got) != 0 {
		t.Errorf("no-section body should parse empty, got %+v", got)
	}
}

func TestCollectStaleOpenQuestions_RejectsArchivedAndNonRepSortsUndatedFirst(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	store, err := NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.Local)

	mustWrite(t, store, "프로젝트/영산고/대표.md", &Page{
		Meta: Frontmatter{Title: "영산고"},
		Body: `개요.

## 미해결 질문
- 2026-06-25 단가 비교 필요
- 2026-07-03 납기 확인
- 담당자 연락처 확보
`,
	})
	// Archived project must be skipped entirely.
	mustWrite(t, store, "프로젝트/종결건/대표.md", &Page{
		Meta: Frontmatter{Title: "종결건", Archived: true},
		Body: "## 미해결 질문\n- 2026-01-01 옛 질문\n",
	})
	// Non-rep pages carrying the section are not scanned.
	mustWrite(t, store, "프로젝트/영산고/로그.md", &Page{
		Meta: Frontmatter{Title: "로그"},
		Body: "## 미해결 질문\n- 2026-01-01 로그의 질문\n",
	})

	got := CollectStaleOpenQuestions(wikiDir, 7, now)
	if len(got) != 2 {
		t.Fatalf("stale questions = %+v, want 2 (undated + 10d-old; 2d-old fresh)", got)
	}
	// Undated ("" sorts first) counts as oldest.
	if got[0].Question != "담당자 연락처 확보" || got[0].AgeDays != -1 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Question != "단가 비교 필요" || got[1].AgeDays != 10 || got[1].Project != "영산고" {
		t.Errorf("got[1] = %+v", got[1])
	}
	for _, q := range got {
		if q.Question == "옛 질문" || q.Question == "로그의 질문" {
			t.Errorf("archived/non-rep question leaked: %+v", q)
		}
	}
}

func TestExpireOpenQuestionsInBody_MovesDatedDropsStruckKeepsUndated(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.Local)
	body := `개요.

## 현재 상태
- 진행 중

## 미해결 질문
- 2026-08-01 오래된 납기 확인
- 담당자 연락처 확보
- 2026-08-20 최근 질문
- 2026-07-01 ~~이미 해결된 단가~~

## 관련 문서
- [[프로젝트/x]]
`
	next, moved, dropped, ok := expireOpenQuestionsInBody(body, now, 14)
	if !ok || moved != 1 || dropped != 1 {
		t.Fatalf("ok=%v moved=%d dropped=%d", ok, moved, dropped)
	}
	items := OpenQuestionsIn(next)
	if len(items) != 2 {
		t.Fatalf("remaining open=%+v want undated + recent", items)
	}
	if !containsQuestion(items, "담당자 연락처 확보") || !containsQuestion(items, "최근 질문") {
		t.Errorf("kept wrong questions: %+v", items)
	}
	if containsQuestion(items, "오래된 납기 확인") {
		t.Error("expired question still in 미해결 질문")
	}
	p := &Page{Body: next}
	_, secs := p.SplitByH2()
	var silence string
	for _, s := range secs {
		if s.Heading == vendorSilenceHeading {
			silence = s.Content
		}
	}
	if !strings.Contains(silence, "2026-08-01 오래된 납기 확인") {
		t.Errorf("벤더 침묵 missing expired bullet:\n%s", next)
	}
	if strings.Contains(next, "이미 해결된") {
		t.Error("struck bullet should be dropped, not moved")
	}
}

func TestExpireStaleOpenQuestions_RewritesRepPageOnly(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	store, err := NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.Local)
	mustWrite(t, store, "프로젝트/영산고/대표.md", &Page{
		Meta: Frontmatter{Title: "영산고"},
		Body: "## 미해결 질문\n- 2026-08-01 단가 비교 필요\n- 담당자 연락처 확보\n",
	})
	mustWrite(t, store, "프로젝트/영산고/로그.md", &Page{
		Meta: Frontmatter{Title: "로그"},
		Body: "## 미해결 질문\n- 2026-01-01 로그의 질문\n",
	})

	if n := store.ExpireStaleOpenQuestions(now, 14); n != 1 {
		t.Fatalf("ExpireStaleOpenQuestions=%d want 1", n)
	}
	got, err := store.ReadPage("프로젝트/영산고/대표.md")
	if err != nil {
		t.Fatal(err)
	}
	if OpenQuestionsIn(got.Body)[0].Question != "담당자 연락처 확보" {
		t.Errorf("rep open questions=%+v", OpenQuestionsIn(got.Body))
	}
	if !strings.Contains(got.Body, "## 벤더 침묵") || !strings.Contains(got.Body, "단가 비교 필요") {
		t.Errorf("rep body missing 벤더 침묵:\n%s", got.Body)
	}
	logPage, _ := store.ReadPage("프로젝트/영산고/로그.md")
	if !strings.Contains(logPage.Body, "로그의 질문") || strings.Contains(logPage.Body, "벤더 침묵") {
		t.Errorf("log page should be untouched: %s", logPage.Body)
	}
	if n := store.ExpireStaleOpenQuestions(now, 14); n != 0 {
		t.Fatalf("second expire=%d want 0", n)
	}
}

func containsQuestion(items []OpenQuestionItem, q string) bool {
	for _, it := range items {
		if it.Question == q {
			return true
		}
	}
	return false
}
