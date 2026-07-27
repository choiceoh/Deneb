// chart.go — render a data chart (line/bar/area/doughnut) as a PNG the agent can
// deliver with send_file.
//
// Why this exists: the agent often has structured numbers (monthly 발주 추이,
// 프로젝트 단계별 구성비, 거래처별 비교) that read far better as a chart than as a
// markdown table. There is no image-generation model in the loop on purpose — a
// data chart needs exact axes and values, which generative models hallucinate.
// Instead we render Chart.js into a fixed-size Deneb-dark canvas with the headless
// Chromium that already ships for the weekly report, screenshot it to a PNG, and
// hand the path back. The agent then calls send_file to put it in the chat.
//
// Reused routine-render plumbing: routine.VisualOutputDir keeps artifacts on real
// disk and routine.VisualRenderReady applies the shared host headroom guard.
// Unlike the weekly report we render
// at a FIXED size and DO NOT trim whitespace — the Deneb theme is dark, so the
// near-white trim in weeklyTrimPNG would crop the whole image.
package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// chartJS is the Chart.js v4 UMD bundle, embedded so the render never depends on
// network access (the headless Chromium runs offline). ~200 KB.
//
//go:embed chartassets/chart.umd.min.js
var chartJS string

// Base chart canvas geometry in CSS pixels. Rendered at 2x device scale (see
// chartRenderImage), so the PNG is 1800x1120 — crisp when the native client
// downscales it into a chat bubble. The height is a base, not a constant: charts
// whose categories stack vertically grow it (chartCanvas in chart_layout.go).
// Either way the size is known before the render, so no whitespace trim is needed.
const (
	chartCanvasW = 900
	chartCanvasH = 560
)

// Deneb dark theme tokens for the chart card.
const (
	chartBg       = "#0A0A0B" // AMOLED card background
	chartFg       = "#E8E8EA" // primary text
	chartMuted    = "#8A8A92" // axis ticks / subtitle
	chartGrid     = "rgba(255,255,255,0.07)"
	chartGridZero = "rgba(255,255,255,0.16)"
)

// chartPalette cycles per series / per doughnut segment. Teal, blue, amber,
// coral, purple — distinct in hue and (roughly) in lightness so the chart stays
// legible for colour-vision deficiency; line charts add a dash cue on top.
var chartPalette = []string{
	"#1D9E75", // teal
	"#378ADD", // blue
	"#EF9F27", // amber
	"#D85A30", // coral
	"#7F77DD", // purple
	"#3FB5B0", // cyan
	"#C45C9C", // magenta
}

// chartSeries is one line/bar/segment group.
type chartSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
	// Type optionally overrides the chart type for this series (e.g. a "line"
	// overlaid on "bar" columns for a combo chart). Empty = inherit chart_type.
	Type string `json:"type"`
}

type chartParams struct {
	ChartType string        `json:"chart_type"` // line | bar | area | doughnut
	Title     string        `json:"title"`
	Subtitle  string        `json:"subtitle"`
	Labels    []string      `json:"labels"`
	Series    []chartSeries `json:"series"`
	YUnit     string        `json:"y_unit"` // e.g. "건", "만원", "%"
	// ValueKind is a semantic hint about what the numbers ARE (amount | count |
	// ratio | temperature | score). It fills in y_unit and decides the tick
	// format and whether the axis may start above zero — see chart_valuekind.go.
	ValueKind string `json:"value_kind"`
	// Stacked stacks bar/area series (구성비 추이). Horizontal flips a bar
	// chart to horizontal orientation (항목별 순위).
	Stacked    bool `json:"stacked"`
	Horizontal bool `json:"horizontal"`
	// Send delivers the rendered PNG to the user in the same call (photo,
	// with Caption), removing the separate send_file round-trip.
	Send    bool   `json:"send"`
	Caption string `json:"caption"`
}

