package artifact

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
)

// TestChartRender_Sample is a manual visual check: set DENEB_CHART_RENDER=1 to
// emit sample PNGs into ~/.cache/deneb-visual for eyeballing. Skipped in CI.
func TestChartRender_Sample(t *testing.T) {
	if os.Getenv("DENEB_CHART_RENDER") == "" {
		t.Skip("set DENEB_CHART_RENDER=1 to render sample charts")
	}
	dir := routine.VisualOutputDir()
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
			ChartType: "doughnut", Title: "프로젝트 단계별 구성비", YUnit: "건",
			Labels: []string{"인허가", "공사", "검사", "준공"},
			Series: []chartSeries{{Name: "비율", Data: []float64{8, 14, 5, 11}}},
		}},
		{"stacked-bar", chartParams{
			ChartType: "bar", Title: "월별 매출 구성 (누적)", YUnit: "백만원", Stacked: true,
			Labels: []string{"1월", "2월", "3월", "4월"},
			Series: []chartSeries{
				{Name: "제품A", Data: []float64{40, 55, 48, 70}},
				{Name: "제품B", Data: []float64{25, 30, 42, 38}},
				{Name: "제품C", Data: []float64{12, 18, 15, 22}},
			},
		}},
		{"horizontal-bar", chartParams{
			ChartType: "bar", Title: "거래처별 발주액 순위", YUnit: "만원", Horizontal: true,
			Labels: []string{"현대차", "탑솔라", "남도에코", "한빛전력"},
			Series: []chartSeries{{Name: "발주액", Data: []float64{4200, 3100, 1800, 950}}},
		}},
		// High-cardinality cases: these are what the fixed canvas used to drop
		// labels on. Eyeball that every 거래처 / 일자 / 공정 name is present.
		{"horizontal-bar-24", chartParams{
			ChartType: "bar", Title: "거래처별 발주액 순위 (24개사)", ValueKind: "amount", YUnit: "만원",
			Horizontal: true,
			Labels:     seriesLabels("거래처", 24),
			Series:     []chartSeries{{Name: "발주액", Data: descendingData(24, 4800, 170)}},
		}},
		{"bar-26-rotated", chartParams{
			ChartType: "bar", Title: "일자별 발주 건수", Subtitle: "라벨이 눕는지 확인",
			ValueKind: "count",
			Labels:    seriesLabels("2026-06-", 26),
			Series:    []chartSeries{{Name: "발주", Data: descendingData(26, 30, 1)}},
		}},
		{"doughnut-14", chartParams{
			ChartType: "doughnut", Title: "공정별 구성비 (14개)", ValueKind: "count",
			Labels: seriesLabels("공정", 14),
			Series: []chartSeries{{Name: "비율", Data: descendingData(14, 40, 2)}},
		}},
		{"temperature", chartParams{
			ChartType: "line", Title: "일평균 기온", Subtitle: "0부터 그리지 않는다",
			ValueKind: "temperature",
			Labels:    []string{"월", "화", "수", "목", "금", "토", "일"},
			Series:    []chartSeries{{Name: "기온", Data: []float64{24.1, 25.3, 26.0, 25.4, 23.8, 22.9, 24.6}}},
		}},
		// No unit and no kind: Chart.js must keep deriving tick precision from the
		// tick spacing. If the ticks all read "0" the override leaked back in.
		{"tiny-values-no-hint", chartParams{
			ChartType: "line", Title: "미세 변화", Subtitle: "눈금이 0으로 뭉개지면 안 된다",
			Labels: []string{"a", "b", "c", "d"},
			Series: []chartSeries{{Name: "값", Data: []float64{0.0001, 0.0003, 0.0002, 0.0005}}},
		}},
		// Same tiny values WITH a unit: the suffix is appended but the adaptive
		// precision must survive.
		{"tiny-values-with-unit", chartParams{
			ChartType: "line", Title: "미세 변화 (단위 있음)", YUnit: "mm",
			Labels: []string{"a", "b", "c", "d"},
			Series: []chartSeries{{Name: "값", Data: []float64{0.0001, 0.0003, 0.0002, 0.0005}}},
		}},
	}
	for _, c := range cases {
		w, h := chartCanvas(c.p)
		html, err := buildChartHTML(c.p, w, h)
		if err != nil {
			t.Fatalf("%s: build html: %v", c.name, err)
		}
		htmlPath := filepath.Join(dir, "sample-"+c.name+".html")
		pngPath := filepath.Join(dir, "sample-"+c.name+".png")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil {
			t.Fatalf("%s: write html: %v", c.name, err)
		}
		if err := chartRenderImage(context.Background(), htmlPath, pngPath, w, h); err != nil {
			t.Fatalf("%s: render: %v", c.name, err)
		}
		t.Logf("%s (%dx%d) -> %s", c.name, w, h, pngPath)
	}
}

