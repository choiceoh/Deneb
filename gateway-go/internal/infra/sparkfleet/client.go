// Package sparkfleet reads the SparkFleet control plane — a sibling service on
// the DGX Spark that launches and monitors the GPU containers Deneb depends on
// (OCR, ASR, embeddings, vLLM). The gateway uses it to surface which backends are
// actually up instead of degrading silently. It is read-only and best-effort:
// when SparkFleet is unreachable, Deneb keeps running exactly as before.
package sparkfleet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	requestTimeout = 5 * time.Second
	pollInterval   = 60 * time.Second
)

// Service is one monitored backend as reported by SparkFleet's /api/services.
type Service struct {
	Node          string `json:"node"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	OK            bool   `json:"ok"`
	HTTPStatus    int    `json:"httpStatus"`
	Model         string `json:"model,omitempty"`
	NodeReachable bool   `json:"nodeReachable"`
}

// Report is the cached result of the last poll, surfaced on /healthz.
type Report struct {
	Status    string    `json:"status"` // ok | degraded | unavailable | off
	URL       string    `json:"url,omitempty"`
	Down      []string  `json:"down,omitempty"`
	Services  []Service `json:"services,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitempty"`
}

// Client polls a SparkFleet control plane. Use New; a nil *Client is valid and
// means "integration disabled" (every method is a safe no-op).
type Client struct {
	baseURL string
	token   string // optional X-Fleet-Token (DENEB_SPARKFLEET_TOKEN)
	http    *http.Client
	logger  *slog.Logger

	mu   sync.RWMutex
	last Report
}

// New returns a client for the SparkFleet base URL (e.g. http://127.0.0.1:18901),
// or nil when url is empty so callers can treat the feature as opt-in.
func New(url string, logger *slog.Logger) *Client {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: url,
		token:   strings.TrimSpace(os.Getenv("DENEB_SPARKFLEET_TOKEN")),
		http:    httputil.NewClient(requestTimeout),
		logger:  logger,
		last:    Report{Status: "unavailable", URL: url},
	}
}

// Run probes once immediately, then on a ticker until ctx is cancelled. Intended
// to run in a background goroutine.
func (c *Client) Run(ctx context.Context) {
	if c == nil {
		return
	}
	c.check(ctx)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.check(ctx)
		}
	}
}

// BaseURL returns the configured SparkFleet base URL, or "" when the
// integration is disabled. Nil-safe like every Client method; used by the
// gateway's fleet passthrough (/api/v1/fleet/*) to address the upstream.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// Token returns the optional SparkFleet API token (DENEB_SPARKFLEET_TOKEN),
// shared by the health poll and the fleet passthrough so a token-protected
// SparkFleet works for both or neither. Nil-safe.
func (c *Client) Token() string {
	if c == nil {
		return ""
	}
	return c.token
}

// HealthReport returns the most recent poll result for inclusion in /healthz.
func (c *Client) HealthReport() Report {
	if c == nil {
		return Report{Status: "off"}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

func (c *Client) check(ctx context.Context) {
	rep := Report{URL: c.baseURL, CheckedAt: time.Now()}
	svcs, err := c.fetch(ctx)
	if err != nil {
		rep.Status = "unavailable"
		rep.Error = err.Error()
		c.logger.Warn("sparkfleet unreachable; cannot verify GPU backends",
			"url", c.baseURL, "error", err)
	} else {
		rep.Services = svcs
		for _, s := range svcs {
			if !s.OK {
				rep.Down = append(rep.Down, s.Name)
			}
		}
		if len(rep.Down) > 0 {
			rep.Status = "degraded"
			c.logger.Warn("GPU backends reported down by SparkFleet",
				"down", strings.Join(rep.Down, ","), "url", c.baseURL)
		} else {
			rep.Status = "ok"
		}
	}
	c.mu.Lock()
	c.last = rep
	c.mu.Unlock()
}

func (c *Client) fetch(ctx context.Context) ([]Service, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/services", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("X-Fleet-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Services []Service `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out.Services, nil
}
