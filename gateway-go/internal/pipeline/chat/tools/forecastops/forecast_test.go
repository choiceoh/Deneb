package forecastops

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/forecast"
)

// stubClient records the request and replays a canned band so the tests assert
// the tool's own contract (coercion, guards, read-out) rather than the model's.
type stubClient struct {
	seen forecast.Request
	resp *forecast.Response
	err  error
}

func (s *stubClient) Forecast(_ context.Context, in forecast.Request) (*forecast.Response, error) {
	s.seen = in
	if s.err != nil {
		return nil, s.err
	}
	if s.resp != nil {
		return s.resp, nil
	}
	out := make([]forecast.SeriesForecast, len(in.Series))
	for i := range in.Series {
		q := make([][]float64, in.Horizon)
		mean := make([]float64, in.Horizon)
		for step := 0; step < in.Horizon; step++ {
			base := float64(100*(i+1) + step)
			q[step] = []float64{base - 5, base, base + 5}
			mean[step] = base
		}
		out[i] = forecast.SeriesForecast{Quantiles: q, Mean: mean}
	}
	return &forecast.Response{
		Model: "amazon/chronos-2", Horizon: in.Horizon,
		QuantileLevels: []float64{0.1, 0.5, 0.9}, Series: out, ElapsedMS: 48,
	}, nil
}

func run(t *testing.T, c Client, args string) string {
	t.Helper()
	out, err := ToolForecast(c)(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("tool returned a hard error for %s: %v", args, err)
	}
	return out
}

func TestForecastRendersBandAndAnchorsOnLastObservation(t *testing.T) {
	c := &stubClient{}
	out := run(t, c, `{"values":[10,20,30,40,50,60,70,80,90,100,110,120],"horizon":3,
		"step_labels":["10월","11월","12월"]}`)

	for _, want := range []string{"amazon/chronos-2", "실측 12점", "중앙값", "10월", "합계"} {
		if !strings.Contains(out, want) {
			t.Fatalf("read-out missing %q:\n%s", want, out)
		}
	}
	// 마지막 실측 120 → 첫 스텝 100 is -16.7%; the anchor is what makes the
	// number mean something to the reader.
	if !strings.Contains(out, "마지막 실측 120.0 → 첫 스텝 100.0") || !strings.Contains(out, "-16.7%") {
		t.Fatalf("missing the observed→forecast anchor:\n%s", out)
	}
	if !strings.Contains(out, "80% 구간 평균 ±5.0") {
		t.Fatalf("missing the band width:\n%s", out)
	}
	if c.seen.Horizon != 3 || len(c.seen.Series) != 1 || len(c.seen.Series[0]) != 12 {
		t.Fatalf("request = %#v", c.seen)
	}
}

func TestForecastPassesGapsThroughAsNaN(t *testing.T) {
	c := &stubClient{}
	run(t, c, `{"values":[10,null,30,"40",50,60],"horizon":2}`)

	got := c.seen.Series[0]
	if len(got) != 6 || !math.IsNaN(got[1]) {
		t.Fatalf("null must become a NaN gap, got %v", got)
	}
	// Models quote numbers often enough that rejecting "40" would fail real calls.
	if got[3] != 40 {
		t.Fatalf("quoted number not coerced: %v", got)
	}
}

func TestForecastAnchorSkipsTrailingGap(t *testing.T) {
	c := &stubClient{}
	out := run(t, c, `{"values":[10,20,30,40,50,60,70,80,90,100,110,null],"horizon":2}`)
	// The unclosed month is not an observation — anchoring on it would print "—".
	if !strings.Contains(out, "마지막 실측 110.0") {
		t.Fatalf("anchor should skip the trailing gap:\n%s", out)
	}
}

func TestForecastBatchesSeriesInOneCallWithLabels(t *testing.T) {
	c := &stubClient{}
	out := run(t, c, `{"series":[[1,2,3,4,5,6,7,8,9,10,11,12],[5,6,7,8,9,10,11,12,13,14,15,16]],
		"labels":["동진전기","한빛솔라"],"horizon":2}`)

	if len(c.seen.Series) != 2 {
		t.Fatalf("both series must ride one request, got %d", len(c.seen.Series))
	}
	if !strings.Contains(out, "동진전기") || !strings.Contains(out, "한빛솔라") || !strings.Contains(out, "2개 계열") {
		t.Fatalf("batch read-out missing labels:\n%s", out)
	}
}

func TestForecastGuardsReturnCalmKoreanInsteadOfFailing(t *testing.T) {
	c := &stubClient{}
	cases := []struct {
		name, args, want string
	}{
		{"no input", `{}`, "values"},
		{"both inputs", `{"values":[1,2,3,4],"series":[[1,2,3,4]]}`, "하나만"},
		{"too short", `{"values":[1,2]}`, "최소 4개"},
		{"horizon over cap", `{"values":[1,2,3,4],"horizon":600}`, "horizon은 1..60"},
		{"bad quantile", `{"values":[1,2,3,4],"quantiles":[1.5]}`, "0과 1 사이"},
		{"non numeric", `{"values":[1,2,3,"많음"]}`, "숫자가 아닙니다"},
		{"flat series", `{"series":[1,2,3,4]}`, "배열의 배열"},
		{"covariates on a batch", `{"series":[[1,2,3,4],[1,2,3,4]],"future_covariates":{"a":[1]}}`, "단일 계열"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, c, tc.args)
			if !strings.Contains(out, tc.want) {
				t.Fatalf("guard message = %q, want it to mention %q", out, tc.want)
			}
		})
	}
}

func TestForecastWarnsWhenContextIsTooShortForSeasonality(t *testing.T) {
	c := &stubClient{}
	short := run(t, c, `{"values":[1,2,3,4,5],"horizon":2}`)
	if !strings.Contains(short, "⚠️ 실측이 5점뿐") {
		t.Fatalf("a 5-point context must be flagged:\n%s", short)
	}
	long := run(t, c, `{"values":[1,2,3,4,5,6,7,8,9,10,11,12],"horizon":2}`)
	if strings.Contains(long, "⚠️") {
		t.Fatalf("a 12-point context must not be flagged:\n%s", long)
	}
	// Every read-out states what the number is not, so the agent does not relay
	// it as if the model knew about signed contracts.
	if !strings.Contains(long, "확정된 계약") {
		t.Fatalf("missing the interpretation caveat:\n%s", long)
	}
}

func TestForecastReportsSidecarFailureWithoutBreakingTheTurn(t *testing.T) {
	c := &stubClient{err: errSidecarDown{}}
	out := run(t, c, `{"values":[1,2,3,4],"horizon":2}`)
	if !strings.Contains(out, "예측 실패") || !strings.Contains(out, "connection refused") {
		t.Fatalf("sidecar failure must surface calmly: %q", out)
	}
}

type errSidecarDown struct{}

func (errSidecarDown) Error() string { return "forecast: request failed: connection refused" }

func TestForecastElidesAVeryLongHorizon(t *testing.T) {
	c := &stubClient{}
	out := run(t, c, `{"values":[1,2,3,4,5,6,7,8,9,10,11,12],"horizon":60}`)
	if !strings.Contains(out, "| … |") {
		t.Fatalf("a 60-step table must be elided:\n%s", out)
	}
	if strings.Count(out, "\n| ") > maxRenderRows+4 {
		t.Fatalf("elision did not bound the table:\n%s", out)
	}
}
