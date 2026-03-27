package vega

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldEnableVega_NoFFI(t *testing.T) {
	got := ShouldEnableVega(false, "", nil)
	if got {
		t.Error("should return false when FFI is not available")
	}
}

func TestShouldEnableVega_FFIAvailable(t *testing.T) {
	got := ShouldEnableVega(true, "", nil)
	if !got {
		t.Error("should return true when FFI available")
	}
}

func TestIsSglangReachable_EmptyURL(t *testing.T) {
	got := IsSglangReachable("")
	if got {
		t.Error("should return false for empty URL")
	}
}

func TestIsSglangReachable_InvalidURL(t *testing.T) {
	got := IsSglangReachable("http://127.0.0.1:99999/v1")
	if got {
		t.Error("should return false for unreachable URL")
	}
}

func TestProbeEmbedModel_ReturnsModelID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]string{
				{"id": "BAAI/bge-m3"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	got := probeEmbedModel(srv.URL)
	if got != "BAAI/bge-m3" {
		t.Errorf("expected BAAI/bge-m3, got %q", got)
	}
}

func TestProbeEmbedModel_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	got := probeEmbedModel(srv.URL)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestProbeEmbedModel_Unreachable(t *testing.T) {
	got := probeEmbedModel("http://127.0.0.1:99999/v1")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestDetectEmbedEndpoint_ExplicitEnv(t *testing.T) {
	t.Setenv("DENEB_EMBED_URL", "http://localhost:9999/v1")
	t.Setenv("DENEB_EMBED_MODEL", "test-embed-model")

	ep := DetectEmbedEndpoint(nil)
	if ep == nil {
		t.Fatal("expected non-nil endpoint")
	}
	if ep.URL != "http://localhost:9999/v1" || ep.Model != "test-embed-model" {
		t.Errorf("unexpected endpoint: %+v", ep)
	}
}

func TestDetectEmbedEndpoint_NoServer(t *testing.T) {
	t.Setenv("DENEB_EMBED_URL", "")
	t.Setenv("DENEB_EMBED_MODEL", "")

	// Override candidates so we don't accidentally hit a real server.
	orig := defaultEmbedCandidates
	defaultEmbedCandidates = []string{"http://127.0.0.1:99999/v1"}
	defer func() { defaultEmbedCandidates = orig }()

	ep := DetectEmbedEndpoint(nil)
	if ep != nil {
		t.Errorf("expected nil, got %+v", ep)
	}
}
