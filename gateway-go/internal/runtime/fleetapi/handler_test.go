package fleetapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPathAllowlistAndDisabledProxy(t *testing.T) {
	for _, tc := range []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/api/jobs/job-12", true},
		{http.MethodPost, "/api/jobs/job-12/cancel", true},
		{http.MethodGet, "/api/jobs/a/b", false},
		{http.MethodPost, "/api/recipes/save", false},
	} {
		if got := PathAllowed(tc.method, tc.path); got != tc.want {
			t.Errorf("PathAllowed(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}

	recorder := httptest.NewRecorder()
	New(nil, nil).Proxy(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled proxy status = %d, want 503", recorder.Code)
	}
}
