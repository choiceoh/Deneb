package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

// plaudListPayload mirrors the live plaud_list_files output shape
// (probed 2026-07-06 against @plaud-ai/mcp 0.3.x).
const plaudListPayload = `{
  "type": "list",
  "data": [
    {
      "id": "174d2f812c09ff81f9f95df708da938a",
      "name": "07-06 회의: 재생에너지 사업 계약·인허가·조달 리스크 관리",
      "created_at": "2026-07-06T03:06:54",
      "start_at": "2026-07-06T01:05:35",
      "duration": 2685000
    },
    {
      "id": "e2ed06775acf905cc668acb6ecddd6b9",
      "name": "Welcome to Plaud.ai",
      "created_at": "2026-06-22T02:10:22",
      "start_at": "2026-06-17T00:27:24.837000",
      "duration": 254000
    },
    {
      "id": "shortie",
      "name": "실수 탭",
      "created_at": "2026-07-06T04:00:00",
      "start_at": "2026-07-06T04:00:00",
      "duration": 30000
    }
  ],
  "page": 1,
  "page_size": 20
}`

func TestParsePlaudList(t *testing.T) {
	files, err := parsePlaudList(plaudListPayload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("want 3 files, got %d", len(files))
	}
	f := files[0]
	if f.ID != "174d2f812c09ff81f9f95df708da938a" {
		t.Errorf("id = %q", f.ID)
	}
	if f.Duration != 2685000*time.Millisecond {
		t.Errorf("duration = %v", f.Duration)
	}
	if want := time.Date(2026, 7, 6, 1, 5, 35, 0, time.UTC); !f.StartAt.Equal(want) {
		t.Errorf("start = %v, want %v", f.StartAt, want)
	}
	// Fractional-seconds variant must parse too.
	if files[1].StartAt.IsZero() {
		t.Error("fractional start_at failed to parse")
	}
}

func TestParsePlaudTranscript(t *testing.T) {
	inner, _ := json.Marshal([]map[string]any{
		{"content": "계약금 25%로 협의했습니다.", "speaker": "오형석"},
		{"content": "다음주까지 검토 회신드리겠습니다.", "speaker_id": float64(3)},
		{"content": "   "},
	})
	outer, _ := json.Marshal([]map[string]any{
		{"data_id": "x", "data_type": "transaction", "data_content": string(inner)},
	})
	got := parsePlaudTranscript(string(outer))
	want := "오형석: 계약금 25%로 협의했습니다.\n화자3: 다음주까지 검토 회신드리겠습니다."
	if got != want {
		t.Errorf("transcript = %q, want %q", got, want)
	}

	// Unknown shape degrades to raw text, never empty.
	if got := parsePlaudTranscript("완전 다른 텍스트 형식"); got != "완전 다른 텍스트 형식" {
		t.Errorf("fallback = %q", got)
	}

	// A wrapped JSON array (extra prefix/suffix around the payload — the
	// production first run hit this) must still parse via the span retry.
	wrapped := "도구 결과:\n" + string(outer) + "\n(끝)"
	if got := parsePlaudTranscript(wrapped); got != want {
		t.Errorf("wrapped payload = %q, want %q", got, want)
	}
}

func TestSplitRelatedProjectsIgnoresHallucinatedPaths(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/비금도-154kv/대표.md"},
		{Path: "프로젝트/영광-bess/대표.md"},
	}
	report := "## 요약\n- 진행\n\n관련프로젝트: 프로젝트/비금도-154kv/대표.md, 프로젝트/없는거/대표.md"
	body, related := splitRelatedProjects(report, cands)
	if strings.Contains(body, "관련프로젝트") {
		t.Errorf("trailer must be stripped from body: %q", body)
	}
	if len(related) != 1 || related[0] != "프로젝트/비금도-154kv/대표.md" {
		t.Errorf("related = %v (hallucinated paths must drop)", related)
	}

	body2, related2 := splitRelatedProjects("## 요약\n- x\n관련프로젝트: 없음", cands)
	if len(related2) != 0 || strings.Contains(body2, "관련프로젝트") {
		t.Errorf("없음 must yield no links, body2=%q", body2)
	}

	// No trailer at all → report unchanged.
	if body3, related3 := splitRelatedProjects("## 요약\n- x", cands); len(related3) != 0 || body3 != "## 요약\n- x" {
		t.Errorf("missing trailer mishandled: %q %v", body3, related3)
	}
}

