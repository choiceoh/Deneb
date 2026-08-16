package wiki

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestWikiDreamerScanDiariesReadsFromOffsets(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(store.DiaryDir(), 0o755); err != nil {
		t.Fatalf("mkdir diary: %v", err)
	}
	diaryPath := filepath.Join(store.DiaryDir(), "diary-2026-05-05.md")
	if err := os.WriteFile(diaryPath, []byte("\n## 10:00\n\nfirst\n"), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	scan1, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan1: %v", err)
	}
	if scan1 == nil || !strings.Contains(scan1.Content, "first") {
		t.Fatalf("scan1 content = %q, want first entry", scanContent(scan1))
	}
	if err := wd.saveDiaryProcessState(scan1.State); err != nil {
		t.Fatalf("save state: %v", err)
	}

	f, err := os.OpenFile(diaryPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open diary append: %v", err)
	}
	if _, err := f.WriteString("\n## 11:00\n\nsecond\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append diary: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close diary: %v", err)
	}

	scan2, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if scan2 == nil || !strings.Contains(scan2.Content, "second") {
		t.Fatalf("scan2 content = %q, want second entry", scanContent(scan2))
	}
	if strings.Contains(scan2.Content, "first") {
		t.Fatalf("scan2 replayed already processed entry: %q", scan2.Content)
	}
}

func TestWikiDreamerScanDiariesReadsSameDayDespiteCutoff(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	store.index.LastProcessed = "2026-05-05" // pre-concurrency test setup: direct field poke
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(store.DiaryDir(), 0o755); err != nil {
		t.Fatalf("mkdir diary: %v", err)
	}
	diaryPath := filepath.Join(store.DiaryDir(), "diary-2026-05-05.md")
	if err := os.WriteFile(diaryPath, []byte("\n## 20:00\n\nsame-day entry\n"), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	scan, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan == nil || !strings.Contains(scan.Content, "same-day entry") {
		t.Fatalf("scan content = %q, want same-day entry", scanContent(scan))
	}
}

func TestProcessedDiaryCapsulesAreCappedAndFormatted(t *testing.T) {
	var capsules []processedDiaryCapsule
	for i := 0; i < scoredCapsuleLimit+3; i++ {
		capsules = appendProcessedDiaryCapsule(capsules, processedDiaryCapsule{
			At:        "2026-05-05T00:00:00Z",
			DiaryDate: "2026-05-" + twoDigit(i+1),
			Proposed:  1,
			Created:   i % 2,
			Updated:   1,
			Paths:     []string{"프로젝트/deneb.md", "프로젝트/deneb.md"},
		})
	}
	if len(capsules) != scoredCapsuleLimit {
		t.Fatalf("capsule count = %d, want %d (storage cap)", len(capsules), scoredCapsuleLimit)
	}
	formatted := formatProcessedDiaryCapsules(capsules)
	if strings.Contains(formatted, "2026-05-01") {
		t.Fatalf("expected oldest capsules to be capped, got %q", formatted)
	}
	if !strings.Contains(formatted, "proposed=1") || !strings.Contains(formatted, "프로젝트/deneb.md") {
		t.Fatalf("expected compact processed history, got %q", formatted)
	}
	if strings.Count(formatted, "프로젝트/deneb.md") != processedCapsuleLimit {
		t.Fatalf("expected duplicated paths to be deduped per capsule, got %q", formatted)
	}
}

func TestDreamProposalReportWritesPreview(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scan := &diaryScanResult{Content: "diary content", LatestDate: "2026-05-05"}
	report := buildDreamProposalReport(scan, []wikiUpdate{{
		Action:     "create",
		Path:       "프로젝트/deneb.md",
		Title:      "Deneb",
		Summary:    "기억 개선",
		Category:   "프로젝트",
		Type:       "concept",
		Confidence: "medium",
		Importance: 0.8,
		Related:    []string{"결정/memory.md", "결정/memory.md"},
		Content:    strings.Repeat("긴 내용 ", 120),
	}})
	report.Applied = dreamApplySummary{Created: 1}

	if err := wd.saveDreamProposalReport(report); err != nil {
		t.Fatalf("saveDreamProposalReport: %v", err)
	}
	data, err := os.ReadFile(wd.dreamProposalPath())
	if err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	var got dreamProposalReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("proposal json: %v", err)
	}
	if got.LatestDiaryDate != "2026-05-05" || got.Applied.Created != 1 {
		t.Fatalf("unexpected report metadata: %+v", got)
	}
	if len(got.Proposed) != 1 || got.Proposed[0].Path != "프로젝트/deneb.md" {
		t.Fatalf("unexpected proposals: %+v", got.Proposed)
	}
	if len(got.Proposed[0].Related) != 1 {
		t.Fatalf("expected related paths to be deduped: %+v", got.Proposed[0].Related)
	}
	if len([]rune(got.Proposed[0].ContentHint)) > 323 {
		t.Fatalf("content hint too long: %d", len([]rune(got.Proposed[0].ContentHint)))
	}
}

