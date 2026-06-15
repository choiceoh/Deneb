package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// stubFleet stands in for the SparkFleet control plane.
func stubFleet(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nodes":[{"name":"gx10","role":"head","reachable":true,"metrics":{"gpus":[{"utilPct":42,"tempC":55}],"memory":{"totalKB":131072000,"availableKB":65536000},"services":[{"name":"vllm","ok":false}]}}]}`))
	})
	mux.HandleFunc("/api/recipes", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"name":"qwen36","node":"gx10","container":"vllm_qwen36","port":8000,"status":{"running":true,"weightsPresent":true,"node":"gx10"}}]`))
	})
	mux.HandleFunc("/api/jobs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"job-1","title":"launch qwen36","state":"failed"}]`))
	})
	mux.HandleFunc("/api/recipes/action", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jobId":"job-9"}`))
	})
	mux.HandleFunc("/api/jobs/job-9/cancel", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/assist/logs", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"container":"vllm_qwen36","state":"exited (code 1)","findings":[{"cause":"OOM","fix":"lower gmu"}],"llm":"KV cache too large"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fleetDepsFor(rawURL string) *toolctx.FleetDeps {
	return &toolctx.FleetDeps{BaseURL: func() string { return rawURL }, Token: func() string { return "" }}
}

func runFleet(t *testing.T, d *toolctx.FleetDeps, args map[string]any) string {
	t.Helper()
	in, _ := json.Marshal(args)
	out, err := ToolFleet(d)(context.Background(), in)
	if err != nil {
		t.Fatalf("fleet(%v): %v", args, err)
	}
	return out
}

func TestFleetTool_off(t *testing.T) {
	// No base URL wired → integration off, every action a calm message (no error).
	out := runFleet(t, &toolctx.FleetDeps{}, map[string]any{"action": "status"})
	if !strings.Contains(out, "꺼져") {
		t.Errorf("expected off message, got %q", out)
	}
}

func TestFleetTool_status(t *testing.T) {
	out := runFleet(t, fleetDepsFor(stubFleet(t).URL), map[string]any{"action": "status"})
	for _, want := range []string{"gx10", "qwen36", "GPU 42%", "다운: vllm", "실패 작업"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q in:\n%s", want, out)
		}
	}
}

func TestFleetTool_recipeAction(t *testing.T) {
	out := runFleet(t, fleetDepsFor(stubFleet(t).URL), map[string]any{"action": "restart", "recipe": "qwen36"})
	if !strings.Contains(out, "job-9") {
		t.Errorf("expected job id in:\n%s", out)
	}
}

func TestFleetTool_actionNeedsRecipe(t *testing.T) {
	out := runFleet(t, fleetDepsFor(stubFleet(t).URL), map[string]any{"action": "launch"})
	if !strings.Contains(out, "recipe") {
		t.Errorf("expected recipe-required message, got %q", out)
	}
}

func TestFleetTool_cancel(t *testing.T) {
	out := runFleet(t, fleetDepsFor(stubFleet(t).URL), map[string]any{"action": "cancel", "jobId": "job-9"})
	if !strings.Contains(out, "취소") {
		t.Errorf("expected cancel confirmation, got %q", out)
	}
}

func TestFleetTool_diagnose(t *testing.T) {
	out := runFleet(t, fleetDepsFor(stubFleet(t).URL), map[string]any{"action": "diagnose", "recipe": "qwen36"})
	for _, want := range []string{"OOM", "KV cache"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagnose missing %q in:\n%s", want, out)
		}
	}
}

func TestFleetTool_unknownAction(t *testing.T) {
	in, _ := json.Marshal(map[string]any{"action": "nuke"})
	if _, err := ToolFleet(fleetDepsFor(stubFleet(t).URL))(context.Background(), in); err == nil {
		t.Error("expected error for unknown action")
	}
}
