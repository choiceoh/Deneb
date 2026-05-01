package modelrole

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newDiscoverySrv(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	// TestMain installs a probe-fails-fast client to keep registry tests
	// hermetic. The discovery tests need a real working client, so they
	// install one for the duration of the test.
	prev := vllmDiscoveryClient
	vllmDiscoveryClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { vllmDiscoveryClient = prev })
	return srv
}

func TestDiscoverServedVllmModels(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		status   int
		wantIDs  []string
		wantErr  bool
	}{
		{
			name:    "single served model",
			body:    `{"data":[{"id":"qwen3.6-35b-a3b","object":"model"}]}`,
			status:  200,
			wantIDs: []string{"qwen3.6-35b-a3b"},
		},
		{
			name:    "multiple served models preserve order",
			body:    `{"data":[{"id":"a"},{"id":"b"}]}`,
			status:  200,
			wantIDs: []string{"a", "b"},
		},
		{
			name:    "empty data is an error",
			body:    `{"data":[]}`,
			status:  200,
			wantErr: true,
		},
		{
			name:    "non-200 status is an error",
			body:    `nope`,
			status:  500,
			wantErr: true,
		},
		{
			name:    "malformed JSON is an error",
			body:    `not json`,
			status:  200,
			wantErr: true,
		},
		{
			name:    "blank ids are skipped, empty list errors",
			body:    `{"data":[{"id":""},{"id":"   "}]}`,
			status:  200,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newDiscoverySrv(t, tc.body, tc.status)
			got, err := DiscoverServedVllmModels(context.Background(), srv.URL+"/v1")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got ids=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("got %v, want %v", got, tc.wantIDs)
			}
			for i := range got {
				if got[i] != tc.wantIDs[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.wantIDs[i])
				}
			}
		})
	}
}

func TestReconcileVllmModel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("non-vllm provider is no-op", func(t *testing.T) {
		cfg := ModelConfig{ProviderID: "zai", Model: "glm-5.1", BaseURL: "http://x"}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "glm-5.1" {
			t.Errorf("model = %q, want unchanged", cfg.Model)
		}
	})

	t.Run("matching configured model is left untouched", func(t *testing.T) {
		srv := newDiscoverySrv(t, `{"data":[{"id":"qwen3.6-35b-a3b"}]}`, 200)
		cfg := ModelConfig{ProviderID: "vllm", Model: "qwen3.6-35b-a3b", BaseURL: srv.URL + "/v1"}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "qwen3.6-35b-a3b" {
			t.Errorf("model = %q, want unchanged", cfg.Model)
		}
	})

	t.Run("mismatched configured model is replaced with first served", func(t *testing.T) {
		srv := newDiscoverySrv(t, `{"data":[{"id":"qwen3.6-35b-a3b"}]}`, 200)
		cfg := ModelConfig{ProviderID: "vllm", Model: "qwen3.6", BaseURL: srv.URL + "/v1"}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "qwen3.6-35b-a3b" {
			t.Errorf("model = %q, want auto-substituted", cfg.Model)
		}
	})

	t.Run("empty configured model is populated", func(t *testing.T) {
		srv := newDiscoverySrv(t, `{"data":[{"id":"served-default"}]}`, 200)
		cfg := ModelConfig{ProviderID: "vllm", Model: "", BaseURL: srv.URL + "/v1"}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "served-default" {
			t.Errorf("model = %q, want auto-discovered", cfg.Model)
		}
	})

	t.Run("probe failure leaves config untouched", func(t *testing.T) {
		// Point at a closed port so the probe fails fast.
		srv := newDiscoverySrv(t, "", 200)
		srv.Close()
		cfg := ModelConfig{ProviderID: "vllm", Model: "qwen3.6", BaseURL: srv.URL + "/v1"}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "qwen3.6" {
			t.Errorf("model = %q, want unchanged on probe failure", cfg.Model)
		}
	})

	t.Run("empty baseURL is a no-op", func(t *testing.T) {
		cfg := ModelConfig{ProviderID: "vllm", Model: "qwen3.6", BaseURL: ""}
		reconcileVllmModel(logger, &cfg)
		if cfg.Model != "qwen3.6" {
			t.Errorf("model = %q, want unchanged", cfg.Model)
		}
	})
}