// TestWikiUpdateSupersedesAcceptsStringOrArray guards the synthesis parse bug:
// the LLM emits `supersedes` as either a single string or an array, and an
// array used to crash the whole dream cycle ("cannot unmarshal array into ...
// of type string"). Both must parse now (flexStringList, like tags/related).
func TestWikiUpdateSupersedesAcceptsStringOrArray(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{`{"supersedes":"old/page.md"}`, []string{"old/page.md"}},
		{`{"supersedes":["a/p.md","b/q.md"]}`, []string{"a/p.md", "b/q.md"}},
		{`{"supersedes":""}`, nil},
		{`{}`, nil},
	}
	for _, c := range cases {
		var u wikiUpdate
		if err := json.Unmarshal([]byte(c.raw), &u); err != nil {
			t.Fatalf("Unmarshal(%s): %v (the array case used to fail here)", c.raw, err)
		}
		if len(u.Supersedes) != len(c.want) {
			t.Fatalf("%s → %v, want %v", c.raw, u.Supersedes, c.want)
		}
		for i := range c.want {
			if u.Supersedes[i] != c.want[i] {
				t.Fatalf("%s → %v, want %v", c.raw, u.Supersedes, c.want)
			}
		}
	}
}

// TestWikiUpdateConfidenceAcceptsStringOrNumber guards the 2026-07-20 synthesis
// parse bug: the LLM emits `confidence` as either a label string or a numeric
// score (0.9 — conflating it with importance), and a number used to skip the
// whole update item ("cannot unmarshal number into ... of type string"), losing
// valid knowledge. Numbers must fold onto the label scale (flexConfidence).
func TestWikiUpdateConfidenceAcceptsStringOrNumber(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"confidence":"high"}`, "high"},
		{`{"confidence":"medium"}`, "medium"},
		{`{"confidence":0.9}`, "high"},
		{`{"confidence":0.8}`, "high"},
		{`{"confidence":1}`, "high"},
		{`{"confidence":0.7}`, "medium"},
		{`{"confidence":0.5}`, "medium"},
		{`{"confidence":0.3}`, "low"},
		{`{"confidence":0}`, "low"},
		{`{"confidence":null}`, ""},
		{`{}`, ""},
	}
	for _, c := range cases {
		var u wikiUpdate
		if err := json.Unmarshal([]byte(c.raw), &u); err != nil {
			t.Fatalf("Unmarshal(%s): %v (the numeric case used to fail here)", c.raw, err)
		}
		if string(u.Confidence) != c.want {
			t.Fatalf("%s → %q, want %q", c.raw, u.Confidence, c.want)
		}
	}
}

// TestParseWikiUpdates_NumericConfidenceNotSkipped pins the parse-level effect
// of the same bug: a numeric confidence must not cost the item its slot in the
// applied batch (7 consecutive skips were observed in one dream cycle).
func TestParseWikiUpdates_NumericConfidenceNotSkipped(t *testing.T) {
	text := `[
		{"action":"create","path":"a.md","title":"A","confidence":0.9},
		{"action":"create","path":"b.md","title":"B","confidence":"medium"},
		{"action":"create","path":"c.md","title":"C"}
	]`
	updates, partial, err := parseWikiUpdates(text, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if partial {
		t.Error("intact array must not report partial")
	}
	if len(updates) != 3 {
		t.Fatalf("expected all 3 items to survive, got %d: %+v", len(updates), updates)
	}
	for i, want := range []string{"high", "medium", ""} {
		if string(updates[i].Confidence) != want {
			t.Errorf("updates[%d].Confidence = %q, want %q", i, updates[i].Confidence, want)
		}
	}
}

// TestParseWikiUpdates_ObjectShapes guards the 2026-07-20 object-response bug:
// the LLM sometimes ignores the bare-array contract and emits a wrapper object
// or a single update object — both used to fail the whole cycle into an 8h
// backoff. Both must unwrap; ambiguous objects still error.
func TestParseWikiUpdates_ObjectShapes(t *testing.T) {
	t.Run("wrapper object unwraps its array field", func(t *testing.T) {
		text := `{"updates": [
			{"action":"create","path":"a.md","title":"A"},
			{"action":"update","path":"b.md","title":"B"}
		]}`
		updates, partial, err := parseWikiUpdates(text, nil)
		if err != nil {
			t.Fatalf("parse: %v (wrapper object used to fail here)", err)
		}
		if partial {
			t.Error("intact wrapper must not report partial")
		}
		if len(updates) != 2 || updates[0].Path != "a.md" {
			t.Fatalf("updates = %+v, want both items", updates)
		}
	})

	t.Run("single update object becomes a one-item batch", func(t *testing.T) {
		updates, partial, err := parseWikiUpdates(`{"action":"create","path":"a.md","title":"A"}`, nil)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if partial || len(updates) != 1 || updates[0].Path != "a.md" {
			t.Fatalf("updates = %+v partial=%v, want the single item", updates, partial)
		}
	})

	t.Run("truncated wrapper salvages the complete prefix", func(t *testing.T) {
		text := `{"updates": [
			{"action":"create","path":"a.md","title":"A"},
			{"action":"create","path":"b.md","title":"B","content":"잘리는 중`
		updates, partial, err := parseWikiUpdates(text, nil)
		if err != nil {
			t.Fatalf("expected salvage, got error: %v", err)
		}
		if !partial {
			t.Error("truncated wrapper must report partial")
		}
		if len(updates) != 1 || updates[0].Path != "a.md" {
			t.Fatalf("updates = %+v, want the one complete item", updates)
		}
	})

	t.Run("object with no array and no item fields still errors", func(t *testing.T) {
		if _, _, err := parseWikiUpdates(`{"note":"no updates here"}`, nil); err == nil {
			t.Fatal("expected error for an unrecognizable object")
		}
	})
}

