package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitProgressLogSectionParsesSections(t *testing.T) {
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

	t.Run("nested date subheadings belong to the section", func(t *testing.T) {
		// ## 진행 로그 with ### date subheadings — the common markdown shape.
		// A deeper heading must NOT end the capture (it used to, leaving the
		// whole log on the 대표페이지); the next same-level heading does.
		content := "개요.\n\n## 진행 로그\n### 2026-07-02\n- 회의\n### 2026-07-03\n- 공정표 제출\n\n## 관련 문서\n- [[a]]"
		body, logLines := splitProgressLogSection(content)
		if !strings.Contains(logLines, "2026-07-03") || !strings.Contains(logLines, "공정표 제출") {
			t.Fatalf("nested subheadings must stay in the section, logLines=%q", logLines)
		}
		if strings.Contains(logLines, "관련 문서") {
			t.Fatalf("same-level heading must end the section, logLines=%q", logLines)
		}
		if strings.Contains(body, "진행 로그") || strings.Contains(body, "2026-07-03") || !strings.Contains(body, "관련 문서") {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("H3 section ends at the next H2", func(t *testing.T) {
		body, logLines := splitProgressLogSection("본문\n\n### 진행 로그\n- 사건\n\n## 다음 섹션\n내용")
		if logLines != "- 사건" {
			t.Fatalf("logLines = %q", logLines)
		}
		if !strings.Contains(body, "다음 섹션") {
			t.Fatalf("body = %q", body)
		}
	})
}

func TestIsDailyMailDigestPageReturnsExpectedFlag(t *testing.T) {
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

	created, updated, _, _, _ := wd.applyUpdates(context.Background(), []wikiUpdate{
		{
			Action: "create", Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지",
			Category: "프로젝트", Type: "entity", Confidence: "high",
			Content: "개요 본문.\n\n## 진행 로그\n- 2026-07-03: 공정표 제출 기한",
		},
		{
			Action: "create", Path: "기타/daily mail analysis.md", Title: "2026-07-03 일일 메일 분석",
			Category: "기타", Content: "다이제스트",
		},
	}, "")
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

// TestApplyUpdates_CountsUserModelPages pins the DreamReport.UserModelUpdated
// feed: 사용자-category writes (the user model) are counted separately from the
// overall created/updated counters — including a legacy 선호/ path folded onto
// 사용자/ by the category normalization — and non-사용자 writes are not.
func TestApplyUpdates_CountsUserModelPages(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	created, updated, userPages, _, _ := wd.applyUpdates(context.Background(), []wikiUpdate{
		{
			Action: "create", Path: "사용자/보고-형식-선호.md", Title: "보고 형식 선호",
			Category: "사용자", Type: "concept", Confidence: "high",
			Content: "산문 보고 선호 — 2026-07-09 발화 근거.",
		},
		{
			Action: "create", Path: "업무/구리값-동향.md", Title: "구리값 동향",
			Category: "업무", Content: "LME 구리값 동향 본문입니다.",
		},
	}, "")
	if created != 2 || updated != 0 {
		t.Fatalf("created=%d updated=%d, want 2/0", created, updated)
	}
	if userPages != 1 {
		t.Fatalf("userPages=%d, want 1 (사용자 write only)", userPages)
	}

	created, updated, userPages, _, _ = wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "update", Path: "선호/보고-형식-선호.md", Title: "보고 형식 선호",
		Category: "선호", Content: "상세 보고 선호로 전환 — 2026-07-09 갱신 근거 줄입니다.",
	}}, "")
	if created != 0 || updated != 1 {
		t.Fatalf("legacy-path update: created=%d updated=%d, want 0/1", created, updated)
	}
	if userPages != 1 {
		t.Fatalf("legacy-path update: userPages=%d, want 1 (선호/→사용자/ fold still counts)", userPages)
	}
}

func TestMergeUpdateContent(t *testing.T) {
	body := "# 당진\n\n## 요약\n당진 솔라빌리지 98MW EPC/O&M 수의계약 진행 건입니다.\n\n## 관련 문서\n- [[프로젝트/거래/hre]]\n"

	t.Run("drops verbatim duplicate lines and inserts before 관련 문서", func(t *testing.T) {
		content := "당진 솔라빌리지 98MW EPC/O&M 수의계약 진행 건입니다.\n공사기한 연장 수용 — COD 2028.3, 준공 2028.6 확정."
		merged := mergeUpdateContent(body, content)
		if strings.Count(merged, "수의계약 진행 건입니다") != 1 {
			t.Errorf("duplicate line must be dropped once:\n%s", merged)
		}
		relIdx := strings.Index(merged, "## 관련 문서")
		newIdx := strings.Index(merged, "COD 2028.3")
		if newIdx == -1 || relIdx == -1 || newIdx > relIdx {
			t.Errorf("new fact must land before 관련 문서:\n%s", merged)
		}
	})

	t.Run("all-duplicate content is a no-op", func(t *testing.T) {
		if merged := mergeUpdateContent(body, "당진 솔라빌리지 98MW EPC/O&M 수의계약 진행 건입니다."); merged != body {
			t.Errorf("no-op expected, got:\n%s", merged)
		}
	})

	t.Run("no boilerplate appends at end", func(t *testing.T) {
		merged := mergeUpdateContent("본문뿐", "새로 추가되는 충분히 긴 사실 한 줄입니다.")
		if !strings.HasSuffix(merged, "새로 추가되는 충분히 긴 사실 한 줄입니다.") {
			t.Errorf("append-at-end expected, got:\n%s", merged)
		}
	})

	t.Run("short repeated bullets pass through", func(t *testing.T) {
		merged := mergeUpdateContent("- 완료\n본문", "- 완료\n다른 새로운 내용이 추가로 붙는 줄입니다.")
		if !strings.Contains(merged, "- 완료\n본문") || strings.Count(merged, "- 완료") != 2 {
			t.Errorf("short lines must not be dropped:\n%s", merged)
		}
	})

	t.Run("substring of a longer body line is kept", func(t *testing.T) {
		// Line-level dedup only: this content line occurs INSIDE a longer body
		// sentence, so it is new standalone context and must survive (the old
		// substring check over-dropped it).
		longBody := "참고로 인터커넥션 승인 완료 상태입니다만 세부 일정은 미정입니다.\n"
		merged := mergeUpdateContent(longBody, "인터커넥션 승인 완료 상태입니다")
		if !strings.Contains(merged, "\n인터커넥션 승인 완료 상태입니다") {
			t.Errorf("substring-only match must not be dropped:\n%s", merged)
		}
	})

	t.Run("bullet restating a body sentence still dedups", func(t *testing.T) {
		merged := mergeUpdateContent(body, "- 당진 솔라빌리지 98MW EPC/O&M 수의계약 진행 건입니다.")
		if merged != body {
			t.Errorf("list-marker variant of an existing line must dedup, got:\n%s", merged)
		}
	})

	t.Run("현재 상태 and 핵심 숫자 replace instead of append", func(t *testing.T) {
		existing := "# 당진\n\n## 현재 상태\n- 어제 상태\n\n## 핵심 숫자\n- 옛 단가 100\n\n## 관련 문서\n- [[a]]\n"
		incoming := "## 현재 상태\n- 오늘 상태\n\n## 핵심 숫자\n- 새 단가 200\n\n공사기한 연장 수용 — COD 2028.3, 준공 2028.6 확정."
		merged := mergeUpdateContent(existing, incoming)
		if strings.Contains(merged, "어제 상태") || strings.Contains(merged, "옛 단가") {
			t.Fatalf("stale snapshot sections survived:\n%s", merged)
		}
		if strings.Count(merged, "## 현재 상태") != 1 || strings.Count(merged, "## 핵심 숫자") != 1 {
			t.Fatalf("sections must be upserted once:\n%s", merged)
		}
		if !strings.Contains(merged, "오늘 상태") || !strings.Contains(merged, "새 단가 200") {
			t.Fatalf("new snapshot missing:\n%s", merged)
		}
		relIdx := strings.Index(merged, "## 관련 문서")
		newIdx := strings.Index(merged, "COD 2028.3")
		if newIdx == -1 || relIdx == -1 || newIdx > relIdx {
			t.Fatalf("non-snapshot prose must still land before 관련 문서:\n%s", merged)
		}
	})
}

func TestApplyUpdates_UpdateFallbackDedup(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	// Existing page the model will mistarget with a slug/ID variant.
	if err := store.WritePage("프로젝트/해밀고흥솔라팜-모듈/대표.md", &Page{
		Meta: Frontmatter{ID: "haemil-solar", Title: "해밀고흥솔라팜 모듈", Category: "프로젝트", Type: "entity", Confidence: "high"},
		Body: "기존 본문",
	}); err != nil {
		t.Fatal(err)
	}

	created, updated, _, _, _ := wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "update", Path: "프로젝트/해밀고흥-솔라팜모듈/대표.md", ID: "haemil-solar",
		Title: "해밀고흥솔라팜 모듈", Category: "프로젝트", Content: "새 진행 사실이 추가되는 줄입니다.",
	}}, "")
	if created != 0 || updated != 1 {
		t.Fatalf("created=%d updated=%d, want 0/1 (retargeted to the existing page)", created, updated)
	}
	if pg, _ := store.ReadPage("프로젝트/해밀고흥-솔라팜모듈/대표.md"); pg != nil {
		t.Error("slug-variant duplicate page must not be created")
	}
	pg, err := store.ReadPage("프로젝트/해밀고흥솔라팜-모듈/대표.md")
	if err != nil || pg == nil || !strings.Contains(pg.Body, "새 진행 사실") {
		t.Fatalf("existing page should carry the update: %v", err)
	}
}