func TestMeetingFilenameFormatsSlugWithFallback(t *testing.T) {
	f := plaudFile{ID: "174d2f812c09ff81f9f95df708da938a", Name: "07-06 회의: 재생에너지 리스크!"}
	got := meetingFilename(f)
	if got != "07-06-회의-재생에너지-리스크-174d2f81.md" {
		t.Errorf("filename = %q", got)
	}
	if got := meetingFilename(plaudFile{ID: "abcdef1234", Name: "!!!"}); got != "회의-abcdef12.md" {
		t.Errorf("empty slug fallback = %q", got)
	}
}

func newTestPlaudService(t *testing.T, exec func(ctx context.Context, name string, args json.RawMessage) (string, error)) (*plaudRecordingsService, *struct {
	pages    map[string]*wiki.Page
	appends  []string
	delivers []string
	systems  []string
},
) {
	t.Helper()
	sink := &struct {
		pages    map[string]*wiki.Page
		appends  []string
		delivers []string
		systems  []string
	}{pages: map[string]*wiki.Page{}}
	s := newPlaudRecordingsService(
		exec,
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			sink.systems = append(sink.systems, system)
			return "## 요약\n- 계약 리스크 논의\n## 결정사항\n- 없음\n## 액션 아이템\n- 없음\n## 리스크·미해결\n- 없음\n관련프로젝트: 프로젝트/비금도-154kv/대표.md", nil
		},
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			return "조각 요약", nil
		},
		func() []mailanalysis.ProjectCandidate {
			return []mailanalysis.ProjectCandidate{{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도"}}
		},
		func() string { return "탑솔라 — 태양광 EPC. 1팀: 공명한 차장" },
		func() string {
			return "## 1. 사용자 확인\n- 이마댐 → 임하댐\n## 7. 프로젝트\n- 비금도"
		},
		func() string { return "테스트 교정 지침: 용어집을 우선 적용한다." },
		"",  // topicsDir
		nil, // projectEntities
		func(relPath string, page *wiki.Page) error {
			sink.pages[relPath] = page
			return nil
		},
		func(projectPath, line, ref string, now time.Time) error {
			sink.appends = append(sink.appends, projectPath+"|"+ref)
			return nil
		},
		func(text string) (bool, error) {
			sink.delivers = append(sink.delivers, text)
			return true, nil
		},
		"", // in-memory state
		nil,
	)
	if s == nil {
		t.Fatal("service must construct")
	}
	return s, sink
}

