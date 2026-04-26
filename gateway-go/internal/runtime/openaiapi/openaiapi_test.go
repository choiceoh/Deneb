package openaiapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

type fakeRegistry struct {
	models map[modelrole.Role]string
}

func (f *fakeRegistry) FullModelID(role modelrole.Role) string {
	return f.models[role]
}

func newTestMux(deps Deps) *http.ServeMux {
	mux := http.NewServeMux()
	Mount(mux, deps)
	return mux
}

func TestModels_NoAuth_ReturnsAllConfiguredAliases(t *testing.T) {
	mux := newTestMux(Deps{
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			modelrole.RoleMain:        "anthropic/claude-sonnet-4-6",
			modelrole.RoleLightweight: "openai/gpt-5",
			modelrole.RoleFallback:    "anthropic/claude-haiku-4-5",
		}},
		StartedAt: func() time.Time { return time.Unix(1700000000, 0) },
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var got ModelsList
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Object != "list" {
		t.Errorf("object = %q, want list", got.Object)
	}
	wantIDs := []string{"deneb-main", "deneb-light", "deneb-fallback"}
	if len(got.Data) != len(wantIDs) {
		t.Fatalf("data len = %d, want %d", len(got.Data), len(wantIDs))
	}
	for i, m := range got.Data {
		if m.ID != wantIDs[i] {
			t.Errorf("data[%d].id = %q, want %q", i, m.ID, wantIDs[i])
		}
		if m.Object != "model" {
			t.Errorf("data[%d].object = %q, want model", i, m.Object)
		}
		if m.OwnedBy != "deneb" {
			t.Errorf("data[%d].owned_by = %q, want deneb", i, m.OwnedBy)
		}
		if m.Created != 1700000000 {
			t.Errorf("data[%d].created = %d, want 1700000000", i, m.Created)
		}
	}
}

func TestModels_SkipsRolesWithoutConfiguredModel(t *testing.T) {
	mux := newTestMux(Deps{
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			modelrole.RoleMain: "anthropic/claude-sonnet-4-6",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got ModelsList
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if len(got.Data) != 1 {
		t.Fatalf("data len = %d, want 1 (only main configured)", len(got.Data))
	}
	if got.Data[0].ID != "deneb-main" {
		t.Errorf("data[0].id = %q, want deneb-main", got.Data[0].ID)
	}
}

func TestModels_NilRegistryReturnsEmptyList(t *testing.T) {
	mux := newTestMux(Deps{})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got ModelsList
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got.Object != "list" {
		t.Errorf("object = %q, want list", got.Object)
	}
	if len(got.Data) != 0 {
		t.Errorf("data len = %d, want 0 (nil registry)", len(got.Data))
	}
}

func TestModels_BearerAuth(t *testing.T) {
	mux := newTestMux(Deps{
		AuthToken: "secret-token",
		ModelRegistry: &fakeRegistry{models: map[modelrole.Role]string{
			modelrole.RoleMain: "anthropic/claude-sonnet-4-6",
		}},
	})

	cases := []struct {
		name         string
		header       string
		wantStatus   int
		wantErrTypeF bool
	}{
		{"no header", "", http.StatusUnauthorized, true},
		{"wrong scheme", "Basic abc", http.StatusUnauthorized, true},
		{"missing token after scheme", "Bearer ", http.StatusUnauthorized, true},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized, true},
		{"correct token", "Bearer secret-token", http.StatusOK, false},
		{"case-insensitive scheme", "bearer secret-token", http.StatusOK, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, strings.TrimSpace(rec.Body.String()))
			}
			if tc.wantErrTypeF {
				var body ErrorBody
				if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body.Error.Type != "invalid_request_error" {
					t.Errorf("error.type = %q, want invalid_request_error", body.Error.Type)
				}
				if body.Error.Message == "" {
					t.Errorf("error.message empty")
				}
			}
		})
	}
}

func TestBearerFromHeader(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Bearer abc", "abc"},
		{"bearer abc", "abc"},
		{"BEARER abc", "abc"},
		{"Bearer  abc  ", "abc"},
		{"Basic abc", ""},
		{"abc", ""},
		{"", ""},
		{"Bearer", ""},
	}
	for _, tc := range cases {
		got := bearerFromHeader(tc.in)
		if got != tc.want {
			t.Errorf("bearerFromHeader(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
