package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestParsePage_WithFrontmatter(t *testing.T) {
	input := `---
id: dgx-spark
title: DGX Spark
summary: 128GB 통합 메모리 로컬 AI 서버
category: 기술
tags: [하드웨어, NVIDIA]
related: [deneb]
resource: file:///home/choiceoh/.deneb/specs/dgx-spark.md
created: 2025-11-15
updated: 2026-04-06
importance: 0.9
---

# DGX Spark

## 요약
NVIDIA DGX Spark.

## 핵심 사실
- fact 1
- fact 2
`

	page := testutil.Must(parsePage([]byte(input)))

	if page.Meta.ID != "dgx-spark" {
		t.Errorf("id = %q, want %q", page.Meta.ID, "dgx-spark")
	}
	if page.Meta.Title != "DGX Spark" {
		t.Errorf("title = %q, want %q", page.Meta.Title, "DGX Spark")
	}
	if page.Meta.Summary != "128GB 통합 메모리 로컬 AI 서버" {
		t.Errorf("summary = %q", page.Meta.Summary)
	}
	if page.Meta.Resource != "file:///home/choiceoh/.deneb/specs/dgx-spark.md" {
		t.Errorf("resource = %q", page.Meta.Resource)
	}
	if page.Meta.Category != "기술" {
		t.Errorf("category = %q, want %q", page.Meta.Category, "기술")
	}
	if len(page.Meta.Tags) != 2 || page.Meta.Tags[0] != "하드웨어" {
		t.Errorf("tags = %v, want [하드웨어, NVIDIA]", page.Meta.Tags)
	}
	if page.Meta.Importance != 0.9 {
		t.Errorf("importance = %f, want 0.9", page.Meta.Importance)
	}
	if page.Meta.Created != "2025-11-15" {
		t.Errorf("created = %q, want 2025-11-15", page.Meta.Created)
	}
}

func TestParsePage_NoFrontmatter(t *testing.T) {
	input := "# Just markdown\n\nSome content."
	page := testutil.Must(parsePage([]byte(input)))
	if page.Meta.Title != "" {
		t.Errorf("title = %q, want empty", page.Meta.Title)
	}
	if page.Body != input {
		t.Errorf("body mismatch")
	}
}

func TestStripLeadingFrontmatter_StripsLeadingBlocksPreservesHorizontalRulesAndProse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "one leading block",
			in:   "---\ntitle: Foo\ncategory: 프로젝트\n---\n\n# Foo\n\nbody",
			want: "# Foo\n\nbody",
		},
		{
			name: "stacked blocks both stripped",
			in:   "---\ntitle: Foo\n---\n\n---\ntitle: Foo\ntags: [a, b]\n---\n\n# Foo",
			want: "# Foo",
		},
		{
			name: "leading newlines before block",
			in:   "\n\n---\nid: foo\n---\n\nbody text",
			want: "body text",
		},
		{
			name: "plain markdown unchanged",
			in:   "# Heading\n\nsome text",
			want: "# Heading\n\nsome text",
		},
		{
			name: "horizontal rule not stripped",
			in:   "intro\n\n---\n\n## (병합: Other)\n\nmore",
			want: "intro\n\n---\n\n## (병합: Other)\n\nmore",
		},
		{
			name: "non-key fenced prose not stripped",
			in:   "---\nthis is just a quote\n---\nrest",
			want: "---\nthis is just a quote\n---\nrest",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripLeadingFrontmatter(tc.in); got != tc.want {
				t.Errorf("stripLeadingFrontmatter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPage_RenderRoundtrip(t *testing.T) {
	page := NewPage("테스트", "기술", []string{"Go", "테스트"})
	page.Meta.ID = "test-page"
	page.Meta.Summary = "테스트용 페이지"
	page.Meta.Resource = "gmail:thread/abc123"
	page.Meta.PID = "p-pl2-001"
	page.Meta.Capacity = 12.5
	page.Meta.Stage = "계약협의"
	page.Body = "# 테스트\n\n## 요약\n테스트 내용."

	rendered := page.Render()

	parsed := testutil.Must(parsePage(rendered))
	if parsed.Meta.Capacity != 12.5 {
		t.Errorf("capacity roundtrip: got %v, want 12.5", parsed.Meta.Capacity)
	}
	if parsed.Meta.Stage != "계약협의" {
		t.Errorf("stage roundtrip: got %q, want 계약협의", parsed.Meta.Stage)
	}
	if parsed.Meta.ID != "test-page" {
		t.Errorf("id roundtrip: got %q", parsed.Meta.ID)
	}
	if parsed.Meta.PID != "p-pl2-001" {
		t.Errorf("pid roundtrip: got %q", parsed.Meta.PID)
	}
	if parsed.Meta.Title != "테스트" {
		t.Errorf("title roundtrip: got %q", parsed.Meta.Title)
	}
	if parsed.Meta.Summary != "테스트용 페이지" {
		t.Errorf("summary roundtrip: got %q", parsed.Meta.Summary)
	}
	if parsed.Meta.Resource != "gmail:thread/abc123" {
		t.Errorf("resource roundtrip: got %q", parsed.Meta.Resource)
	}
	if parsed.Meta.Category != "기술" {
		t.Errorf("category roundtrip: got %q", parsed.Meta.Category)
	}
	if len(parsed.Meta.Tags) != 2 {
		t.Errorf("tags roundtrip: got %v", parsed.Meta.Tags)
	}
}

// TestFrontmatter_ClientRoundtripAndNormalize: the 거래처 field survives a
// render→parse cycle, and normalizeClientName strips wikilink wrappers and
// collapses whitespace on both ends of the cycle.
func TestFrontmatter_ClientRoundtripAndNormalize(t *testing.T) {
	page := NewPage("금호타이어 곡성 1단계", "프로젝트", nil)
	page.Meta.Client = "금호타이어"
	page.Body = "# 본문"

	parsed := testutil.Must(parsePage(page.Render()))
	if parsed.Meta.Client != "금호타이어" {
		t.Errorf("client roundtrip: got %q", parsed.Meta.Client)
	}

	for in, want := range map[string]string{
		"[[금호타이어]]":  "금호타이어",
		"  현대차  ":    "현대차",
		"LG  전자":     "LG 전자",
		"":           "",
		"   [[ ]]  ": "",
	} {
		if got := normalizeClientName(in); got != want {
			t.Errorf("normalizeClientName(%q) = %q, want %q", in, got, want)
		}
	}

	// Empty client renders no line at all.
	page.Meta.Client = "  "
	if strings.Contains(string(page.Render()), "client:") {
		t.Error("blank client must not render a client: line")
	}
}

func TestPage_Section_ReturnsHeadingBodyOrEmptyForMissing(t *testing.T) {
	page := &Page{
		Body: `# Title

## 요약
This is the summary.

## 핵심 사실
- fact one
- fact two

## 백링크
- [[foo]]
`,
	}

	summary := page.Section("요약")
	if summary != "This is the summary." {
		t.Errorf("Section(요약) = %q", summary)
	}

	facts := page.Section("핵심 사실")
	if facts != "- fact one\n- fact two" {
		t.Errorf("Section(핵심 사실) = %q", facts)
	}

	missing := page.Section("없는 섹션")
	if missing != "" {
		t.Errorf("Section(없는 섹션) = %q, want empty", missing)
	}
}

func TestPage_Sections_ReturnsOrderedHeadingList(t *testing.T) {
	page := &Page{
		Body: "# Title\n\n## Alpha\nA\n\n## Beta\nB\n\n## Gamma\nC\n",
	}

	headings := page.Sections()
	want := []string{"Alpha", "Beta", "Gamma"}
	if len(headings) != len(want) {
		t.Fatalf("Sections = %v, want %v", headings, want)
	}
	for i, h := range headings {
		if h != want[i] {
			t.Errorf("Sections[%d] = %q, want %q", i, h, want[i])
		}
	}
}

func TestWritePageFile_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	page := NewPage("원자적 쓰기", "기술", nil)
	page.Body = "# 원자적 쓰기\n\nContent."

	if err := writePageFile(path, page); err != nil {
		t.Fatalf("writePageFile: %v", err)
	}

	// Verify .tmp doesn't linger.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after write")
	}

	// Verify content.
	parsed := testutil.Must(ParsePageFile(path))
	if parsed.Meta.Title != "원자적 쓰기" {
		t.Errorf("title = %q after write", parsed.Meta.Title)
	}
}

