package sparkfleet

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestNewEmptyURLDisabled(t *testing.T) {
	if New("", nil) != nil {
		t.Error("empty URL should yield a nil client (integration off)")
	}
	if New("   ", slog.Default()) != nil {
		t.Error("blank URL should yield a nil client")
	}
}

func TestCheckClassifiesBackends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/services" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"services":[
			{"node":"gx10","name":"paddleocr","ok":true,"httpStatus":200},
			{"node":"gx10","name":"embeddings","ok":false,"httpStatus":0},
			{"node":"spark4tb","name":"vllm-tp2","ok":true,"httpStatus":200,"model":"step3p7"}
		]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, slog.Default())
	if c == nil {
		t.Fatal("expected a client for a non-empty URL")
	}
	c.check(context.Background())

	rep := c.HealthReport()
	if rep.Status != "degraded" {
		t.Errorf("status: got %q want degraded", rep.Status)
	}
	if len(rep.Down) != 1 || rep.Down[0] != "embeddings" {
		t.Errorf("down: got %v want [embeddings]", rep.Down)
	}
	if len(rep.Services) != 3 || rep.Services[2].Model != "step3p7" {
		t.Errorf("services parsed wrong: %+v", rep.Services)
	}
}

func TestCheckAllHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[{"name":"paddleocr","ok":true}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, slog.Default())
	c.check(context.Background())
	if rep := c.HealthReport(); rep.Status != "ok" || len(rep.Down) != 0 {
		t.Errorf("expected ok with no down services, got %+v", rep)
	}
}

func TestCheckUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(srv.URL, slog.Default())
	c.check(context.Background())
	if rep := c.HealthReport(); rep.Status != "unavailable" || rep.Error == "" {
		t.Errorf("expected unavailable with an error, got %+v", rep)
	}
}

// capHandler captures slog records for assertions.
type capHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}
func (h *capHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capHandler) WithGroup(string) slog.Handler      { return h }
func (h *capHandler) count(level slog.Level, msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			n++
		}
	}
	return n
}

// TestCheckLogsDownOnlyOnTransition verifies a persistently-down backend logs
// once (not every poll) and a recovery logs once — the fix for journald being
// flooded with the same down-set 1000+ times/day.
func TestCheckLogsDownOnlyOnTransition(t *testing.T) {
	down := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok := "true"
		if down {
			ok = "false"
		}
		_, _ = w.Write([]byte(`{"services":[{"name":"embeddings","ok":` + ok + `}]}`))
	}))
	defer srv.Close()

	cap := &capHandler{}
	c := New(srv.URL, slog.New(cap))

	c.check(context.Background()) // newly down → 1 Warn
	c.check(context.Background()) // still down → no new log
	c.check(context.Background()) // still down → no new log
	if w := cap.count(slog.LevelWarn, "GPU backends reported down by SparkFleet"); w != 1 {
		t.Fatalf("persistent down must log once, got %d Warn", w)
	}

	down = false
	c.check(context.Background()) // recovered → 1 Info
	if i := cap.count(slog.LevelInfo, "GPU backends recovered (SparkFleet)"); i != 1 {
		t.Fatalf("recovery must log once, got %d Info", i)
	}
}

func TestNilClientIsSafe(t *testing.T) {
	var c *Client
	c.Run(context.Background()) // must not panic
	if rep := c.HealthReport(); rep.Status != "off" {
		t.Errorf("nil client report: got %q want off", rep.Status)
	}
}
