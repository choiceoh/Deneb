// Hand-written constants. Previously generated from YAML.

package agent

// readOnlyToolFallback is used when ToolExecutor does not implement
// ConcurrencyChecker. Prefer declaring ConcurrencySafe on ToolDef instead.
var readOnlyToolFallback = map[string]bool{
	"read":             true,
	"grep":             true,
	"find":             true,
	"tree":             true,
	"diff":             true,
	"analyze":          true,
	"batch_read":       true,
	"search_and_read":  true,
	"inspect":          true,
	"read_spillover":   true,
	"process":          true,
	"kv":               true,
	"memory":           true,
	"web":              true,
	"http":             true,
	"health_check":     true,
	"image":            true,
	"sessions_list":    true,
	"sessions_history": true,
	"sessions_search":  true,
	"skills_list":      true,
	"agent_logs":       true,
	"gateway_logs":     true,
}