func TestPlaudTickAnalyzesOnlyWhenNewRecordingAppears(t *testing.T) {
	transcript := strings.Repeat("오형석: 계약 조건 협의를 진행했습니다. ", 20)
	inner, _ := json.Marshal([]map[string]any{{"content": transcript}})
	outer, _ := json.Marshal([]map[string]any{{"data_content": string(inner)}})

	calls := map[string]int{}
	exec := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		calls[name]++
		switch name {
		case plaudListTool:
			return plaudListPayload, nil
		case plaudTranscriptTool:
			return string(outer), nil
		}
		t.Fatalf("unexpected tool %s", name)
		return "", nil
	}
	s, sink := newTestPlaudService(t, exec)

	// Tick 1: baseline — everything marked seen, nothing analyzed.
	s.tick(context.Background())
	if !s.baselined() {
		t.Fatal("first tick must baseline")
	}
	if calls[plaudTranscriptTool] != 0 || len(sink.pages) != 0 {
		t.Fatalf("baseline must not analyze: transcripts=%d pages=%d", calls[plaudTranscriptTool], len(sink.pages))
	}

	// A new recording appears; tick 2 analyzes exactly it.
	s.mu.Lock()
	delete(s.state.Seen, "174d2f812c09ff81f9f95df708da938a")
	s.mu.Unlock()
	s.tick(context.Background())

	if calls[plaudTranscriptTool] != 1 {
		t.Fatalf("want 1 transcript pull, got %d", calls[plaudTranscriptTool])
	}
	wantPath := "프로젝트/비금도-154kv/회의록/07-06-회의-재생에너지-사업-계약-인허가-조달-리스크-관리-174d2f81.md"
	page, ok := sink.pages[wantPath]
	if !ok {
		t.Fatalf("meeting page missing; pages=%v", keysOfPlaudPages(sink.pages))
	}
	if !strings.Contains(page.Body, "## 요약") || !strings.Contains(page.Body, "전사 발췌") {
		t.Errorf("page body incomplete:\n%s", page.Body)
	}
	if len(page.Meta.Related) != 1 || page.Meta.Related[0] != "프로젝트/비금도-154kv/대표.md" {
		t.Errorf("related = %v", page.Meta.Related)
	}
	if len(sink.systems) == 0 || !strings.Contains(sink.systems[0], "# 용어집") || !strings.Contains(sink.systems[0], "테스트 교정 지침") {
		t.Fatalf("synthesis system must include glossary + correction prompt; systems=%q", sink.systems)
	}
	if !strings.Contains(sink.systems[0], "비금도") {
		t.Fatalf("sliced glossary should keep project hint 비금도; system=%q", sink.systems[0])
	}
	if len(sink.appends) != 1 || !strings.Contains(sink.appends[0], "plaud:174d2f812c09ff81f9f95df708da938a") {
		t.Errorf("project status append = %v", sink.appends)
	}
	if len(sink.delivers) != 1 || !strings.Contains(sink.delivers[0], "회의 분석") {
		t.Errorf("feed delivery = %v", sink.delivers)
	}
	if !s.seen("174d2f812c09ff81f9f95df708da938a") {
		t.Error("analyzed recording must be marked seen")
	}
	// The synthesis prompt must carry the topic-knowledge block and the
	// correction appendix section (2026-07-06 bake-off outcome).
	if len(sink.systems) != 1 || !strings.Contains(sink.systems[0], "탑솔라") ||
		!strings.Contains(sink.systems[0], "## 표기 교정") {
		t.Errorf("synthesis system prompt missing knowledge/correction blocks")
	}

	// Tick 3: steady state — no rework.
	s.tick(context.Background())
	if calls[plaudTranscriptTool] != 1 || len(sink.delivers) != 1 {
		t.Errorf("steady state must not re-analyze: transcripts=%d delivers=%d",
			calls[plaudTranscriptTool], len(sink.delivers))
	}
}

// The 2026-07-20 incident: Plaud listed freshly synced recordings whose cloud
// transcripts were still minutes away; the empty transcript was mistaken for a
// silent recording and permanently skipped. An empty transcript must be
// retried, then analyzed as soon as the transcript materializes.
func TestPlaudEmptyTranscriptRetriesUntilReady(t *testing.T) {
	transcript := strings.Repeat("오형석: 계약 조건 협의를 진행했습니다. ", 20)
	inner, _ := json.Marshal([]map[string]any{{"content": transcript}})
	outer, _ := json.Marshal([]map[string]any{{"data_content": string(inner)}})

	ready := false
	exec := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		switch name {
		case plaudListTool:
			return plaudListPayload, nil
		case plaudTranscriptTool:
			if ready {
				return string(outer), nil
			}
			return "[]", nil // synced but not yet transcribed
		}
		t.Fatalf("unexpected tool %s", name)
		return "", nil
	}
	s, sink := newTestPlaudService(t, exec)
	s.tick(context.Background()) // baseline
	s.mu.Lock()
	delete(s.state.Seen, "174d2f812c09ff81f9f95df708da938a")
	s.mu.Unlock()

	s.tick(context.Background())
	if s.seen("174d2f812c09ff81f9f95df708da938a") {
		t.Fatal("not-ready transcript must not mark the recording seen")
	}
	if len(sink.delivers) != 0 || len(sink.pages) != 0 {
		t.Fatalf("no analysis output expected yet: delivers=%d pages=%d", len(sink.delivers), len(sink.pages))
	}

	ready = true
	s.tick(context.Background())
	if !s.seen("174d2f812c09ff81f9f95df708da938a") {
		t.Fatal("recording must be analyzed once the transcript is ready")
	}
	if len(sink.delivers) != 1 || len(sink.pages) != 1 {
		t.Fatalf("want 1 feed card + 1 wiki page, got delivers=%d pages=%d", len(sink.delivers), len(sink.pages))
	}
}

