package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestChartRender_Sample is a manual visual check: set DENEB_CHART_RENDER=1 to
// emit sample PNGs into ~/.cache/deneb-visual for eyeballing. Skipped in CI.
func TestChartRender_Sample(t *testing.T) {
	if os.Getenv("DENEB_CHART_RENDER") == "" {
		t.Skip("set DENEB_CHART_RENDER=1 to render sample charts")
	}
	dir := weeklyOutputDir()
	cases := []struct {
		name string
		p    chartParams
	}{
		{"bar-line-combo", chartParams{
			ChartType: "bar", Title: "월별 발주 · 견적 추이", Subtitle: "2026년 상반기 · 단위 건",
			Labels: []string{"1월", "2월", "3월", "4월", "5월", "6월"},
			YUnit:  "건",
			Series: []chartSeries{
				{Name: "발주", Data: []float64{12, 19, 15, 24, 22, 30}},
				{Name: "견적", Data: []float64{20, 25, 23, 31, 28, 36}, Type: "line"},
			},
		}},
		{"line-area", chartParams{
			ChartType: "area", Title: "누적 매출", Subtitle: "단위: 백만원",
			Labels: []string{"1월", "2월", "3월", "4월", "5월", "6월"},
			Series: []chartSeries{{Name: "매출", Data: []float64{120, 240, 360, 510, 680, 900}}},
		}},
		{"doughnut", chartParams{
			ChartType: "doughnut", Title: "프로젝트 단계별 구성비",
			Labels: []string{"인허가", "공사", "검사", "준공"},
			Series: []chartSeries{{Name: "비율", Data: []float64{8, 14, 5, 11}}},
		}},
	}
	for _, c := range cases {
		html, err := buildChartHTML(c.p)
		if err != nil {
			t.Fatalf("%s: build html: %v", c.name, err)
		}
		htmlPath := filepath.Join(dir, "sample-"+c.name+".html")
		pngPath := filepath.Join(dir, "sample-"+c.name+".png")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
			t.Fatalf("%s: write html: %v", c.name, err)
		}
		if err := chartRenderImage(context.Background(), htmlPath, pngPath); err != nil {
			t.Fatalf("%s: render: %v", c.name, err)
		}
		t.Logf("%s -> %s", c.name, pngPath)
	}
}
