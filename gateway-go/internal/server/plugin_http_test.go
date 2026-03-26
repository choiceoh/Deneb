package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPluginHTTPRouter_RegisterAndMatch(t *testing.T) {
	router := NewPluginHTTPRouter(slog.Default(), nil)

	var called bool
	router.Register(PluginHTTPRoute{
		PluginID:   "test-plugin",
		PathPrefix: "/plugins/test-plugin/",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/plugins/test-plugin/webhook", nil)
	w := httptest.NewRecorder()

	handled := router.Handle(w, req)
	if !handled {
		t.Fatal("expected route to be handled")
	}
	if !called {
		t.Error("expected plugin handler to be called")
	}
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Result().StatusCode)
	}
}

func TestPluginHTTPRouter_UnmatchedPath(t *testing.T) {
	router := NewPluginHTTPRouter(slog.Default(), nil)

	router.Register(PluginHTTPRoute{
		PluginID:   "test-plugin",
		PathPrefix: "/plugins/test-plugin/",
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})

	req := httptest.NewRequest(http.MethodGet, "/plugins/other-plugin/webhook", nil)
	w := httptest.NewRecorder()

	handled := router.Handle(w, req)
	if handled {
		t.Error("expected unmatched path to return false")
	}
}

func TestPluginHTTPRouter_AuthEnforcement(t *testing.T) {
	// Auth check that accepts tokens starting with "valid-".
	authCheck := func(r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		return auth == "Bearer valid-token"
	}
	router := NewPluginHTTPRouter(slog.Default(), authCheck)

	var called bool
	router.Register(PluginHTTPRoute{
		PluginID:     "secure-plugin",
		PathPrefix:   "/plugins/secure-plugin/",
		RequiresAuth: true,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	})

	// Without auth — should get 401.
	req := httptest.NewRequest(http.MethodGet, "/plugins/secure-plugin/api", nil)
	w := httptest.NewRecorder()
	handled := router.Handle(w, req)
	if !handled {
		t.Fatal("expected auth-protected route to be handled")
	}
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Result().StatusCode)
	}
	if called {
		t.Error("handler should not be called without valid auth")
	}

	// With valid auth — should succeed.
	called = false
	req = httptest.NewRequest(http.MethodGet, "/plugins/secure-plugin/api", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w = httptest.NewRecorder()
	handled = router.Handle(w, req)
	if !handled {
		t.Fatal("expected auth-protected route to be handled with valid token")
	}
	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("expected 200 with valid auth, got %d", w.Result().StatusCode)
	}
	if !called {
		t.Error("handler should be called with valid auth")
	}
}

func TestPluginHTTPRouter_NilAuthCheckDeniesProtectedRoutes(t *testing.T) {
	// No auth check provided — protected routes should always be denied.
	router := NewPluginHTTPRouter(slog.Default(), nil)

	router.Register(PluginHTTPRoute{
		PluginID:     "protected",
		PathPrefix:   "/plugins/protected/",
		RequiresAuth: true,
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})

	req := httptest.NewRequest(http.MethodGet, "/plugins/protected/data", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()
	handled := router.Handle(w, req)
	if !handled {
		t.Fatal("expected route to be handled")
	}
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 with nil authCheck, got %d", w.Result().StatusCode)
	}
}

func TestPluginHTTPRouter_LongestPrefixWins(t *testing.T) {
	router := NewPluginHTTPRouter(slog.Default(), nil)

	var which string
	router.Register(PluginHTTPRoute{
		PluginID:   "parent",
		PathPrefix: "/plugins/",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			which = "parent"
		}),
	})
	router.Register(PluginHTTPRoute{
		PluginID:   "child",
		PathPrefix: "/plugins/child/",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			which = "child"
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/plugins/child/action", nil)
	w := httptest.NewRecorder()
	router.Handle(w, req)

	if which != "child" {
		t.Errorf("expected child route to win, got %q", which)
	}
}

func TestPluginHTTPRouter_RouteCount(t *testing.T) {
	router := NewPluginHTTPRouter(slog.Default(), nil)

	if router.RouteCount() != 0 {
		t.Error("expected 0 routes initially")
	}

	router.Register(PluginHTTPRoute{
		PluginID:   "a",
		PathPrefix: "/plugins/a/",
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})
	router.Register(PluginHTTPRoute{
		PluginID:   "b",
		PathPrefix: "/plugins/b/",
		Handler:    http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	})

	if router.RouteCount() != 2 {
		t.Errorf("expected 2 routes, got %d", router.RouteCount())
	}
}

func TestPluginHTTPRouter_PrefixNormalization(t *testing.T) {
	router := NewPluginHTTPRouter(slog.Default(), nil)

	var called bool
	// Register without trailing slash — should be normalized.
	router.Register(PluginHTTPRoute{
		PluginID:   "norm",
		PathPrefix: "/plugins/norm",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/plugins/norm/test", nil)
	w := httptest.NewRecorder()
	handled := router.Handle(w, req)
	if !handled || !called {
		t.Error("expected normalized prefix to match")
	}
}