// A transcript that stays empty past the wait budget is a genuinely silent
// recording: give up quietly — mark seen, no quarantine card.
func TestPlaudEmptyTranscriptGivesUpQuietlyAfterBudget(t *testing.T) {
	exec := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		switch name {
		case plaudListTool:
			return plaudListPayload, nil
		case plaudTranscriptTool:
			return "[]", nil
		}
		t.Fatalf("unexpected tool %s", name)
		return "", nil
	}
	s, sink := newTestPlaudService(t, exec)
	s.tick(context.Background()) // baseline
	s.mu.Lock()
	delete(s.state.Seen, "174d2f812c09ff81f9f95df708da938a")
	s.mu.Unlock()

	for i := 0; i < plaudTranscriptWaitTicks; i++ {
		s.tick(context.Background())
	}
	if !s.seen("174d2f812c09ff81f9f95df708da938a") {
		t.Fatal("recording must be marked seen once the wait budget is spent")
	}
	if len(sink.delivers) != 0 {
		t.Fatalf("silent recording must not post any card, got %v", sink.delivers)
	}
	s.mu.Lock()
	leftover := len(s.state.Failures)
	s.mu.Unlock()
	if leftover != 0 {
		t.Errorf("failure counter must be cleared after give-up, got %d entries", leftover)
	}
}

// plaudListJSON builds a list_files payload with meeting-sized recordings.
func plaudListJSON(ids ...string) string {
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{
			"id":       id,
			"name":     "회의 " + id,
			"start_at": "2026-07-10T01:00:00",
			"duration": 1800000,
		})
	}
	out, _ := json.Marshal(map[string]any{"type": "list", "data": rows})
	return string(out)
}

// A full first page (>= the server's default cap) must trigger a second page
// request; a short page ends the loop with the union of both.
func TestPlaudListRecordingsPagesUntilShortPage(t *testing.T) {
	page1 := make([]string, 0, plaudListPageFloor)
	for i := 0; i < plaudListPageFloor; i++ {
		page1 = append(page1, "p1-"+string(rune('a'+i)))
	}
	page2 := []string{"p2-a", "p2-b", "p2-c"}

	var pages []float64
	exec := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		if name != plaudListTool {
			t.Fatalf("unexpected tool %s", name)
		}
		var a map[string]any
		if err := json.Unmarshal(args, &a); err != nil {
			t.Fatalf("args: %v", err)
		}
		if got, _ := a["page_size"].(float64); got != plaudListPageSize {
			t.Fatalf("page_size = %v, want %d", a["page_size"], plaudListPageSize)
		}
		page, _ := a["page"].(float64)
		pages = append(pages, page)
		if page == 1 {
			return plaudListJSON(page1...), nil
		}
		return plaudListJSON(page2...), nil
	}
	s, _ := newTestPlaudService(t, exec)
	files, err := s.listRecordings(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != len(page1)+len(page2) {
		t.Fatalf("want %d files, got %d", len(page1)+len(page2), len(files))
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("pages requested = %v, want [1 2]", pages)
	}
}

