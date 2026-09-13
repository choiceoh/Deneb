package observeops

import (
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// formatRouting renders who ACTUALLY served, from the router's own meter.
//
// It belongs next to the engine's speed numbers because those numbers are
// misleading alone: knowing the local engine decodes at N tokens a second says
// nothing if it answered a third of the turns. The router meters against the
// entry that served after failover, and the local engine and its cloud twin
// answer under the same model name, so this is the only place the split exists.
func formatRouting(usage observe.RouterUsage, localNames map[string]bool, ok bool) string {
	var b strings.Builder
	b.WriteString("\nrouting (who actually served, from the router's own meter)\n")
	if !ok {
		b.WriteString("  (router meter unavailable — it is a diagnostic, not a dependency)\n")
		return b.String()
	}
	if len(usage.Models) == 0 {
		b.WriteString("  (no requests metered in this window)\n")
		return b.String()
	}
	local, remote := usage.LocalShare(localNames)
	total := local + remote
	window := usage.Window
	if window == "" {
		window = "current window"
	}
	fmt.Fprintf(&b, "  window %s · %d requests\n", window, total)
	if total > 0 {
		fmt.Fprintf(&b, "  local engine served %d (%.1f%%) · elsewhere %d (%.1f%%)\n",
			local, 100*float64(local)/float64(total), remote, 100*float64(remote)/float64(total))
	}

	rows := make([]observe.RouterModelUsage, len(usage.Models))
	copy(rows, usage.Models)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Requests > rows[j].Requests })
	for _, m := range rows {
		where := "remote"
		if localNames[m.Model] {
			where = "LOCAL "
		}
		fmt.Fprintf(&b, "    %s %-26s %7d req · in %s · out %s\n",
			where, m.Model, m.Requests, compactTokens(m.InputTokens), compactTokens(m.OutputTokens))
	}
	b.WriteString("  the router meters the entry that answered, after failover — so a local\n")
	b.WriteString("  entry's row is traffic the engine really took, and the gap is substitution.\n")
	return b.String()
}

// compactTokens keeps large token counts readable in a fixed-width row.
func compactTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
