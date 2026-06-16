package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDiagramRender_Sample is a manual visual check: set DENEB_DIAGRAM_RENDER=1 to
// emit sample PNGs into ~/.cache/deneb-visual for eyeballing. Skipped in CI.
func TestDiagramRender_Sample(t *testing.T) {
	if os.Getenv("DENEB_DIAGRAM_RENDER") == "" {
		t.Skip("set DENEB_DIAGRAM_RENDER=1 to render sample diagrams")
	}
	dir := weeklyOutputDir()
	cases := []struct {
		name string
		p    diagramParams
		def  func(diagramParams) string
		win  int
	}{
		{name: "flowchart-state", p: diagramParams{
			DiagramType: "flowchart", Title: "태양광 시공 진행 단계", Subtitle: "인허가 → 준공",
			Direction: "LR",
			Nodes: []diagramNode{
				{ID: "permit", Label: "인허가", Shape: "stadium"},
				{ID: "build", Label: "공사", Shape: "rect"},
				{ID: "inspect", Label: "검사", Shape: "diamond"},
				{ID: "done", Label: "준공", Shape: "round"},
			},
			Edges: []diagramEdge{
				{From: "permit", To: "build", Label: "승인"},
				{From: "build", To: "inspect"},
				{From: "inspect", To: "done", Label: "합격"},
				{From: "inspect", To: "build", Label: "보완"},
			},
		}},
		{name: "timeline-history", p: diagramParams{
			DiagramType: "timeline", Title: "탑솔라 연혁", Subtitle: "주요 이정표",
			Events: []timelineEvent{
				{Section: "창업기", Period: "2021", Items: []string{"법인 설립", "첫 시공 계약"}},
				{Section: "창업기", Period: "2022", Items: []string{"누적 1MW 돌파"}},
				{Section: "성장기", Period: "2023", Items: []string{"남도에코 설립", "케이블 사업 진출"}},
				{Section: "성장기", Period: "2024", Items: []string{"ERP 도입", "5MW 준공"}},
				{Section: "도약기", Period: "2025", Items: []string{"AI 비서 Deneb 가동"}},
			},
		}},
		{name: "gantt-project", p: diagramParams{
			DiagramType: "gantt", Title: "상반기 프로젝트 일정", Subtitle: "단위: 주",
			Tasks: []ganttTask{
				{Name: "부지 계약", Section: "준비", Start: "2026-01-05", Duration: "2w", Status: "done"},
				{Name: "인허가 접수", Section: "준비", Start: "2026-01-19", Duration: "3w", Status: "done"},
				{Name: "기초 공사", Section: "시공", Start: "2026-02-09", Duration: "4w", Status: "active"},
				{Name: "모듈 설치", Section: "시공", Start: "2026-03-09", Duration: "3w"},
				{Name: "준공 검사", Section: "마감", Start: "2026-03-30", Duration: "1w", Status: "crit"},
			},
		}},
	}
	for _, c := range cases {
		var def, win string
		switch c.p.DiagramType {
		case "gantt":
			def, win = compileGantt(c.p), "2200,1300"
		case "timeline":
			def, win = compileTimeline(c.p), "2400,1600"
		default:
			def, win = compileFlowchart(c.p), "1600,2200"
		}
		t.Logf("%s mermaid:\n%s", c.name, def)
		html := buildDiagramHTML(c.p, def)
		htmlPath := filepath.Join(dir, "sample-"+c.name+".html")
		pngPath := filepath.Join(dir, "sample-"+c.name+".png")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
			t.Fatalf("%s: write html: %v", c.name, err)
		}
		if err := renderHTMLToPNG(context.Background(), htmlPath, pngPath, win, 10000); err != nil {
			t.Fatalf("%s: render: %v", c.name, err)
		}
		if err := trimDarkPNG(pngPath); err != nil {
			t.Fatalf("%s: trim: %v", c.name, err)
		}
		t.Logf("%s -> %s", c.name, pngPath)
	}
}