func TestApplyUpdates_LogReroutesAfterDedup(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	if err := store.WritePage("프로젝트/해밀고흥솔라팜-모듈/대표.md", &Page{
		Meta: Frontmatter{ID: "haemil-solar", Title: "해밀고흥솔라팜 모듈", Category: "프로젝트", Type: "entity", Confidence: "high"},
		Body: "기존 본문",
	}); err != nil {
		t.Fatal(err)
	}

	// A slug-variant create carrying a 진행 로그 section: the dedup retarget
	// must run FIRST so the rerouted log lands under the real project folder,
	// not a duplicate one named after the proposed path.
	created, updated, _, _, _ := wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "create", Path: "프로젝트/해밀고흥-솔라팜모듈/대표.md", ID: "haemil-solar",
		Title: "해밀고흥솔라팜 모듈", Category: "프로젝트",
		Content: "요약 갱신 내용입니다.\n\n## 진행 로그\n- 2026-07-03: 모듈 납기 확정",
	}}, "")
	if created != 0 || updated != 1 {
		t.Fatalf("created=%d updated=%d, want 0/1", created, updated)
	}
	logPage, err := store.ReadPage(LogPagePath("해밀고흥솔라팜-모듈"))
	if err != nil || logPage == nil || !strings.Contains(logPage.Body, "납기 확정") {
		t.Fatalf("log must land under the EXISTING project folder: %v", err)
	}
	if pg, _ := store.ReadPage(LogPagePath("해밀고흥-솔라팜모듈")); pg != nil {
		t.Error("log must not be filed under the duplicate slug-variant folder")
	}
}