// When the tool ignores page params (documented for filtered calls) every page
// returns the same rows — the loop must stop as soon as a page adds nothing.
func TestPlaudListRecordingsStopsWhenPagingIgnored(t *testing.T) {
	rows := make([]string, 0, plaudListPageFloor)
	for i := 0; i < plaudListPageFloor; i++ {
		rows = append(rows, "same-"+string(rune('a'+i)))
	}
	calls := 0
	exec := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		calls++
		return plaudListJSON(rows...), nil
	}
	s, _ := newTestPlaudService(t, exec)
	files, err := s.listRecordings(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != len(rows) {
		t.Fatalf("want %d unique files, got %d", len(rows), len(files))
	}
	if calls != 2 {
		t.Fatalf("want exactly 2 list calls (page 2 adds nothing), got %d", calls)
	}
}

// A chunk whose gist call fails must degrade to a raw excerpt, not sink the
// whole reduce (the 2026-07-10 all-or-nothing fallback cut the meeting tail).
func TestPlaudReduceTranscriptSurvivesChunkFailure(t *testing.T) {
	s, _ := newTestPlaudService(t, func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		return "", nil
	})
	calls := 0
	s.gist = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		calls++
		if calls == 2 {
			return "", testError("output truncated at max_tokens")
		}
		if calls == 1 {
			return "첫 조각 요약", nil
		}
		return "마지막 조각 요약", nil
	}
	transcript := strings.Repeat("가", plaudChunkRunes) +
		strings.Repeat("나", plaudChunkRunes) +
		strings.Repeat("다", plaudChunkRunes/2)
	out, err := s.reduceTranscript(context.Background(), transcript, "# 회의 정보\n- 제목: 테스트 회의\n")
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 gist calls, got %d", calls)
	}
	if !strings.Contains(out, "첫 조각 요약") || !strings.Contains(out, "마지막 조각 요약") {
		t.Fatalf("healthy gists missing:\n%.200s", out)
	}
	if !strings.Contains(out, strings.Repeat("나", 100)) {
		t.Fatal("failed chunk must contribute a raw excerpt")
	}
	if strings.Contains(out, strings.Repeat("나", plaudChunkFallbackRunes+1)) {
		t.Fatal("raw excerpt must be bounded")
	}
}

func keysOfPlaudPages(m map[string]*wiki.Page) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPlaudSelectNewIgnoresSeenAndCapsCount(t *testing.T) {
	s, _ := newTestPlaudService(t, func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		return "", nil
	})
	s.mu.Lock()
	s.state.Baselined = true
	s.state.Seen["seen-1"] = 1
	s.mu.Unlock()

	base := time.Date(2026, 7, 6, 1, 0, 0, 0, time.UTC)
	files := []plaudFile{
		{ID: "seen-1", Name: "이미 봄", StartAt: base, Duration: time.Hour},
		{ID: "tiny", Name: "실수 탭", StartAt: base, Duration: 30 * time.Second},
		{ID: "d", Name: "네번째", StartAt: base.Add(4 * time.Hour), Duration: time.Hour},
		{ID: "b", Name: "두번째", StartAt: base.Add(2 * time.Hour), Duration: time.Hour},
		{ID: "a", Name: "첫번째", StartAt: base.Add(1 * time.Hour), Duration: time.Hour},
		{ID: "c", Name: "세번째", StartAt: base.Add(3 * time.Hour), Duration: time.Hour},
	}
	got := s.selectNew(files)
	if len(got) != plaudMaxPerTick {
		t.Fatalf("cap: want %d, got %d", plaudMaxPerTick, len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Errorf("order = %s,%s,%s (want oldest first)", got[0].ID, got[1].ID, got[2].ID)
	}
	if !s.seen("tiny") {
		t.Error("sub-minimum recording must be marked seen (never re-surface)")
	}
}

