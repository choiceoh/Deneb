package sparkfleet

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestNilClientIsSafe(t *testing.T) {
	var c *Client
	c.Run(context.Background()) // must not panic
	if rep := c.HealthReport(); rep.Status != "off" {
		t.Errorf("nil client report: got %q want off", rep.Status)
	}
}