func seriesLabels(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return out
}

func descendingData(n int, top, step float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = top - float64(i)*step
	}
	return out
}

// Stacked/horizontal flow into the Chart.js config: stacked marks both scales,
// horizontal flips indexAxis and moves the value axis (beginAtZero) to x.
func TestChartConfigRendersStackedAndHorizontalAxes(t *testing.T) {
	p := chartParams{
		ChartType: "bar",
		Labels:    []string{"a", "b"},
		Series: []chartSeries{
			{Name: "s1", Data: []float64{1, 2}},
			{Name: "s2", Data: []float64{3, 4}},
		},
		Stacked:    true,
		Horizontal: true,
	}
	w, h := chartCanvas(p)
	cfg, err := chartConfig(p, w, h)
	if err != nil {
		t.Fatalf("chartConfig: %v", err)
	}
	opts := cfg["options"].(map[string]any)
	if opts["indexAxis"] != "y" {
		t.Errorf("horizontal must set indexAxis=y, got %v", opts["indexAxis"])
	}
	scales := opts["scales"].(map[string]any)
	x := scales["x"].(map[string]any)
	y := scales["y"].(map[string]any)
	if x["stacked"] != true || y["stacked"] != true {
		t.Errorf("stacked must mark both scales: x=%v y=%v", x["stacked"], y["stacked"])
	}
	// Horizontal: value axis (beginAtZero) is x, category axis is y.
	if x["beginAtZero"] != true {
		t.Errorf("horizontal must put the value axis on x: %v", x)
	}
	if _, ok := y["beginAtZero"]; ok {
		t.Errorf("category axis must not carry beginAtZero: %v", y)
	}
}

// The HTML page carries the JS runtime that labels doughnut segments and
// suffixes value ticks with y_unit (JSON config can't hold callbacks).
func TestBuildChartHTMLRendersRuntimeWiring(t *testing.T) {
	p := chartParams{
		ChartType: "doughnut",
		Labels:    []string{"인허가", "공사"},
		YUnit:     "건",
		Series:    []chartSeries{{Data: []float64{3, 7}}},
	}
	w, h := chartCanvas(p)
	html, err := buildChartHTML(p, w, h)
	if err != nil {
		t.Fatalf("buildChartHTML: %v", err)
	}
	for _, want := range []string{`const Y_UNIT = "건"`, "segLabels", "ticks.callback"} {
		if !strings.Contains(html, want) {
			t.Errorf("chart html missing %q", want)
		}
	}
}

// send=true without a connected channel degrades to the send_file instruction
// (the render succeeded — never fail the call at delivery stage).
func TestFinishRenderedImage_SendWithoutChannel(t *testing.T) {
	out := finishRenderedImage(context.Background(), "/tmp/x.png", "차트", true, "", "제목")
	if !strings.Contains(out, "자동 전송 실패") || !strings.Contains(out, "send_file") {
		t.Errorf("expected graceful degradation to send_file instruction, got: %q", out)
	}
	out = finishRenderedImage(context.Background(), "/tmp/x.png", "차트", false, "", "")
	if !strings.Contains(out, "send_file") || strings.Contains(out, "자동 전송 실패") {
		t.Errorf("send=false must keep the classic instruction, got: %q", out)
	}
}
