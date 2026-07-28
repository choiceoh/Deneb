package artifact

import "testing"

// The whole point of the adaptive canvas: a horizontal ranking chart with many
// categories grows instead of letting autoSkip drop names, and it stops growing
// at chartCanvasMaxH so the PNG stays deliverable.
func TestChartCanvasGrowsWithCategoriesAndClamps(t *testing.T) {
	small := chartParams{
		ChartType: "bar", Horizontal: true,
		Labels: seriesLabels("거래처", 4), Series: []chartSeries{{Data: descendingData(4, 10, 1)}},
	}
	many := chartParams{
		ChartType: "bar", Horizontal: true,
		Labels: seriesLabels("거래처", 24), Series: []chartSeries{{Data: descendingData(24, 10, 1)}},
	}
	huge := chartParams{
		ChartType: "bar", Horizontal: true,
		Labels: seriesLabels("거래처", 200), Series: []chartSeries{{Data: descendingData(200, 10, 1)}},
	}

	if _, h := chartCanvas(small); h != chartCanvasH {
		t.Errorf("a 4-category chart must keep the base height, got %d", h)
	}
	_, hMany := chartCanvas(many)
	if hMany <= chartCanvasH {
		t.Errorf("24 categories must grow past the base height, got %d", hMany)
	}
	if _, h := chartCanvas(huge); h != chartCanvasMaxH {
		t.Errorf("growth must clamp at %d, got %d", chartCanvasMaxH, h)
	}
}

// Stacked series share one row per category, so a stacked horizontal chart must
// not grow as if each series had its own bar.
func TestChartCanvasStackedSharesRows(t *testing.T) {
	base := chartParams{ChartType: "bar", Horizontal: true, Labels: seriesLabels("항목", 12)}
	base.Series = []chartSeries{
		{Data: descendingData(12, 10, 1)}, {Data: descendingData(12, 8, 1)}, {Data: descendingData(12, 6, 1)},
	}
	grouped := base
	stacked := base
	stacked.Stacked = true

	_, hGrouped := chartCanvas(grouped)
	_, hStacked := chartCanvas(stacked)
	if hStacked >= hGrouped {
		t.Errorf("stacked (%d) must need less height than grouped (%d)", hStacked, hGrouped)
	}
}

// Category ticks: short labels stay flat, medium ones rotate 45°, long Korean
// names go upright — and every case turns autoSkip OFF so nothing is dropped.
func TestCategoryTicksRotateInsteadOfSkipping(t *testing.T) {
	cases := []struct {
		name     string
		labels   []string
		rotation any
	}{
		{"short flat", []string{"1월", "2월", "3월", "4월"}, 0},
		{"medium rotates 45", seriesLabels("2026-06-", 14), 45},
		{"long goes upright", seriesLabels("남도에코건설기술", 26), 90},
	}
	for _, c := range cases {
		p := chartParams{ChartType: "bar", Labels: c.labels}
		ticks := categoryTicks(p, chartCanvasW)
		if ticks == nil {
			t.Fatalf("%s: expected tick overrides, got nil", c.name)
		}
		if ticks["autoSkip"] != false {
			t.Errorf("%s: autoSkip must be off so no label is dropped, got %v", c.name, ticks["autoSkip"])
		}
		if ticks["maxRotation"] != c.rotation {
			t.Errorf("%s: want rotation %v, got %v", c.name, c.rotation, ticks["maxRotation"])
		}
	}
}

// Past the cap, per-label rendering is not honest at chart canvas size — hand the
// axis back to Chart.js autoSkip rather than promise labels we cannot draw.
func TestCategoryTicksYieldsToAutoSkipPastCap(t *testing.T) {
	p := chartParams{ChartType: "bar", Labels: seriesLabels("d", chartTickLabelCap+1)}
	if ticks := categoryTicks(p, chartCanvasW); ticks != nil {
		t.Errorf("past %d categories the axis must fall back to autoSkip, got %v", chartTickLabelCap, ticks)
	}
}

// A horizontal chart gives every category its own row, so labels never rotate
// and never shrink — the grown canvas already bought the space. This holds right
// up to the cap, where the canvas is clamped and rows are tightest.
func TestCategoryTicksHorizontalUsesRowsNotRotation(t *testing.T) {
	for _, n := range []int{8, chartTickLabelCap} {
		p := chartParams{
			ChartType: "bar", Horizontal: true, Labels: seriesLabels("거래처", n),
			Series: []chartSeries{{Data: descendingData(n, 10, 1)}},
		}
		w, _ := chartCanvas(p)
		ticks := categoryTicks(p, w)
		if ticks["autoSkip"] != false {
			t.Errorf("%d categories: horizontal must keep every row, got %v", n, ticks)
		}
		if _, rotated := ticks["maxRotation"]; rotated {
			t.Errorf("%d categories: horizontal labels must not rotate, got %v", n, ticks)
		}
		if _, dense := ticks["font"]; dense {
			t.Errorf("%d categories: a row-per-category axis keeps the normal font, got %v", n, ticks)
		}
	}

	// The invariant that keeps it true: even clamped, a row clears the font.
	tight := chartParams{
		ChartType: "bar", Horizontal: true,
		Labels: seriesLabels("거래처", chartTickLabelCap),
		Series: []chartSeries{{Data: descendingData(chartTickLabelCap, 10, 1)}},
	}
	_, h := chartCanvas(tight)
	if rowPx := float64(h-chartPlotHeadroom) / float64(chartTickLabelCap); rowPx < chartTickFontPx*1.5 {
		t.Errorf("a clamped %d-row chart leaves only %.1fpx per label — too tight for %vpx text",
			chartTickLabelCap, rowPx, chartTickFontPx)
	}
}

// The doughnut legend is one entry per slice: many slices on a clamped canvas
// must pack tighter, few slices must keep the roomy default.
func TestDoughnutLegendPacksWhenClamped(t *testing.T) {
	if got := doughnutLegendLabels(4, chartCanvasH)["padding"]; got != 14 {
		t.Errorf("4 slices must keep the roomy legend padding, got %v", got)
	}
	if got := doughnutLegendLabels(40, chartCanvasMaxH)["padding"]; got != 8 {
		t.Errorf("40 slices must pack the legend, got %v", got)
	}
}

// Hangul is full-width, ASCII is not — the fitting heuristic must see that or a
// Korean axis silently overflows.
func TestLabelWidthPxCountsHangulWider(t *testing.T) {
	hangul := labelWidthPx("남도에코", chartTickFontPx)
	ascii := labelWidthPx("abcd", chartTickFontPx)
	if hangul <= ascii {
		t.Errorf("4 Hangul glyphs (%.1f) must measure wider than 4 ASCII (%.1f)", hangul, ascii)
	}
}
