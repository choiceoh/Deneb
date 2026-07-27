// chart_layout.go — cardinality-adaptive layout for the chart tool.
//
// Why this exists: chart.go renders into a FIXED canvas, so with many categories
// the Chart.js default (ticks.autoSkip) silently DROPS category labels — a
// 20-거래처 ranking bar chart comes out with a third of the names missing and
// nothing in the image hints that anything was hidden. Rather than adopt a
// layout engine (Flint et al.), we do the two adaptations that actually matter
// for the four chart types Deneb draws:
//
//  1. the canvas grows along the category axis when categories stack vertically
//     — horizontal bars (one row per bar) and doughnut legends (one entry per
//     slice) — so every row has room instead of being skipped;
//  2. category tick labels keep every entry (autoSkip off) and rotate 45° then
//     90° with a smaller font when they cannot fit flat.
//
// Past chartTickLabelCap categories neither helps: a PNG in a chat bubble stops
// being readable long before that, so the axis goes back to autoSkip and the
// thinning is Chart.js's problem again.
package artifact

const (
	// Grown-canvas ceiling in CSS pixels (chartCanvasH is the base and the
	// floor). The render is 2x device scale, so 1120 CSS px is already a
	// 2240 px tall PNG — past that a chat bubble scales it into illegibility.
	chartCanvasMaxH = 1120

	// Vertical budget outside the plot area: card padding, title block, legend
	// row. Charts without a title waste a little of it — cheaper than making the
	// canvas depend on the title text.
	chartPlotHeadroom = 150

	// Horizontal-bar row geometry: one bar per series per category, plus a gap
	// between category groups.
	chartBarRowH     = 24
	chartBarGroupGap = 12

	// Doughnut legend geometry: entries stack on the right, one per slice.
	chartLegendHeadroom = 120
	chartLegendRowH     = 32

	// Category-axis label fitting. chartPlotGutterPx is the width the plot area
	// loses to card padding plus the value-axis ticks, so the category axis has
	// (canvasW - gutter) to spread labels across. chartRot45Gain is how much
	// wider than its slot a label may be before 45° stops helping — a rotated
	// label needs only ~1/√2 of its length in slot width and neighbours
	// interleave.
	chartPlotGutterPx    = 116.0
	chartTickGapPx       = 8.0
	chartRot45Gain       = 2.6
	chartTickFontPx      = 13.0
	chartTickFontDensePx = 11.0

	// Past this many categories, honest per-label rendering is impossible at
	// chart canvas size — hand the axis back to Chart.js autoSkip.
	chartTickLabelCap = 40
)

// chartCanvas returns the CSS-pixel canvas for this chart. Width is fixed (a
// chat bubble is width-bound); height grows with the category count whenever the
// categories are laid out vertically.
func chartCanvas(p chartParams) (int, int) {
	n := len(p.Labels)
	switch {
	case p.ChartType == "doughnut":
		return chartCanvasW, clampCanvasH(chartLegendHeadroom + n*chartLegendRowH)
	case p.Horizontal && p.ChartType == "bar":
		bars := n * max(1, len(p.Series))
		if p.Stacked {
			bars = n // stacked series share one row per category
		}
		return chartCanvasW, clampCanvasH(chartPlotHeadroom + bars*chartBarRowH + n*chartBarGroupGap)
	}
	return chartCanvasW, chartCanvasH
}

func clampCanvasH(h int) int {
	return min(max(h, chartCanvasH), chartCanvasMaxH)
}

// categoryTicks returns the ticks overrides for the category axis, or nil to
// leave the Chart.js defaults (autoSkip on) in place.
//
// A horizontal chart needs nothing but autoSkip off: chartCanvas already gave
// each category its own row, and even at chartCanvasMaxH with chartTickLabelCap
// categories a row is ~24px — roomy enough for the normal tick font.
func categoryTicks(p chartParams, canvasW int) map[string]any {
	n := len(p.Labels)
	if n == 0 || n > chartTickLabelCap {
		return nil
	}
	if p.Horizontal {
		return map[string]any{"autoSkip": false}
	}

	widest := 0.0
	for _, l := range p.Labels {
		if w := labelWidthPx(l, chartTickFontPx); w > widest {
			widest = w
		}
	}
	slot := (float64(canvasW)-chartPlotGutterPx)/float64(n) - chartTickGapPx
	switch {
	case slot <= 0:
		return nil
	case widest <= slot:
		return map[string]any{"autoSkip": false, "maxRotation": 0, "minRotation": 0}
	case widest <= slot*chartRot45Gain:
		return map[string]any{"autoSkip": false, "maxRotation": 45, "minRotation": 45}
	default:
		// Upright labels need only one glyph width per slot; the smaller font
		// buys back some of the vertical space they take from the plot area.
		return map[string]any{
			"autoSkip": false, "maxRotation": 90, "minRotation": 90,
			"font": denseTickFont(),
		}
	}
}

// doughnutLegendLabels tunes the right-hand legend for the slice count: once the
// canvas is clamped, entries have to pack tighter than chartLegendRowH to all
// fit, so padding, swatch and font shrink together.
func doughnutLegendLabels(n, canvasH int) map[string]any {
	labels := map[string]any{
		"color": chartFg, "font": legendFont(),
		"padding": 14, "boxWidth": 14, "boxHeight": 14, "usePointStyle": true,
	}
	if n == 0 || float64(canvasH-chartLegendHeadroom)/float64(n) >= chartLegendRowH {
		return labels
	}
	labels["font"] = denseTickFont()
	labels["padding"] = 8
	labels["boxWidth"] = 10
	labels["boxHeight"] = 10
	return labels
}

// labelWidthPx approximates a rendered tick label's width: Hangul/CJK glyphs are
// about one em wide, Latin and digits about 0.55em. Rough is fine — it only has
// to pick between flat, 45° and 90°.
func labelWidthPx(s string, fontPx float64) float64 {
	w := 0.0
	for _, r := range s {
		if r >= 0x2E80 { // CJK radicals and up: full-width
			w += fontPx
		} else {
			w += fontPx * 0.55
		}
	}
	return w
}

func denseTickFont() map[string]any {
	return map[string]any{"family": "'Noto Sans CJK KR'", "size": chartTickFontDensePx}
}