// TestApplyUpdates_PreservesBatchPartialSuccessOrder pins the side-effect
// boundaries around a failed page write. Progress-log rerouting happens before
// the main page write, so that log survives; supersede marking happens after a
// successful write, so it must not run. A later update in the same batch still
// succeeds and contributes to the counters.
func TestApplyUpdates_PreservesBatchPartialSuccessOrder(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	if err := store.WritePage("업무/기존.md", &Page{
		Meta: Frontmatter{Title: "기존 문서", Category: "업무"},
		Body: "대체 전 사실",
	}); err != nil {
		t.Fatal(err)
	}

	// A directory at the final page path makes the representative-page write
	// fail while leaving its parent available for the earlier 로그.md reroute.
	failedRep := RepPagePath("실패프로젝트")
	if err := os.MkdirAll(filepath.Join(store.Dir(), failedRep), 0o755); err != nil {
		t.Fatal(err)
	}

	created, updated, userPages, oversized, _ := wd.applyUpdates(context.Background(), []wikiUpdate{
		{
			Action: "create", Path: failedRep, Title: "실패프로젝트",
			Category: "프로젝트", Supersedes: flexStringList{"업무/기존.md"},
			Content: "대표 본문\n\n## 진행 로그\n- 2026-07-12: 로그는 먼저 보존",
		},
		{
			Action: "create", Path: "업무/후속-성공.md", Title: "후속 성공",
			Category: "업무", Content: "앞선 실패 뒤에도 저장되는 사실",
		},
	}, "")
	if created != 1 || updated != 0 || userPages != 0 || len(oversized) != 0 {
		t.Fatalf("result = created:%d updated:%d user:%d oversized:%v, want 1/0/0/[]",
			created, updated, userPages, oversized)
	}

	logPage, err := store.ReadPage(LogPagePath("실패프로젝트"))
	if err != nil || logPage == nil || !strings.Contains(logPage.Body, "로그는 먼저 보존") {
		t.Fatalf("pre-write log side effect must survive the page failure: page=%v err=%v", logPage, err)
	}
	oldPage, err := store.ReadPage("업무/기존.md")
	if err != nil {
		t.Fatal(err)
	}
	if oldPage.Meta.SupersededBy != "" {
		t.Fatalf("failed write must not mark superseded, got %q", oldPage.Meta.SupersededBy)
	}
	if page, err := store.ReadPage("업무/후속-성공.md"); err != nil || page == nil {
		t.Fatalf("later batch item must still succeed: page=%v err=%v", page, err)
	}
}
