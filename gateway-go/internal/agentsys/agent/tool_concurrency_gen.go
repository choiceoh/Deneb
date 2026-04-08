// Hand-written constants. Previously generated from YAML.

package agent

// readOnlyToolFallback is used when ToolExecutor does not implement
// ConcurrencyChecker. Prefer declaring ConcurrencySafe on ToolDef instead.
var readOnlyToolFallback = map[string]struct{}{
	"read":             {},
	"grep":             {},
	"find":             {},
	"tree":             {},
	"diff":             {},
	"analyze":          {},
	"batch_read":       {},
	"read_spillover":   {},
	"process":          {},
	"kv":               {},
	"memory":           {},
	"web":              {},
	"http":             {},
	"deep_research":    {},
	"health_check":     {},
	"sessions_list":    {},
	"sessions_history": {},
	"sessions_search":  {},
	"skills_list":      {},
	"agent_logs":       {},
	"gateway_logs":     {},
}