// TestNormalizeDreamAction pins the synonym fold: create/update variants map
// onto the contract, empty defaults to update (create-on-missing), and
// deletion-like or unknown verbs stay unknown so they are dropped, never
// guessed into a destructive write.
func TestNormalizeDreamAction(t *testing.T) {
	cases := map[string]string{
		"create": "create", "New": "create",
		"update": "update", "append": "update", "Merge": "update",
		"modify": "update", "edit": "update", "revise": "update", "add": "update",
		"": "update", " Update ": "update",
		"delete": "", "remove": "", "supersede": "", "rename": "",
	}
	for in, want := range cases {
		if got := normalizeDreamAction(in); got != want {
			t.Errorf("normalizeDreamAction(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDreamTransientSynthesisBackoff verifies the short-retry ladder: a
// transport-class synthesis failure holds ShouldDream for the retry delay
// without consuming triggers, and the budget escalates to the full backoff
// after wikiDreamTransientRetryMax consecutive misses (12 of 14 synthesis
// failures in the 2026-07-20 week were transient and each cost a full 8h).
func TestDreamTransientSynthesisBackoff(t *testing.T) {
	wd := &WikiDreamer{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	wd.turnCount = wikiDreamTurnThreshold // trigger demand present

	if !wd.ShouldDream(context.Background()) {
		t.Fatal("turn threshold should trigger before any failure")
	}
	for i := 1; i <= wikiDreamTransientRetryMax; i++ {
		if !wd.noteTransientSynthesisFailure() {
			t.Fatalf("failure %d should stay within the short-retry budget", i)
		}
		if wd.ShouldDream(context.Background()) {
			t.Fatalf("ShouldDream must be held during the retry delay (failure %d)", i)
		}
		// Simulate the delay passing: the hold expires, demand re-fires.
		wd.cmu.Lock()
		wd.synthRetryNotBefore = time.Now().Add(-time.Second)
		wd.cmu.Unlock()
		if !wd.ShouldDream(context.Background()) {
			t.Fatalf("expired hold must release the trigger (failure %d)", i)
		}
	}
	if wd.noteTransientSynthesisFailure() {
		t.Fatal("budget exhausted: the caller must fall back to the full-interval backoff")
	}
	wd.clearTransientSynthesisFailures()
	if wd.synthTransientFails != 0 || !wd.synthRetryNotBefore.IsZero() {
		t.Fatal("success must clear the transient-failure state")
	}
	if !wd.ShouldDream(context.Background()) {
		t.Fatal("cleared state must release the trigger")
	}
}

// TestParseWikiUpdates_SkipsMalformedItem verifies one malformed update is
// skipped while the well-formed ones still apply — the whole batch used to fail
// on a single bad field and, if deterministic, stall the diary pipeline (the
// structural generalization of the #2341 supersedes fix).
func TestParseWikiUpdates_SkipsMalformedItem(t *testing.T) {
	text := `[
		{"action":"create","path":"good/a.md","title":"A"},
		{"action":"create","path":"bad.md","title":"B","importance":"not-a-number"},
		{"action":"update","path":"good/c.md","title":"C","supersedes":["x.md","y.md"]}
	]`
	updates, partial, err := parseWikiUpdates(text, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if partial {
		t.Error("per-item skips on an intact array must not report partial")
	}
	if len(updates) != 2 {
		t.Fatalf("expected 2 well-formed updates (1 skipped), got %d: %+v", len(updates), updates)
	}
	if updates[0].Path != "good/a.md" || updates[1].Path != "good/c.md" {
		t.Fatalf("wrong updates kept: %+v", updates)
	}
	if len(updates[1].Supersedes) != 2 {
		t.Fatalf("supersedes array should still parse: %+v", updates[1].Supersedes)
	}
}

func TestParseWikiUpdates_DecodesStringEncodedItem(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	text := `[
		{"action":"create","path":"good/a.md","title":"A"},
		"{\"action\":\"update\",\"path\":\"good/b.md\",\"title\":\"B\"}",
		"not an update object"
	]`
	updates, partial, err := parseWikiUpdates(text, logger)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if partial {
		t.Error("string-encoded items in an intact array must not report partial")
	}
	if len(updates) != 2 {
		t.Fatalf("expected regular and string-encoded updates, got %d: %+v", len(updates), updates)
	}
	if updates[0].Path != "good/a.md" || updates[1].Path != "good/b.md" {
		t.Fatalf("wrong updates kept: %+v", updates)
	}
	if got := logs.String(); strings.Contains(got, "skipped malformed update item") ||
		!strings.Contains(got, "synthesis dropped malformed items") {
		t.Fatalf("string item log classification = %q", got)
	}
}

// TestParseWikiUpdates_NonArrayIsError verifies a non-array response is a
// genuine total failure (caller backs off and re-consumes the diary content).
func TestParseWikiUpdates_NonArrayIsError(t *testing.T) {
	if _, _, err := parseWikiUpdates(`{"not":"an array"}`, nil); err == nil {
		t.Fatal("expected error for non-array response")
	}
}

func TestBuildWikiSynthesisPromptRendersCompoundingRules(t *testing.T) {
	prompt := buildWikiSynthesisPrompt("index", "history", "", "", "", "diary")
	for _, want := range []string{
		"상호링크",
		"모순/갱신",
		"지식 정리",
		"supersedes",
		"[[경로-또는-제목]]",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("synthesis prompt missing %q", want)
		}
	}
}

// TestBuildWikiSynthesisPromptIncludesPreferenceRules pins the user-model
// learning directives ported from the agent-memory papers: behavioral-pattern
// abstraction (Evo-Memory/ReMem arXiv:2511.20857 — derive working-style from
// recurring behavior, not just stated preferences) and fact-level preference
// replacement (Mem0 arXiv:2504.19413 — update the value in place so a 사용자 page
// is a current policy, not an accumulating log). The recurrence gate applies to
// INFERRED rules only — a stated standing preference records on first
// occurrence — and the confidence split plus the transient-instruction guard
// keep the profile conservative + operator-reviewable. The facet taxonomy
// (소통/리듬/포맷/성향/컨텍스트) keeps 사용자 pages small and per-axis.
func TestBuildWikiSynthesisPromptRendersPreferenceRules(t *testing.T) {
	prompt := buildWikiSynthesisPrompt("index", "history", "", "", "", "diary")
	for _, want := range []string{
		"working-style 추론", // behavioral abstraction directive present
		"2회 이상 반복",         // recurrence gate for inferred rules (no single-shot / speculative inference)
		"현행 정책",            // fact-level replacement: 사용자 page is current policy, not a log
		"현행 프로필",           // user-model facet taxonomy (per-axis pages)
		"1회로도 즉시 기록",       // stated standing preferences record on first occurrence
		"이번만",              // transient-instruction guard (one-offs are not preferences)
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("synthesis prompt missing preference rule %q", want)
		}
	}
}

func scanContent(scan *diaryScanResult) string {
	if scan == nil {
		return ""
	}
	return scan.Content
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func TestParseWikiUpdates_SalvagesDamagedArray(t *testing.T) {
	t.Run("mid-string truncation keeps preceding items", func(t *testing.T) {
		text := `[
  {"action":"update","path":"프로젝트/a/대표.md","title":"A","content":"본문"},
  {"action":"create","path":"프로젝트/b/대표.md","title":"B","content":"잘리는 중`
		updates, partial, err := parseWikiUpdates(text, nil)
		if err != nil {
			t.Fatalf("expected salvage, got error: %v", err)
		}
		if !partial {
			t.Error("salvaged array must report partial")
		}
		if len(updates) != 1 || updates[0].Path != "프로젝트/a/대표.md" {
			t.Fatalf("updates = %+v, want the one complete item", updates)
		}
	})

	t.Run("unescaped quote inside a value keeps preceding items", func(t *testing.T) {
		// The 2026-07-03 failure shape: a closed value followed by stray text.
		text := `[
  {"action":"update","path":"프로젝트/a/대표.md","title":"A"},
  {"action":"update","path":"프로젝트/b/대표.md","summary":"98MW EPC — "회의" 이후"}
]`
		updates, partial, err := parseWikiUpdates(text, nil)
		if err != nil {
			t.Fatalf("expected salvage, got error: %v", err)
		}
		if !partial {
			t.Error("salvaged array must report partial")
		}
		if len(updates) != 1 || updates[0].Path != "프로젝트/a/대표.md" {
			t.Fatalf("updates = %+v, want the one complete item", updates)
		}
	})

	t.Run("first element already damaged still errors", func(t *testing.T) {
		if _, _, err := parseWikiUpdates(`[{"broken": }`, nil); err == nil {
			t.Fatal("expected error when nothing is salvageable")
		}
	})

	t.Run("trailing junk after a complete array is not partial", func(t *testing.T) {
		text := `[
  {"action":"update","path":"프로젝트/a/대표.md","title":"A","content":"본문"},
  {"action":"create","path":"프로젝트/b/대표.md","title":"B","content":"본문"}
]
이상으로 위키 갱신 제안을 마칩니다.`
		updates, partial, err := parseWikiUpdates(text, nil)
		if err != nil {
			t.Fatalf("expected clean parse, got error: %v", err)
		}
		if partial {
			t.Error("complete array + trailing junk must not report partial (nothing was lost)")
		}
		if len(updates) != 2 {
			t.Fatalf("updates = %+v, want both items", updates)
		}
	})
}

func TestWikiDreamerLLMRequestDisablesThinkingForHeadroomModel(t *testing.T) {
	wd := &WikiDreamer{synthesisMaxTokens: 16384}
	req := wd.llmRequest("system", "prompt", 2048)

	if req.Thinking == nil || req.Thinking.Type != "disabled" {
		t.Fatalf("Thinking = %#v, want disabled for a reasoning model without a template toggle", req.Thinking)
	}
	if req.MaxTokens != 8192 {
		t.Fatalf("MaxTokens = %d, want 8192 (4x auxiliary headroom)", req.MaxTokens)
	}

	plain := (&WikiDreamer{}).llmRequest("system", "prompt", 2048)
	if plain.Thinking != nil {
		t.Fatalf("plain-model Thinking = %#v, want nil", plain.Thinking)
	}
}
