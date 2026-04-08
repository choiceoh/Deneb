// Hand-written constants. Previously generated from YAML.

package agent

// readOnlyToolFallback is used when ToolExecutor does not implement
// ConcurrencyChecker. Prefer declaring ConcurrencySafe on ToolDef instead.
var readOnlyToolFallback = map[string]struct{}{
	"read":           {},
	"grep":           {},
	"find":           {},
	"tree":           {},
	"diff":           {},
	"analyze":        {},
	"read_spillover": {},
	"process":        {},
	"kv":             {},
	"memory":         {},
	"web":            {},
	"health_check":   {},
	"sessions":       {},
	"skills":         {},
	"polaris":        {},
}
