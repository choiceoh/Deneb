// Package pluginrouter provides HTTP routing for server-side plugin handlers.
//
// Plugins register routes under path prefixes and the router dispatches
// incoming requests using longest-prefix matching. Routes may optionally
// require Bearer token authentication via a caller-supplied check function.
package pluginrouter

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// Route defines a single HTTP route registered by a plugin.
type Route struct {
	PluginID     string       // owning plugin identifier
	PathPrefix   string       // URL path prefix to match (e.g. "/plugins/my-plugin/")
	RequiresAuth bool         // if true, validate Bearer token before dispatching
	Handler      http.Handler // handler to invoke for matched requests
}

// Router dispatches incoming HTTP requests to registered plugin routes.
// Thread-safe for concurrent registration and lookup.
type Router struct {
	mu     sync.RWMutex
	routes []Route
	logger *slog.Logger

	// authCheck is called when a route requires auth. Returns true if the
	// request carries a valid Bearer token. Nil means auth is always denied
	// for protected routes (safe default).
	authCheck func(r *http.Request) bool
}

// New creates a new plugin HTTP router.
// authCheck is an optional function that validates Bearer tokens on protected
// routes. Pass nil to deny all auth-required routes (useful in tests or when
// auth is not configured).
func New(logger *slog.Logger, authCheck func(r *http.Request) bool) *Router {
	return &Router{
		logger:    logger,
		authCheck: authCheck,
	}
}

// Register adds a plugin HTTP route. Routes are matched in registration order
// by longest prefix, so more specific routes should be registered first or
// will still win by prefix length.
func (pr *Router) Register(route Route) {
	// Normalize prefix to end with "/".
	if route.PathPrefix != "" && !strings.HasSuffix(route.PathPrefix, "/") {
		route.PathPrefix = route.PathPrefix + "/"
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.routes = append(pr.routes, route)
	pr.logger.Info("registered plugin HTTP route",
		"pluginId", route.PluginID,
		"prefix", route.PathPrefix,
		"requiresAuth", route.RequiresAuth,
	)
}

// Handle dispatches the request to a matching plugin route.
// Returns true if the request was handled, false if no route matched.
func (pr *Router) Handle(w http.ResponseWriter, r *http.Request) bool {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	// Find the best (longest prefix) matching route.
	var best *Route
	bestLen := 0
	for i := range pr.routes {
		rt := &pr.routes[i]
		if strings.HasPrefix(r.URL.Path, rt.PathPrefix) && len(rt.PathPrefix) > bestLen {
			best = rt
			bestLen = len(rt.PathPrefix)
		}
	}

	if best == nil {
		return false
	}

	// Auth enforcement.
	if best.RequiresAuth {
		if pr.authCheck == nil || !pr.authCheck(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return true
		}
	}

	// Recover from handler panics to prevent server crash.
	defer func() {
		if rv := recover(); rv != nil {
			pr.logger.Error("plugin HTTP handler panic",
				"pluginId", best.PluginID,
				"prefix", best.PathPrefix,
				"panic", rv,
			)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()

	best.Handler.ServeHTTP(w, r)
	return true
}

// RouteCount returns the number of registered routes (useful for tests/status).
func (pr *Router) RouteCount() int {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return len(pr.routes)
}
