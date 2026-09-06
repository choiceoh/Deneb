// Package forecast is the HTTP client for the Chronos-2 time-series sidecar
// (scripts/deploy/forecast-server.py). The sidecar is zero-shot: it holds no
// per-series state, so a forecast is one request carrying whatever numbers the
// caller already has. Disabled unless DENEB_FORECAST_URL is set.
package forecast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// requestTimeout covers a full batch. A 120-point / 14-step ask is ~50ms warm
// on GB10; the ceiling exists so a wedged sidecar cannot eat a whole turn.
const requestTimeout = 20 * time.Second

// DefaultBaseURL is where setup-forecast.sh installs the sidecar. Used when
// the env var is unset but a caller explicitly opts in.
const DefaultBaseURL = "http://127.0.0.1:8005"

// Series is one observation window. NaN marks a missing observation and
// marshals as JSON null — Chronos-2 imputes gaps natively, so a closed month
// or a skipped reading must NOT be pre-filled with a fabricated number.
type Series []float64

func (s Series) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, v := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b.WriteString("null")
			continue
		}
		b.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// Request is one forecast ask. Covariates are accepted for a single series
// only (the sidecar rejects them otherwise).
type Request struct {
	Series           []Series          `json:"series"`
	Horizon          int               `json:"horizon"`
	Quantiles        []float64         `json:"quantiles,omitempty"`
	PastCovariates   map[string]Series `json:"past_covariates,omitempty"`
	FutureCovariates map[string]Series `json:"future_covariates,omitempty"`
}

// SeriesForecast is one input series' prediction. Quantiles is horizon-major:
// Quantiles[step][level] follows Response.QuantileLevels.
type SeriesForecast struct {
	Quantiles [][]float64 `json:"quantiles"`
	Mean      []float64   `json:"mean"`
}

// Median returns the 0.5 track when it was requested, else nil.
func (f SeriesForecast) Median(levels []float64) []float64 {
	for i, q := range levels {
		if math.Abs(q-0.5) < 1e-9 {
			return f.Column(i)
		}
	}
	return nil
}

// Column extracts one quantile level's track across the horizon.
func (f SeriesForecast) Column(level int) []float64 {
	if level < 0 {
		return nil
	}
	out := make([]float64, 0, len(f.Quantiles))
	for _, step := range f.Quantiles {
		if level >= len(step) {
			return nil
		}
		out = append(out, step[level])
	}
	return out
}

// Response mirrors the sidecar payload.
type Response struct {
	Model          string           `json:"model"`
	Horizon        int              `json:"horizon"`
	QuantileLevels []float64        `json:"quantile_levels"`
	Series         []SeriesForecast `json:"series"`
	ElapsedMS      int              `json:"elapsed_ms"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

// NewFromEnv enables forecasting only when DENEB_FORECAST_URL is set — an
// unconfigured deployment must not advertise a tool that can only refuse.
func NewFromEnv() *Client {
	return New(os.Getenv("DENEB_FORECAST_URL"))
}

func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: requestTimeout}}
}

// Forecast returns one prediction per input series, in input order.
func (c *Client) Forecast(ctx context.Context, in Request) (*Response, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("forecast: sidecar not configured")
	}
	if len(in.Series) == 0 {
		return nil, fmt.Errorf("forecast: no series")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("forecast: marshal: %w", err)
	}
	endpoint := c.baseURL
	if !strings.HasSuffix(endpoint, "/forecast") {
		endpoint += "/forecast"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("forecast: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forecast: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("forecast: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The sidecar answers 400 with a specific validation message; surfacing
		// it verbatim is what lets the agent fix its own call.
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error) != "" {
			return nil, fmt.Errorf("forecast: %s", e.Error)
		}
		return nil, fmt.Errorf("forecast: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded Response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("forecast: decode: %w", err)
	}
	if len(decoded.Series) != len(in.Series) {
		return nil, fmt.Errorf("forecast: expected %d series, got %d", len(in.Series), len(decoded.Series))
	}
	return &decoded, nil
}

// Health reports the sidecar's model identity, or an error when it is down.
func (c *Client) Health(ctx context.Context) (string, error) {
	if c == nil || c.baseURL == "" {
		return "", fmt.Errorf("forecast: sidecar not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var h struct {
		Status string `json:"status"`
		Model  string `json:"model"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(raw, &h); err != nil || h.Status != "ok" {
		return "", fmt.Errorf("forecast: unhealthy sidecar")
	}
	return h.Model, nil
}
