package forecast

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewDisabledWithoutURL(t *testing.T) {
	if New("") != nil || New("   ") != nil {
		t.Fatal("empty base URL must leave the client disabled")
	}
	var c *Client
	if _, err := c.Forecast(context.Background(), Request{Series: []Series{{1, 2, 3, 4}}}); err == nil {
		t.Fatal("nil client must refuse instead of panicking")
	}
}

func TestSeriesMarshalsMissingPointsAsNull(t *testing.T) {
	raw, err := json.Marshal(Series{1.5, math.NaN(), 3, math.Inf(1)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// A gap must reach the sidecar as null so Chronos-2 imputes it; encoding it
	// as 0 would feed the model a fabricated observation.
	if got, want := string(raw), "[1.5,null,3,null]"; got != want {
		t.Fatalf("series JSON = %s, want %s", got, want)
	}
}

func TestForecastRoundTripSendsSeriesAndDecodesBand(t *testing.T) {
	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/forecast") {
			t.Errorf("path = %s, want /forecast", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &seen); err != nil {
			t.Errorf("request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"model":"amazon/chronos-2","horizon":2,"quantile_levels":[0.1,0.5,0.9],
			"series":[{"quantiles":[[1,2,3],[4,5,6]],"mean":[2,5]}],"elapsed_ms":47}`))
	}))
	defer srv.Close()

	resp, err := New(srv.URL).Forecast(context.Background(), Request{
		Series:    []Series{{10, 11, 12, 13}},
		Horizon:   2,
		Quantiles: []float64{0.1, 0.5, 0.9},
	})
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}
	if seen["horizon"].(float64) != 2 || len(seen["series"].([]any)) != 1 {
		t.Fatalf("request did not carry the series/horizon: %#v", seen)
	}
	if resp.Model != "amazon/chronos-2" || resp.ElapsedMS != 47 {
		t.Fatalf("response metadata = %#v", resp)
	}
	median := resp.Series[0].Median(resp.QuantileLevels)
	if len(median) != 2 || median[0] != 2 || median[1] != 5 {
		t.Fatalf("median track = %v, want [2 5]", median)
	}
	if got := resp.Series[0].Median([]float64{0.1, 0.9}); got != nil {
		t.Fatalf("median without a 0.5 level = %v, want nil", got)
	}
}

func TestForecastSurfacesSidecarValidationMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"series[0]: need at least 4 points, got 2"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Forecast(context.Background(), Request{Series: []Series{{1, 2}}, Horizon: 3})
	// The agent can only fix its own call if the specific reason survives.
	if err == nil || !strings.Contains(err.Error(), "need at least 4 points") {
		t.Fatalf("err = %v, want the sidecar's validation message", err)
	}
}

func TestForecastRejectsSeriesCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","horizon":1,"quantile_levels":[0.5],"series":[{"quantiles":[[1]],"mean":[1]}]}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).Forecast(context.Background(), Request{
		Series:  []Series{{1, 2, 3, 4}, {5, 6, 7, 8}},
		Horizon: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "expected 2 series") {
		t.Fatalf("err = %v, want a per-series alignment failure", err)
	}
}

func TestHealthRequiresOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","model":"amazon/chronos-2"}`))
	}))
	defer srv.Close()
	model, err := New(srv.URL).Health(context.Background())
	if err != nil || model != "amazon/chronos-2" {
		t.Fatalf("health = %q, %v", model, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"loading"}`))
	}))
	defer bad.Close()
	if _, err := New(bad.URL).Health(context.Background()); err == nil {
		t.Fatal("a sidecar that is not ok must report unhealthy")
	}
}
