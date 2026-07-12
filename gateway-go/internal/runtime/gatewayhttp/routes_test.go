package gatewayhttp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterRoutesPreservesClientMethodBoundaries(t *testing.T) {
	t.Setenv("DENEB_MCP_DISABLE", "1")
	mux := http.NewServeMux()
	RegisterRoutes(mux, Config{Logger: discardLogger()})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "native rpc rejects get",
			method:     http.MethodGet,
			path:       "/api/v1/miniapp/rpc",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "native events reject post",
			method:     http.MethodPost,
			path:       "/api/v1/miniapp/events",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "app update remains authenticated",
			method:     http.MethodGet,
			path:       "/api/v1/app/update/manifest",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "fleet proxy remains authenticated",
			method:     http.MethodGet,
			path:       "/api/v1/fleet/api/state",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRegisterFleetAlertRoutePreservesMethodAndPublisher(t *testing.T) {
	mux := http.NewServeMux()
	var gotTitle, gotBody string
	RegisterFleetAlertRoute(mux, FleetAlertConfig{
		Publish: func(title, body string) {
			gotTitle, gotBody = title, body
		},
		Logger: discardLogger(),
	})

	get := httptest.NewRequest(http.MethodGet, "/api/hooks/fleet", nil)
	get.RemoteAddr = "127.0.0.1:5555"
	getRecorder := httptest.NewRecorder()
	mux.ServeHTTP(getRecorder, get)
	if getRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", getRecorder.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/hooks/fleet",
		strings.NewReader(`{"level":"warn","title":"node warm","message":"temperature rising"}`))
	post.RemoteAddr = "127.0.0.1:5555"
	postRecorder := httptest.NewRecorder()
	mux.ServeHTTP(postRecorder, post)
	if postRecorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200; body=%s", postRecorder.Code, postRecorder.Body.String())
	}
	if gotTitle != "⚠️ 플릿 · node warm" || gotBody != "temperature rising" {
		t.Fatalf("published alert = (%q, %q)", gotTitle, gotBody)
	}
}

func TestWithCORSAnswersPreflightBeforeAuthentication(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})
	handler := WithCORS(next)
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/miniapp/rpc", nil)
	req.Header.Set("Origin", "http://localhost:1420")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if nextCalled {
		t.Fatal("preflight reached authenticated downstream handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Deneb-Client-Token") {
		t.Fatalf("allow headers = %q, missing client token header", got)
	}
}

type attachmentStub struct{}

func (attachmentStub) GetAttachment(context.Context, string, string) ([]byte, error) {
	return []byte("attachment"), nil
}

func TestAdaptAttachmentFactoryPreservesNarrowPort(t *testing.T) {
	adapted := adaptAttachmentFactory(func() (MailAttachmentClient, error) {
		return attachmentStub{}, nil
	})
	client, err := adapted()
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.GetAttachment(context.Background(), "message", "attachment")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "attachment" {
		t.Fatalf("attachment = %q", data)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
