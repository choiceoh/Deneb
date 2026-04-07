package wiki

import (
	"os"
	"path/filepath"
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

	page, err := ParsePage([]byte(input))
	testutil.NoError(t, err)

	if page.Meta.ID != "dgx-spark" {
		t.Errorf("id = %q, want %q", page.Meta.ID, "dgx-spark")
	}
	if page.Meta.Title != "DGX Spark" {
		t.Errorf("title = %q, want %q", page.Meta.Title, "DGX Spark")
	}
	if page.Meta.Summary != "128GB 통합 메모리 로컬 AI 서버" {
		t.Errorf("summary = %q", page.Meta.Summary)
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
	page, err := ParsePage([]byte(input))
	testutil.NoError(t, err)
	if page.Meta.Title != "" {
		t.Errorf("title = %q, want empty", page.Meta.Title)
	}
	if page.Body != input {
		t.Errorf("body mismatch")
	}
}

func TestPage_RenderRoundtrip(t *testing.T) {
	page := NewPage("테스트", "기술", []string{"Go", "테스트"})
	page.Meta.ID = "test-page"
	page.Meta.Summary = "테스트용 페이지"
	page.Body = "# 테스트\n\n## 요약\n테스트 내용."

	rendered := page.Render()

	parsed, err := ParsePage(rendered)
	testutil.NoError(t, err)
	if parsed.Meta.ID != "test-page" {
		t.Errorf("id roundtrip: got %q", parsed.Meta.ID)
	}
	if parsed.Meta.Title != "테스트" {
		t.Errorf("title roundtrip: got %q", parsed.Meta.Title)
	}
	if parsed.Meta.Summary != "테스트용 페이지" {
		t.Errorf("summary roundtrip: got %q", parsed.Meta.Summary)
	}
	if parsed.Meta.Category != "기술" {
		t.Errorf("category roundtrip: got %q", parsed.Meta.Category)
	}
	if len(parsed.Meta.Tags) != 2 {
		t.Errorf("tags roundtrip: got %v", parsed.Meta.Tags)
	}
}

func TestPage_Section(t *testing.T) {
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

func TestPage_Sections(t *testing.T) {
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

	if err := WritePageFile(path, page); err != nil {
		t.Fatalf("WritePageFile: %v", err)
	}

	// Verify .tmp doesn't linger.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp file should not exist after write")
	}

	// Verify content.
	parsed, err := ParsePageFile(path)
	testutil.NoError(t, err)
	if parsed.Meta.Title != "원자적 쓰기" {
		t.Errorf("title = %q after write", parsed.Meta.Title)
	}
}
