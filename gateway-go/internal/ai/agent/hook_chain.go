// hook_chain.go — declarative, singleton-safe composition for the BeforeAPICall
// hook chain.
//
// ComposeBeforeAPICall threads hooks in the order the caller happens to pass
// them, and AgentConfig.BeforeAPICall is a single field whose doc warns that
// "Overwriting this field silently replaces any prior hook". Both make ordering
// and clobbering a matter of getting every call site right by convention. The
// prompt-cache doctrine leans on that convention hard: the trailing cache hook
// MUST run last, which today is enforced only by being the final argument.
//
// BeforeAPICallChain turns those conventions into structure (a narrow borrow of
// HarnessX's typed-processor algebra — declarative `_order` + `_singleton_group`
// — without the rest): each hook declares a stage (PRE/NORMAL/POST) and a unique
// name, so "trailing runs last" is a property of the hook, not of argument order,
// and registering two hooks under one name is a loud, deterministic conflict
// instead of a silent overwrite.
package agent

import (
	"log/slog"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// BeforeAPICallStage is a coarse ordering tier for BeforeAPICall hooks. Hooks
// run PRE, then NORMAL, then POST; within a stage they run in registration order
// (an `after` dependency can push one further back). It replaces "pass the
// arguments to ComposeBeforeAPICall in the right order at every call site": e.g.
// the trailing prompt-cache hook declares HookStagePost instead of relying on
// being the final argument.
type BeforeAPICallStage int

const (
	HookStagePre    BeforeAPICallStage = -1
	HookStageNormal BeforeAPICallStage = 0
	HookStagePost   BeforeAPICallStage = 1
)

type beforeAPICallEntry struct {
	name     string
	stage    BeforeAPICallStage
	after    []string
	regIndex int
	fn       func(messages []llm.Message) []llm.Message
}

// BeforeAPICallChain accumulates named, ordered BeforeAPICall hooks and composes
// them deterministically. Two registrations sharing a name are a singleton-group
// conflict: the first wins and Build logs the duplicate at Error, turning the
// silent-clobber footgun documented on AgentConfig.BeforeAPICall into a loud,
// deterministic one. The zero value is ready to use.
type BeforeAPICallChain struct {
	entries []beforeAPICallEntry
	seen    map[string]bool
	dups    []string
}

// Add registers fn under a unique name at the given stage. A nil fn is skipped
// (the common "feature disabled" shape), so callers can register conditionally
// without pre-checking. A duplicate (or empty) name is rejected — the first
// registration wins — and surfaced by Build. after lists hook names that must
// run before this one; unknown names are ignored.
func (c *BeforeAPICallChain) Add(name string, stage BeforeAPICallStage, fn func(messages []llm.Message) []llm.Message, after ...string) {
	if fn == nil {
		return
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if name == "" || c.seen[name] {
		c.dups = append(c.dups, name)
		return
	}
	c.seen[name] = true
	c.entries = append(c.entries, beforeAPICallEntry{
		name:     name,
		stage:    stage,
		after:    after,
		regIndex: len(c.entries),
		fn:       fn,
	})
}

// Build composes the registered hooks into a single BeforeAPICall function in
// (stage, after, registration) order, or nil when none are registered. It logs
// any duplicate/empty-name conflicts at Error so a silent clobber cannot hide.
func (c *BeforeAPICallChain) Build(logger *slog.Logger) func(messages []llm.Message) []llm.Message {
	if logger != nil {
		for _, d := range c.dups {
			logger.Error("agent: duplicate BeforeAPICall hook ignored (singleton group)", "name", d)
		}
	}
	ordered := c.ordered()
	fns := make([]func(messages []llm.Message) []llm.Message, 0, len(ordered))
	for _, e := range ordered {
		fns = append(fns, e.fn)
	}
	return ComposeBeforeAPICall(fns...)
}

// ordered returns the entries in deterministic run order: primarily by stage,
// then a stable topological pass that honours `after`, with registration order
// as the tie-break. `after` is a hard constraint, so it wins over the stage tier
// in the rare case the two disagree.
func (c *BeforeAPICallChain) ordered() []beforeAPICallEntry {
	base := make([]beforeAPICallEntry, len(c.entries))
	copy(base, c.entries)
	sort.SliceStable(base, func(i, j int) bool {
		if base[i].stage != base[j].stage {
			return base[i].stage < base[j].stage
		}
		return base[i].regIndex < base[j].regIndex
	})

	idx := make(map[string]int, len(base))
	for i, e := range base {
		idx[e.name] = i
	}
	indeg := make([]int, len(base))
	adj := make([][]int, len(base))
	for i, e := range base {
		for _, dep := range e.after {
			if j, ok := idx[dep]; ok {
				adj[j] = append(adj[j], i)
				indeg[i]++
			}
		}
	}

	out := make([]beforeAPICallEntry, 0, len(base))
	used := make([]bool, len(base))
	for len(out) < len(base) {
		progressed := false
		for i := range base {
			if used[i] || indeg[i] != 0 {
				continue
			}
			used[i] = true
			out = append(out, base[i])
			for _, k := range adj[i] {
				indeg[k]--
			}
			progressed = true
			break // restart the scan so ready nodes keep their stable base order
		}
		if !progressed {
			// Cycle (or nothing left ready): append the remainder in base order
			// so the result stays a total order rather than dropping hooks.
			for i := range base {
				if !used[i] {
					out = append(out, base[i])
				}
			}
			break
		}
	}
	return out
}
