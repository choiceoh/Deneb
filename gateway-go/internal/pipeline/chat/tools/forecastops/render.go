package forecastops

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/forecast"
)

// render turns the sidecar payload into the read-out the agent relays. Single
// series get the full quantile band (that interval is the whole point of asking
// a model instead of drawing a trendline); a batch gets one median row each,
// because 16 banded tables is not something anyone reads.
func render(resp *forecast.Response, in []forecast.Series, labels, stepLabels []string) string {
	var b strings.Builder
	nf := formatFor(resp, in)
	if len(resp.Series) == 1 {
		renderSingle(&b, resp, in[0], stepLabels, nf)
	} else {
		renderBatch(&b, resp, labels, stepLabels, nf)
	}
	if short := shortestContext(in); short < seasonalHint {
		fmt.Fprintf(&b, "\n⚠️ 실측이 %d점뿐 — 주/월 주기를 추정하기엔 짧습니다. 구간을 넓게 보세요.", short)
	}
	b.WriteString("\n숫자 추이만으로 낸 통계 예측 — 이미 확정된 계약·발주·휴일은 반영되지 않았습니다.")
	return strings.TrimRight(b.String(), "\n")
}

func renderSingle(b *strings.Builder, resp *forecast.Response, in forecast.Series, stepLabels []string, nf numFormat) {
	levels := resp.QuantileLevels
	fmt.Fprintf(b, "📈 예측 — %s · 실측 %d점 → %d스텝 · %dms\n\n",
		resp.Model, len(in), resp.Horizon, resp.ElapsedMS)

	header := make([]string, 0, len(levels)+1)
	header = append(header, "스텝")
	for _, q := range levels {
		header = append(header, quantileName(q))
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("| --- |" + strings.Repeat(" ---: |", len(levels)) + "\n")

	f := resp.Series[0]
	for i, step := range f.Quantiles {
		if elided(b, i, len(f.Quantiles), len(levels)) {
			continue
		}
		cells := make([]string, 0, len(step)+1)
		cells = append(cells, stepLabel(stepLabels, i))
		for _, v := range step {
			cells = append(cells, nf.num(v))
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}

	center := f.Median(levels)
	if center == nil {
		center = f.Mean
	}
	fmt.Fprintf(b, "\n합계 %s", nf.num(sum(center)))
	if last, ok := lastObserved(in); ok && len(center) > 0 {
		fmt.Fprintf(b, " · 마지막 실측 %s → 첫 스텝 %s", nf.num(last), nf.num(center[0]))
		if last != 0 {
			fmt.Fprintf(b, " (%+.1f%%)", (center[0]-last)/math.Abs(last)*100)
		}
	}
	if w, ok := bandWidth(f, levels); ok {
		fmt.Fprintf(b, " · %s 구간 평균 ±%s", bandName(levels), nf.num(w))
	}
	b.WriteString("\n")
}

func renderBatch(b *strings.Builder, resp *forecast.Response, labels, stepLabels []string, nf numFormat) {
	levels := resp.QuantileLevels
	fmt.Fprintf(b, "📈 예측 — %s · %d개 계열 → %d스텝 · %dms (중앙값 기준)\n\n",
		resp.Model, len(resp.Series), resp.Horizon, resp.ElapsedMS)

	cols := resp.Horizon
	if cols > 12 {
		cols = 12
	}
	header := []string{"계열"}
	for i := 0; i < cols; i++ {
		header = append(header, stepLabel(stepLabels, i))
	}
	header = append(header, "합계")
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("| --- |" + strings.Repeat(" ---: |", len(header)-1) + "\n")

	for i, f := range resp.Series {
		center := f.Median(levels)
		if center == nil {
			center = f.Mean
		}
		cells := []string{seriesLabel(labels, i)}
		for step := 0; step < cols && step < len(center); step++ {
			cells = append(cells, nf.num(center[step]))
		}
		for len(cells) < cols+1 {
			cells = append(cells, "—")
		}
		cells = append(cells, nf.num(sum(center)))
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	if resp.Horizon > cols {
		fmt.Fprintf(b, "\n(%d스텝 중 앞 %d개만 표시 — 합계는 전 구간 기준)\n", resp.Horizon, cols)
	}
}

// elided keeps a long horizon readable: head 12 rows, tail 6, one marker row.
func elided(b *strings.Builder, i, total, levels int) bool {
	if total <= maxRenderRows {
		return false
	}
	head, tail := 12, 6
	if i < head || i >= total-tail {
		return false
	}
	if i == head {
		b.WriteString("| … |" + strings.Repeat(" … |", levels) + "\n")
	}
	return true
}

func quantileName(q float64) string {
	if math.Abs(q-0.5) < 1e-9 {
		return "중앙값"
	}
	return "P" + strconv.FormatFloat(q*100, 'f', -1, 64)
}

// bandName describes the outer quantile pair as a confidence band ("80%").
func bandName(levels []float64) string {
	if len(levels) < 2 {
		return ""
	}
	span := (levels[len(levels)-1] - levels[0]) * 100
	return strconv.FormatFloat(span, 'f', -1, 64) + "%"
}

// bandWidth is the mean half-width between the outermost quantiles — the one
// number that says how much to trust the middle track.
func bandWidth(f forecast.SeriesForecast, levels []float64) (float64, bool) {
	if len(levels) < 2 || len(f.Quantiles) == 0 {
		return 0, false
	}
	total, n := 0.0, 0
	for _, step := range f.Quantiles {
		if len(step) != len(levels) {
			return 0, false
		}
		total += (step[len(step)-1] - step[0]) / 2
		n++
	}
	if n == 0 {
		return 0, false
	}
	return total / float64(n), true
}

func stepLabel(labels []string, i int) string {
	if i < len(labels) && strings.TrimSpace(labels[i]) != "" {
		return strings.TrimSpace(labels[i])
	}
	return fmt.Sprintf("+%d", i+1)
}

func seriesLabel(labels []string, i int) string {
	if i < len(labels) && strings.TrimSpace(labels[i]) != "" {
		return strings.TrimSpace(labels[i])
	}
	return fmt.Sprintf("계열 %d", i+1)
}

// lastObserved skips trailing gaps — the anchor for "vs 마지막 실측" has to be
// a real reading, not the NaN a caller sent for an unclosed month.
func lastObserved(s forecast.Series) (float64, bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if !math.IsNaN(s[i]) {
			return s[i], true
		}
	}
	return 0, false
}

func shortestContext(in []forecast.Series) int {
	short := math.MaxInt32
	for _, s := range in {
		if len(s) < short {
			short = len(s)
		}
	}
	return short
}

func sum(v []float64) float64 {
	total := 0.0
	for _, x := range v {
		total += x
	}
	return total
}

// numFormat is chosen once per read-out, not per value: a column that reads
// 95 next to 100.0 looks like a bug, so precision follows the magnitude of the
// whole answer.
type numFormat struct {
	decimals int
	sep      bool
}

func formatFor(resp *forecast.Response, in []forecast.Series) numFormat {
	maxAbs := 0.0
	consider := func(v float64) {
		if !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) > maxAbs {
			maxAbs = math.Abs(v)
		}
	}
	for _, f := range resp.Series {
		for _, step := range f.Quantiles {
			for _, v := range step {
				consider(v)
			}
		}
		center := f.Median(resp.QuantileLevels)
		if center == nil {
			center = f.Mean
		}
		consider(sum(center)) // the 합계 cell shares the column's precision
	}
	for _, s := range in {
		for _, v := range s {
			consider(v)
		}
	}
	switch {
	case maxAbs >= 1000:
		return numFormat{decimals: 0, sep: true}
	case maxAbs >= 10:
		return numFormat{decimals: 1}
	default:
		return numFormat{decimals: 2}
	}
}

func (n numFormat) num(v float64) string {
	if math.IsNaN(v) {
		return "—"
	}
	s := strconv.FormatFloat(v, 'f', n.decimals, 64)
	if !n.sep {
		return s
	}
	return withSeparators(s)
}

// withSeparators groups the integer part of an already-formatted number.
func withSeparators(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	head, tail, _ := strings.Cut(s, ".")
	var parts []string
	for len(head) > 3 {
		parts = append([]string{head[len(head)-3:]}, parts...)
		head = head[:len(head)-3]
	}
	parts = append([]string{head}, parts...)
	out := strings.Join(parts, ",")
	if tail != "" {
		out += "." + tail
	}
	if neg {
		return "-" + out
	}
	return out
}