func TestPlaudAuthErrorThrottledNotice(t *testing.T) {
	execErr := func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		return "", context.DeadlineExceeded
	}
	s, sink := newTestPlaudService(t, execErr)
	now := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	authErr := testError("MCP error: 401 Not authenticated")
	s.handleToolError(authErr)
	s.handleToolError(authErr) // same day → throttled
	if len(sink.delivers) != 1 {
		t.Fatalf("want exactly 1 auth notice, got %d", len(sink.delivers))
	}
	now = now.Add(25 * time.Hour)
	s.handleToolError(authErr)
	if len(sink.delivers) != 2 {
		t.Fatalf("next-day auth notice must fire, got %d", len(sink.delivers))
	}

	// Unknown-tool (not wired) must stay quiet: no notice.
	s.handleToolError(testError(`unknown tool "plaud_list_files"`))
	if len(sink.delivers) != 2 {
		t.Error("unknown-tool path must not notify")
	}
}

type testError string

func (e testError) Error() string { return string(e) }

// Every reduce chunk must carry the meeting header + its position, so a chunk
// from the middle of a long meeting still knows which meeting it belongs to
// (an isolated chunk otherwise makes the gist model hedge or mislabel speakers).
func TestPlaudReduceTranscriptPrependsMeetingContext(t *testing.T) {
	s, _ := newTestPlaudService(t, func(ctx context.Context, name string, args json.RawMessage) (string, error) {
		return "", nil
	})
	var prompts []string
	s.gist = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		prompts = append(prompts, user)
		return "요약", nil
	}
	transcript := strings.Repeat("가\n", plaudChunkRunes) + strings.Repeat("나\n", plaudChunkRunes)
	header := "# 회의 정보\n- 제목: 무림 온산 협의\n- 일시: 2026-07-24 14:00 (KST)\n"
	if _, err := s.reduceTranscript(context.Background(), transcript, header); err != nil {
		t.Fatalf("reduce: %v", err)
	}
	if len(prompts) < 2 {
		t.Fatalf("want multiple chunks, got %d", len(prompts))
	}
	for i, p := range prompts {
		if !strings.Contains(p, "무림 온산 협의") {
			t.Errorf("chunk %d lost the meeting title: %.120s", i, p)
		}
		if !strings.Contains(p, fmt.Sprintf("구간: %d/%d", i+1, len(prompts))) {
			t.Errorf("chunk %d lost its position marker: %.120s", i, p)
		}
	}
}

// Chunk cuts snap back to a line break so a speaker's utterance is not sliced
// mid-sentence; an unbreakable run still falls back to the hard cut.
func TestSplitTranscriptChunksSnapsToUtteranceBoundary(t *testing.T) {
	// Utterance lines short relative to the chunk size — the realistic shape
	// (12k-rune chunks over speaker turns), so a break lands in the lookback tail.
	line := strings.Repeat("가", 11) + "\n" // 12 runes
	transcript := strings.Repeat(line, 25) // 300 runes
	chunks := splitTranscriptChunks([]rune(transcript), 100)
	if len(chunks) < 2 {
		t.Fatalf("want a split, got %d chunk(s)", len(chunks))
	}
	// No chunk may begin mid-utterance: every chunk after the first starts right
	// after a line break, which holds iff each prior chunk ends with one.
	for i, c := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(c, "\n") {
			t.Errorf("chunk %d ends mid-utterance (must snap to a line break): ...%q", i, lastRunes(c, 6))
		}
	}
	// Nothing may be lost or duplicated by the snap.
	if joined := strings.Join(chunks, ""); joined != transcript {
		t.Error("chunking must preserve the transcript exactly")
	}

	// One unbroken line: hard cut so progress is still made.
	hard := splitTranscriptChunks([]rune(strings.Repeat("나", 250)), 100)
	if len(hard) != 3 {
		t.Errorf("unbreakable run: want 3 hard-cut chunks, got %d", len(hard))
	}
}

// lastRunes returns the final n runes of s (rune-safe for error messages).
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