// ToolChart renders a chart to a PNG and returns its path for send_file.
func ToolChart() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p chartParams
		if err := jsonutil.UnmarshalInto("chart params", input, &p); err != nil {
			return "", err
		}
		p.ChartType = strings.ToLower(strings.TrimSpace(p.ChartType))
		if p.ChartType == "" {
			p.ChartType = "bar"
		}
		if len(p.Series) == 0 {
			return "", fmt.Errorf("series is required (at least one data series)")
		}
		hasData := false
		for _, s := range p.Series {
			if len(s.Data) > 0 {
				hasData = true
				break
			}
		}
		if !hasData {
			return "", fmt.Errorf("series carry no data points")
		}

		dir := routine.VisualOutputDir()
		if !routine.VisualRenderReady(dir) {
			return "", fmt.Errorf("chart render skipped: insufficient memory/disk headroom on host")
		}

		canvasW, canvasH := chartCanvas(p)
		html, err := buildChartHTML(p, canvasW, canvasH)
		if err != nil {
			return "", err
		}

		stamp := time.Now().Format("20060102-150405.000")
		htmlPath := filepath.Join(dir, "deneb-chart-"+stamp+".html")
		pngPath := filepath.Join(dir, "deneb-chart-"+stamp+".png")
		if err := os.WriteFile(htmlPath, []byte(html), 0o644); err != nil { //nolint:gosec // public chart markup, not secret
			return "", fmt.Errorf("write chart html: %w", err)
		}
		defer os.Remove(htmlPath) //nolint:errcheck // best-effort cleanup of the intermediate

		if err := chartRenderImage(ctx, htmlPath, pngPath, canvasW, canvasH); err != nil {
			return "", fmt.Errorf("chart render failed: %w", err)
		}

		return finishRenderedImage(ctx, pngPath, "차트", p.Send, p.Caption, p.Title), nil
	}
}

