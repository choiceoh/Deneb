package artifact

import (
	"strings"
	"testing"
)

// The reason value_kind exists: a temperature axis must not be forced to zero,
// while every other kind still is.
func TestValueKindTemperatureEscapesZeroBase(t *testing.T) {
	temp := chartParams{
		ChartType: "line", ValueKind: "temperature",
		Labels: []string{"월", "화"}, Series: []chartSeries{{Data: []float64{24.1, 25.3}}},
	}
	w, h := chartCanvas(temp)
	cfg, err := chartConfig(temp, w, h)
	if err != nil {
		t.Fatalf("chartConfig: %v", err)
	}
	y := cfg["options"].(map[string]any)["scales"].(map[string]any)["y"].(map[string]any)
	if y["beginAtZero"] != false {
		t.Errorf("temperature must not start at zero: %v", y["beginAtZero"])
	}
	if y["grace"] != "8%" {
		t.Errorf("a non-zero-based axis needs grace so the low point clears the axis: %v", y["grace"])
	}

	count := temp
	count.ValueKind = "count"
	w, h = chartCanvas(count)
	cfg, err = chartConfig(count, w, h)
	if err != nil {
		t.Fatalf("chartConfig: %v", err)
	}
	y = cfg["options"].(map[string]any)["scales"].(map[string]any)["y"].(map[string]any)
	if y["beginAtZero"] != true {
		t.Errorf("count must stay zero-based: %v", y["beginAtZero"])
	}
	if _, ok := y["grace"]; ok {
		t.Errorf("a zero-based axis must not carry grace: %v", y)
	}
}

// 건수 charts must not label ticks 1.5 — the kind pins integer ticks.
func TestValueKindCountPinsIntegerTicks(t *testing.T) {
	p := chartParams{
		ChartType: "bar", ValueKind: "count",
		Labels: []string{"a", "b"}, Series: []chartSeries{{Data: []float64{1, 3}}},
	}
	w, h := chartCanvas(p)
	cfg, err := chartConfig(p, w, h)
	if err != nil {
		t.Fatalf("chartConfig: %v", err)
	}
	y := cfg["options"].(map[string]any)["scales"].(map[string]any)["y"].(map[string]any)
	if got := y["ticks"].(map[string]any)["precision"]; got != 0 {
		t.Errorf("count ticks must be integers (precision 0), got %v", got)
	}
}

// The kind fills a blank y_unit but never overrides an explicit one — only the
// agent knows whether 금액 is in 원, 만원 or 백만원.
func TestResolveYUnitDefaultsFromKindButExplicitWins(t *testing.T) {
	cases := []struct {
		kind, unit, want string
	}{
		{"count", "", "건"},
		{"ratio", "", "%"},
		{"temperature", "", "°C"},
		{"amount", "", ""}, // 원/만원/백만원 — undecidable, stays blank
		{"count", "회", "회"},
		{"", "만원", "만원"},
		{"nonsense", "", ""},
	}
	for _, c := range cases {
		got := resolveYUnit(chartParams{ValueKind: c.kind, YUnit: c.unit})
		if got != c.want {
			t.Errorf("kind=%q y_unit=%q: want %q, got %q", c.kind, c.unit, c.want, got)
		}
	}
}

// An unset or invented kind must render exactly as charts did before value_kind:
// zero-based axis, no derived unit, locale-default tick decimals.
func TestUnknownValueKindKeepsPriorBehaviour(t *testing.T) {
	for _, kind := range []string{"", "  ", "Currency", "flint"} {
		spec := resolveValueKind(chartParams{ValueKind: kind})
		if spec != defaultValueKind {
			t.Errorf("kind %q must fall back to the default spec, got %+v", kind, spec)
		}
		if fmt := numberFormatOptions(spec); len(fmt) != 0 {
			t.Errorf("kind %q must leave number formatting to the locale, got %v", kind, fmt)
		}
	}
}

// The kind's decimal policy has to reach the render, where the tick callback and
// the doughnut legend both format values through it.
func TestBuildChartHTMLCarriesNumberFormat(t *testing.T) {
	p := chartParams{
		ChartType: "doughnut", ValueKind: "count",
		Labels: []string{"인허가", "공사"}, Series: []chartSeries{{Data: []float64{3, 7}}},
	}
	w, h := chartCanvas(p)
	html, err := buildChartHTML(p, w, h)
	if err != nil {
		t.Fatalf("buildChartHTML: %v", err)
	}
	for _, want := range []string{
		`const Y_UNIT = "건"`, // derived from the kind, not passed in
		`const Y_NUMFMT = {"maximumFractionDigits":0}`,
		"toLocaleString('ko-KR', Y_NUMFMT)",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chart html missing %q", want)
		}
	}
}

// With no unit and no kind there is nothing to add to a tick, so the override
// must not be installed: Chart.js derives tick precision from the tick spacing,
// and a flat locale default (max 3 fraction digits) would render a
// 0.0001~0.0005 axis as a column of "0".
func TestBuildChartHTMLLeavesTickFormattingToChartJSWithoutAHint(t *testing.T) {
	p := chartParams{
		ChartType: "line",
		Labels:    []string{"a", "b", "c"},
		Series:    []chartSeries{{Data: []float64{0.0001, 0.0003, 0.0005}}},
	}
	w, h := chartCanvas(p)
	html, err := buildChartHTML(p, w, h)
	if err != nil {
		t.Fatalf("buildChartHTML: %v", err)
	}
	if !strings.Contains(html, `const Y_UNIT = ""`) || !strings.Contains(html, `const Y_NUMFMT = {}`) {
		t.Fatalf("expected an empty unit and no number-format policy in the page")
	}
	// The guard that keeps Chart.js in charge, and the fallback that keeps its
	// adaptive precision even when a unit suffix has to be appended.
	for _, want := range []string{"if ((Y_UNIT || HAS_NUMFMT)", "chartNumericTick.call(this, v, i, ticks)"} {
		if !strings.Contains(html, want) {
			t.Errorf("chart html missing %q", want)
		}
	}
}