// TestWritePageFile_RedactsBody ensures secret patterns in a wiki page body
// are masked at the write boundary. Even if the Dreamer synthesizes a page
// that quotes a secret, it must never reach disk unmasked.
func TestWritePageFile_RedactsBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "redact-body.md")

	token := "sk-proj-" + strings.Repeat("Z", 40) // synthetic, not a real credential
	page := NewPage("세션 기록", "기술", []string{"NVIDIA"})
	page.Body = "# 세션 기록\n\n## 핵심 사실\n\n- 토큰: " + token + " 를 확인했음"

	if err := writePageFile(path, page); err != nil {
		t.Fatalf("writePageFile: %v", err)
	}

	data := testutil.Must(os.ReadFile(path))
	if strings.Contains(string(data), token) {
		t.Fatalf("page body still contains raw token: %q", string(data))
	}
	// Korean surface text and structural frontmatter must survive intact.
	if !strings.Contains(string(data), "핵심 사실") {
		t.Errorf("Korean heading was altered: %q", string(data))
	}
	if !strings.Contains(string(data), "category: 기술") {
		t.Errorf("category frontmatter was altered: %q", string(data))
	}
	if !strings.Contains(string(data), "title: 세션 기록") {
		t.Errorf("Korean title was altered: %q", string(data))
	}
}

// TestWritePageFile_RedactsSummary verifies the LLM-generated Summary field
// is scrubbed along with the body.
func TestWritePageFile_RedactsSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "redact-summary.md")

	token := "ghp_" + strings.Repeat("Z", 36) // synthetic
	page := NewPage("정리", "기술", nil)
	page.Meta.Summary = "사용자 GitHub 토큰 " + token + " 관련 변경"
	page.Body = "# 정리"

	if err := writePageFile(path, page); err != nil {
		t.Fatalf("writePageFile: %v", err)
	}

	data := testutil.Must(os.ReadFile(path))
	if strings.Contains(string(data), token) {
		t.Fatalf("summary still contains raw token: %q", string(data))
	}
}

func TestNormalizeStage_DropsOutOfVocabulary(t *testing.T) {
	for in, want := range map[string]string{
		"계약협의": "계약협의", " 시공 ": "시공", "유실": "유실",
		"협상중": "", "done": "", "": "",
	} {
		if got := normalizeStage(in); got != want {
			t.Errorf("normalizeStage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPageDueDoneRoundTrip(t *testing.T) {
	p := &Page{Meta: Frontmatter{Title: "대한전선", Due: "2026-07-20", DueDone: "2026-07-20"}, Body: "본문"}
	rendered := string(p.Render())
	if !strings.Contains(rendered, "due_done: 2026-07-20") {
		t.Fatalf("due_done not rendered:\n%s", rendered)
	}
	parsed, err := parsePage([]byte(rendered))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Meta.DueDone != "2026-07-20" || parsed.Meta.Due != "2026-07-20" {
		t.Fatalf("round-trip lost fields: due=%q due_done=%q", parsed.Meta.Due, parsed.Meta.DueDone)
	}
}