// buildChartHTML assembles the self-contained dark-themed Chart.js page at the
// given canvas size (chartCanvas picks it from the chart's cardinality).
func buildChartHTML(p chartParams, canvasW, canvasH int) (string, error) {
	// Resolve the unit once, here, so the value-axis ticks and the doughnut
	// legend both see the kind-derived default.
	p.YUnit = resolveYUnit(p)
	cfg, err := chartConfig(p, canvasW, canvasH)
	if err != nil {
		return "", err
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal chart config: %w", err)
	}

	subtitle := ""
	if s := strings.TrimSpace(p.Subtitle); s != "" {
		subtitle = `<div class="subtitle">` + htmlEscape(s) + `</div>`
	}
	title := strings.TrimSpace(p.Title)
	titleBlock := ""
	if title != "" || subtitle != "" {
		titleBlock = `<div class="head"><div class="title">` + htmlEscape(title) + `</div>` + subtitle + `</div>`
	}

	yUnitJSON, _ := json.Marshal(strings.TrimSpace(p.YUnit))
	numFmtJSON, err := json.Marshal(numberFormatOptions(resolveValueKind(p)))
	if err != nil {
		return "", fmt.Errorf("marshal number format: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html lang="ko"><head><meta charset="utf-8">
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html,body { width:%dpx; height:%dpx; background:%s;
    font-family:'Noto Sans CJK KR','Noto Sans KR',sans-serif; -webkit-font-smoothing:antialiased; }
  .card { width:100%%; height:100%%; padding:26px 30px 24px; display:flex; flex-direction:column; }
  .head { margin-bottom:14px; }
  .title { color:%s; font-size:25px; font-weight:700; letter-spacing:-0.4px; line-height:1.25; }
  .subtitle { color:%s; font-size:14px; font-weight:400; margin-top:5px; letter-spacing:-0.2px; }
  .chartwrap { position:relative; flex:1 1 auto; min-height:0; }
</style></head><body>
<div class="card">%s<div class="chartwrap"><canvas id="c"></canvas></div></div>
<script>%s</script>
<script>
  const cfg = %s;
  const Y_UNIT = %s;
  const Y_NUMFMT = %s;
%s
</script>
</body></html>`,
		canvasW, canvasH, chartBg, chartFg, chartMuted,
		titleBlock, chartJS, cfgJSON, yUnitJSON, numFmtJSON, chartRuntimeJS)
	return b.String(), nil
}

// chartRuntimeJS is the config post-processing that a JSON-only Chart.js config
// cannot express (callbacks, canvas drawing):
//   - value-axis tick suffix for y_unit ("20" → "20만원") — replaces the old
//     "단위: X" axis-title workaround. Y_NUMFMT carries the value_kind's decimal
//     policy (Intl.NumberFormat options; empty object = no policy, defer to
//     Chart.js). With neither a unit nor a kind the callback is not installed at
//     all — see the note in the script;
//   - doughnut segment % labels + value-annotated legend entries: a static PNG
//     has no hover tooltips, so without these a 구성비 chart shows no numbers
//     at all (the audit's worst chart finding).
//
// Kept as a raw const (not inside the Fprintf format string) so its % and
// template literals need no escaping.
const chartRuntimeJS = `
  const fmtVal = v => Number(v).toLocaleString('ko-KR', Y_NUMFMT);
  // Tick numbers: only a value_kind pins the decimals. Without one we hand the
  // number back to Chart.js's own numeric formatter, which derives precision
  // (and scientific notation) from the tick SPACING — a flat locale default
  // caps at 3 fraction digits and would collapse a 0.0001~0.0005 axis into a
  // column of "0". With neither a unit nor a kind there is nothing to add, so
  // the callback is not installed at all and Chart.js formats end to end.
  const HAS_NUMFMT = Object.keys(Y_NUMFMT).length > 0;
  const chartNumericTick = (globalThis.Chart && Chart.Ticks && Chart.Ticks.formatters)
    ? Chart.Ticks.formatters.numeric : null;
  if ((Y_UNIT || HAS_NUMFMT) && cfg.options && cfg.options.scales) {
    const axis = (cfg.options.indexAxis === 'y') ? 'x' : 'y';
    const sc = cfg.options.scales[axis];
    if (sc) {
      sc.ticks = sc.ticks || {};
      sc.ticks.callback = function (v, i, ticks) {
        if (!HAS_NUMFMT && chartNumericTick) return chartNumericTick.call(this, v, i, ticks) + Y_UNIT;
        return fmtVal(v) + Y_UNIT;
      };
    }
  }
  const segLabels = { id: 'segLabels', afterDatasetsDraw(chart) {
    if (chart.config.type !== 'doughnut') return;
    const data = chart.data.datasets[0].data.map(Number);
    const total = data.reduce((a, b) => a + (b || 0), 0);
    if (!total) return;
    const ctx = chart.ctx, meta = chart.getDatasetMeta(0);
    ctx.save();
    ctx.font = '600 16px "Noto Sans CJK KR","Noto Sans KR",sans-serif';
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle'; ctx.fillStyle = '#FFFFFF';
    meta.data.forEach((arc, i) => {
      const f = (data[i] || 0) / total;
      if (f < 0.04) return; // slice too thin to label legibly
      const pos = arc.tooltipPosition();
      ctx.fillText(Math.round(f * 100) + '%', pos.x, pos.y);
    });
    ctx.restore();
  }};
  if (cfg.type === 'doughnut' && Array.isArray(cfg.data.labels)) {
    const data = cfg.data.datasets[0].data.map(Number);
    const total = data.reduce((a, b) => a + (b || 0), 0) || 1;
    cfg.data.labels = cfg.data.labels.map((l, i) => {
      const v = data[i] || 0;
      return l + ' · ' + fmtVal(v) + Y_UNIT + ' (' + Math.round(v / total * 100) + '%)';
    });
  }
  cfg.plugins = [segLabels];
  new Chart(document.getElementById('c').getContext('2d'), cfg);
`

// chartConfig builds the Chart.js config object (marshaled to JSON, no JS
// callbacks) for the given params.
func chartConfig(p chartParams, canvasW, canvasH int) (map[string]any, error) {
	base := p.ChartType
	fill := false
	if base == "area" {
		base = "line"
		fill = true
	}
	switch base {
	case "line", "bar", "doughnut":
	default:
		return nil, fmt.Errorf("unsupported chart_type %q (use line, area, bar, or doughnut)", p.ChartType)
	}

	if base == "doughnut" {
		return doughnutConfig(p, canvasH), nil
	}
	return xyConfig(p, base, fill, canvasW, canvasH), nil
}

// xyConfig builds line/area/bar (and line+bar combos) with x/y axes.
func xyConfig(p chartParams, base string, fill bool, canvasW, canvasH int) map[string]any {
	datasets := make([]map[string]any, 0, len(p.Series))
	for i, s := range p.Series {
		color := chartPalette[i%len(chartPalette)]
		dtype := base
		if t := strings.ToLower(strings.TrimSpace(s.Type)); t == "line" || t == "bar" {
			dtype = t
		}
		ds := map[string]any{
			"label": s.Name,
			"data":  s.Data,
			"type":  dtype,
		}
		if dtype == "line" {
			ds["borderColor"] = color
			ds["backgroundColor"] = rgba(color, 0.16)
			ds["borderWidth"] = 2.6
			ds["tension"] = 0.35
			ds["pointRadius"] = 2.5
			ds["pointBackgroundColor"] = color
			ds["fill"] = fill
			// A line drawn over bars (combo) reads as a distinct overlay with a dash.
			if base == "bar" {
				ds["borderDash"] = []int{6, 4}
				ds["fill"] = false
			}
		} else { // bar
			ds["backgroundColor"] = rgba(color, 0.88)
			ds["borderColor"] = color
			ds["borderWidth"] = 0
			ds["borderRadius"] = 6
			ds["maxBarThickness"] = 54
		}
		datasets = append(datasets, ds)
	}

	// Category axis (labels) vs value axis (numbers) — horizontal bars swap
	// them. The y_unit tick suffix is attached in JS (chartRuntimeJS) since a
	// JSON-marshaled config cannot carry a callback.
	categoryTickCfg := map[string]any{"color": chartMuted, "font": tickFont()}
	// Cardinality adaptation: keep every category label (rotating or shrinking
	// it) instead of letting the Chart.js autoSkip default drop labels silently.
	for k, v := range categoryTicks(p, canvasW) {
		categoryTickCfg[k] = v
	}
	categoryScale := map[string]any{
		"grid":  map[string]any{"display": false, "drawBorder": false},
		"ticks": categoryTickCfg,
	}
	valueScale := map[string]any{
		"grid":  map[string]any{"color": chartGrid, "drawBorder": false},
		"ticks": map[string]any{"color": chartMuted, "font": tickFont(), "padding": 6},
	}
	applyValueKind(valueScale, resolveValueKind(p))
	if p.Stacked {
		categoryScale["stacked"] = true
		valueScale["stacked"] = true
	}
	xScale, yScale := categoryScale, valueScale
	if p.Horizontal {
		xScale, yScale = valueScale, categoryScale
	}

	showLegend := len(p.Series) > 1 || anySeriesNamed(p.Series)
	options := map[string]any{
		"responsive":          true,
		"maintainAspectRatio": false,
		"animation":           false,
		"layout":              map[string]any{"padding": map[string]any{"top": 4, "right": 6}},
		"interaction":         map[string]any{"intersect": false},
		"plugins": map[string]any{
			"legend": legendCfg(showLegend),
		},
		"scales": map[string]any{"x": xScale, "y": yScale},
	}
	if p.Horizontal {
		options["indexAxis"] = "y"
	}
	return map[string]any{
		"type":    base,
		"data":    map[string]any{"labels": p.Labels, "datasets": datasets},
		"options": options,
	}
}

// doughnutConfig builds a doughnut (구성비) from the first series. canvasH decides
// how tightly the right-hand legend has to pack its per-slice entries.
func doughnutConfig(p chartParams, canvasH int) map[string]any {
	s := p.Series[0]
	colors := make([]string, len(s.Data))
	for i := range s.Data {
		colors[i] = chartPalette[i%len(chartPalette)]
	}
	return map[string]any{
		"type": "doughnut",
		"data": map[string]any{
			"labels": p.Labels,
			"datasets": []map[string]any{{
				"data":            s.Data,
				"backgroundColor": colors,
				"borderColor":     chartBg,
				"borderWidth":     3,
				"hoverOffset":     0,
			}},
		},
		"options": map[string]any{
			"responsive":          true,
			"maintainAspectRatio": false,
			"animation":           false,
			"cutout":              "60%",
			"layout":              map[string]any{"padding": 8},
			"plugins": map[string]any{
				"legend": map[string]any{
					"display":  true,
					"position": "right",
					"labels":   doughnutLegendLabels(len(p.Labels), canvasH),
				},
			},
		},
	}
}

func legendCfg(show bool) map[string]any {
	return map[string]any{
		"display":  show,
		"position": "top",
		"align":    "end",
		"labels": map[string]any{
			"color": chartFg, "font": legendFont(),
			"padding": 14, "boxWidth": 14, "boxHeight": 14, "usePointStyle": true,
		},
	}
}

func tickFont() map[string]any { return map[string]any{"family": "'Noto Sans CJK KR'", "size": 13} }

func legendFont() map[string]any { return map[string]any{"family": "'Noto Sans CJK KR'", "size": 14} }

func anySeriesNamed(series []chartSeries) bool {
	for _, s := range series {
		if strings.TrimSpace(s.Name) != "" {
			return true
		}
	}
	return false
}

// rgba turns "#RRGGBB" into an "rgba(r,g,b,a)" string for Chart.js fills.
func rgba(hex string, alpha float64) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return hex
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return "#" + hex
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", r, g, b, alpha)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// chartRenderImage screenshots the chart HTML to a PNG via the shared
// headless-Chromium renderer. The canvas size is known up front (the chart fills
// it) so no trim is needed; charts render synchronously, so a 4s virtual-time
// budget is plenty.
func chartRenderImage(ctx context.Context, htmlPath, pngPath string, canvasW, canvasH int) error {
	window := fmt.Sprintf("%d,%d", canvasW, canvasH)
	return renderHTMLToPNG(ctx, htmlPath, pngPath, window, 4000)
}
