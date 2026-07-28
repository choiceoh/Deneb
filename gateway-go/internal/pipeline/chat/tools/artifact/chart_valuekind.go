// chart_valuekind.go — value_kind: a narrow semantic hint for the value axis.
//
// Flint (microsoft/flint-chart) derives scales and number formats from a 70-type
// semantic catalogue. The catalogue itself is English and mostly irrelevant to
// what Deneb charts, but the idea holds: the agent knows WHAT the numbers are,
// and from that we can derive things it currently has to spell out by hand
// (y_unit) or cannot express at all — a temperature axis must not start at zero,
// because 18~26°C drawn from 0 flattens the whole series into a straight line.
//
// Deliberately five kinds, not seventy: each one has to change a rendering
// decision, otherwise it is just one more word for the model to guess at. An
// unset or unrecognized kind keeps the pre-existing behaviour exactly.
package artifact

import "strings"

// valueKindSpec is what a kind decides. decimals < 0 means "locale default",
// which is what the y_unit tick callback did before value_kind existed.
type valueKindSpec struct {
	unit     string
	zeroBase bool
	decimals int
}

var chartValueKinds = map[string]valueKindSpec{
	// 금액: the unit is 원/만원/백만원 and only the agent knows which, so no
	// default — but money ticks are always whole and always zero-based.
	"amount": {unit: "", zeroBase: true, decimals: 0},
	// 건수: integer ticks, so a 3-건 axis stops labelling 1.5.
	"count": {unit: "건", zeroBase: true, decimals: 0},
	// 비율
	"ratio": {unit: "%", zeroBase: true, decimals: 1},
	// 온도: the one kind that must NOT be forced to zero.
	"temperature": {unit: "°C", zeroBase: false, decimals: 1},
	// 점수
	"score": {unit: "점", zeroBase: true, decimals: 0},
}

// defaultValueKind preserves the behaviour charts had before value_kind: a
// zero-based value axis, no derived unit, locale-default tick decimals.
var defaultValueKind = valueKindSpec{unit: "", zeroBase: true, decimals: -1}

// resolveValueKind maps p.ValueKind to its spec, falling back to the pre-existing
// behaviour for an empty or unknown kind (the model may invent one; a chart is
// not worth failing over).
func resolveValueKind(p chartParams) valueKindSpec {
	spec, ok := chartValueKinds[strings.ToLower(strings.TrimSpace(p.ValueKind))]
	if !ok {
		return defaultValueKind
	}
	return spec
}

// resolveYUnit fills in the axis unit from the value kind when the agent left
// y_unit blank. An explicit y_unit always wins — "백만원" is not something the
// kind table can know.
func resolveYUnit(p chartParams) string {
	if u := strings.TrimSpace(p.YUnit); u != "" {
		return u
	}
	return resolveValueKind(p).unit
}

// applyValueKind stamps the kind's decisions onto the value scale: whether the
// axis may start above zero (with a little grace so the lowest point is not
// glued to the axis) and how many decimals a tick may carry.
func applyValueKind(valueScale map[string]any, spec valueKindSpec) {
	if spec.zeroBase {
		valueScale["beginAtZero"] = true
	} else {
		valueScale["beginAtZero"] = false
		valueScale["grace"] = "8%"
	}
	if spec.decimals >= 0 {
		ticks, _ := valueScale["ticks"].(map[string]any)
		if ticks != nil {
			ticks["precision"] = spec.decimals
		}
	}
}

// numberFormatOptions is the Intl.NumberFormat options object the render JS uses
// for tick and doughnut-legend values. Empty object = locale default.
func numberFormatOptions(spec valueKindSpec) map[string]any {
	if spec.decimals < 0 {
		return map[string]any{}
	}
	return map[string]any{"maximumFractionDigits": spec.decimals}
}
