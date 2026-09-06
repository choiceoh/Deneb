// Package forecastops implements the forecast tool over the Chronos-2 sidecar.
//
// The contract is deliberately "numbers in, numbers out": the agent has already
// pulled the history it cares about (ERP ledger, wiki 원장, 메일 집계), so the
// tool takes that series verbatim rather than owning a second data path into
// every business store. What it adds is the part the model cannot do in its
// head — a calibrated interval around each future step.
package forecastops

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/forecast"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

const (
	maxHorizon    = 60 // a chief-of-staff forecast past this is not a real ask
	maxRenderRows = 36 // beyond this the middle rows are elided, stats kept
	maxSeries     = 16 // batch ceiling; the sidecar allows 32
	minPoints     = 4  // the sidecar's floor, repeated here for a clear error
	seasonalHint  = 12 // below this, warn that seasonality cannot be inferred
)

// Client is the sidecar surface the tool needs (kept narrow for tests).
type Client interface {
	Forecast(ctx context.Context, in forecast.Request) (*forecast.Response, error)
}

type params struct {
	Values           []any            `json:"values"`
	Series           []any            `json:"series"`
	Labels           []string         `json:"labels"`
	Horizon          int              `json:"horizon"`
	Quantiles        []any            `json:"quantiles"`
	StepLabels       []string         `json:"step_labels"`
	PastCovariates   map[string][]any `json:"past_covariates"`
	FutureCovariates map[string][]any `json:"future_covariates"`
}

// ToolForecastFromEnv returns the executor when DENEB_FORECAST_URL points at a
// sidecar, and nil otherwise so registration can skip the tool entirely. Same
// rule fleet and browser follow: never show the agent a surface that can only
// refuse. The env cannot change without a restart, so deciding here is
// equivalent to deciding at call time.
func ToolForecastFromEnv() toolport.ToolFunc {
	client := forecast.NewFromEnv()
	if client == nil {
		return nil
	}
	return ToolForecast(client)
}

// ToolForecast returns the forecast executor. A nil client is guarded at
// registration — an unconfigured sidecar leaves the tool unregistered rather
// than advertising a surface that can only refuse.
func ToolForecast(client Client) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p params
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("forecast 파라미터 해석 실패: %w", err)
		}

		series, refusal := collectSeries(p)
		if refusal != "" {
			return refusal, nil
		}
		horizon := p.Horizon
		if horizon <= 0 {
			horizon = 12
		}
		if horizon > maxHorizon {
			return fmt.Sprintf("horizon은 1..%d 입니다 (요청 %d). 더 먼 미래는 예측 신뢰도가 없습니다.", maxHorizon, horizon), nil
		}
		levels, refusal := parseQuantiles(p.Quantiles)
		if refusal != "" {
			return refusal, nil
		}

		req := forecast.Request{Series: series, Horizon: horizon, Quantiles: levels}
		if len(p.PastCovariates) > 0 || len(p.FutureCovariates) > 0 {
			if len(series) > 1 {
				return "covariates(설명변수)는 단일 계열(values)에만 쓸 수 있습니다.", nil
			}
			if req.PastCovariates, refusal = covariates(p.PastCovariates, "past_covariates"); refusal != "" {
				return refusal, nil
			}
			if req.FutureCovariates, refusal = covariates(p.FutureCovariates, "future_covariates"); refusal != "" {
				return refusal, nil
			}
		}

		resp, err := client.Forecast(ctx, req)
		if err != nil {
			return fmt.Sprintf("예측 실패: %v", err), nil
		}
		return render(resp, series, p.Labels, p.StepLabels), nil
	}
}

// collectSeries accepts either values (one series) or series (a batch). The
// second return is a refusal for the agent, empty when the input is usable.
func collectSeries(p params) ([]forecast.Series, string) {
	if len(p.Values) > 0 && len(p.Series) > 0 {
		return nil, "values와 series 중 하나만 주세요 (단일 계열=values, 여러 계열=series)."
	}
	var raw [][]any
	switch {
	case len(p.Values) > 0:
		raw = [][]any{p.Values}
	case len(p.Series) > 0:
		for i, row := range p.Series {
			nested, ok := row.([]any)
			if !ok {
				return nil, fmt.Sprintf("series[%d]: 숫자 배열의 배열이어야 합니다. 계열이 하나면 values를 쓰세요.", i)
			}
			raw = append(raw, nested)
		}
	default:
		return nil, "values(숫자 배열)가 필요합니다. 과거 실측치를 시간순으로 주세요."
	}
	if len(raw) > maxSeries {
		return nil, fmt.Sprintf("계열이 너무 많습니다 (최대 %d개, 요청 %d개).", maxSeries, len(raw))
	}
	out := make([]forecast.Series, 0, len(raw))
	for i, row := range raw {
		vals, refusal := numbers(row, fmt.Sprintf("series[%d]", i))
		if refusal != "" {
			return nil, refusal
		}
		if len(vals) < minPoints {
			return nil, fmt.Sprintf("계열 %d: 실측치가 %d개뿐입니다 (최소 %d개 필요).", i+1, len(vals), minPoints)
		}
		out = append(out, vals)
	}
	return out, ""
}

// numbers coerces a JSON array to a series. null becomes NaN (a real gap the
// model imputes); numeric strings are accepted because models quote numbers.
func numbers(raw []any, field string) (forecast.Series, string) {
	out := make(forecast.Series, 0, len(raw))
	for i, v := range raw {
		switch t := v.(type) {
		case nil:
			out = append(out, math.NaN())
		case float64:
			out = append(out, t)
		case json.Number:
			f, err := t.Float64()
			if err != nil {
				return nil, fmt.Sprintf("%s[%d]: 숫자가 아닙니다 (%v).", field, i, v)
			}
			out = append(out, f)
		case string:
			s := strings.ReplaceAll(strings.TrimSpace(t), ",", "")
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Sprintf("%s[%d]: 숫자가 아닙니다 (%q).", field, i, t)
			}
			out = append(out, f)
		default:
			return nil, fmt.Sprintf("%s[%d]: 숫자가 아닙니다 (%v).", field, i, v)
		}
	}
	return out, ""
}

func covariates(in map[string][]any, field string) (map[string]forecast.Series, string) {
	if len(in) == 0 {
		return nil, ""
	}
	out := make(map[string]forecast.Series, len(in))
	for name, raw := range in {
		vals, refusal := numbers(raw, field+"."+name)
		if refusal != "" {
			return nil, refusal
		}
		out[name] = vals
	}
	return out, ""
}

func parseQuantiles(raw []any) ([]float64, string) {
	if len(raw) == 0 {
		return nil, "" // sidecar default: 0.1 / 0.5 / 0.9
	}
	vals, refusal := numbers(raw, "quantiles")
	if refusal != "" {
		return nil, refusal
	}
	for _, q := range vals {
		if math.IsNaN(q) || q <= 0 || q >= 1 {
			return nil, "quantiles는 0과 1 사이 값이어야 합니다 (예: [0.1, 0.5, 0.9])."
		}
	}
	return vals, ""
}
