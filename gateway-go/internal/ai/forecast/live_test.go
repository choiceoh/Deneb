package forecast

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// TestForecast_Live exercises the real Chronos-2 sidecar. Skipped everywhere by
// default (CI has no GPU); on the gateway host it is the only check that proves
// the wire contract still matches the server:
//
//	DENEB_FORECAST_LIVE=1 go test -run TestForecast_Live ./internal/ai/forecast/
//
// Override the endpoint with DENEB_FORECAST_URL.
func TestForecast_Live(t *testing.T) {
	if os.Getenv("DENEB_FORECAST_LIVE") == "" {
		t.Skip("set DENEB_FORECAST_LIVE=1 to run against the sidecar")
	}
	baseURL := os.Getenv("DENEB_FORECAST_URL")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := New(baseURL)
	if client == nil {
		t.Fatal("client disabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if model, err := client.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	} else {
		t.Logf("sidecar model: %s", model)
	}

	// A clean weekly cycle with one missing reading: the forecast must follow
	// the period rather than the local slope, which is what separates this model
	// from a trendline and what the gap handling has to survive.
	const period = 7
	history := make(Series, 0, 56)
	for i := 0; i < 56; i++ {
		history = append(history, 100+30*math.Sin(2*math.Pi*float64(i)/period))
	}
	history[20] = math.NaN()

	resp, err := client.Forecast(ctx, Request{
		Series:    []Series{history},
		Horizon:   period,
		Quantiles: []float64{0.1, 0.5, 0.9},
	})
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}
	if len(resp.Series) != 1 || len(resp.Series[0].Quantiles) != period {
		t.Fatalf("shape = %d series / %d steps", len(resp.Series), len(resp.Series[0].Quantiles))
	}
	median := resp.Series[0].Median(resp.QuantileLevels)
	if median == nil {
		t.Fatal("no median track returned")
	}
	for step, want := range median {
		expected := 100 + 30*math.Sin(2*math.Pi*float64(56+step)/period)
		if math.Abs(want-expected) > 15 {
			t.Errorf("step %d: %.1f, want ~%.1f (the weekly cycle was not carried)", step+1, want, expected)
		}
		low, high := resp.Series[0].Quantiles[step][0], resp.Series[0].Quantiles[step][2]
		if !(low <= want && want <= high) {
			t.Errorf("step %d: median %.1f outside its own P10..P90 band [%.1f, %.1f]", step+1, want, low, high)
		}
	}
	t.Logf("median: %.1f  elapsed: %dms", median, resp.ElapsedMS)
}
