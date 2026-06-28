package observatory

import (
	"fmt"
	"strings"
)

// Markdown renders the report as a compact digest an agent can parse in one
// glance. Status tokens are plain words (ok/STALE/MISSING/DOWN) rather than
// emoji so they tokenize cleanly for an LLM reader.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Deneb self-status — %s\n", r.GeneratedAt.Format("2006-01-02 15:04 MST"))

	parts := make([]string, 0, len(r.Liveness))
	for _, l := range r.Liveness {
		switch {
		case l.Missing:
			parts = append(parts, l.Name+" MISSING")
		case l.Fresh:
			parts = append(parts, fmt.Sprintf("%s ok(%s)", l.Name, humanAge(l.AgeHours)))
		default:
			parts = append(parts, fmt.Sprintf("%s STALE(%s)", l.Name, humanAge(l.AgeHours)))
		}
	}
	fmt.Fprintf(&b, "LIVENESS  %s\n", strings.Join(parts, " · "))

	sat := ""
	if r.Skill.Total > 0 && r.Skill.Evolve+r.Skill.Genesis == 0 {
		sat = " (saturated: no evolve/genesis)"
	}
	fmt.Fprintf(&b, "SKILL     no-op %d / evolve %d / genesis %d%s\n",
		r.Skill.NoOp, r.Skill.Evolve, r.Skill.Genesis, sat)

	fmt.Fprintf(&b, "MEMORY    dreamer→%s diary→%s", dash(r.Memory.DreamerConsumedThrough), dash(r.Memory.LatestDiary))
	if r.Memory.BacklogDays > 0 {
		fmt.Fprintf(&b, " (backlog %dd)", r.Memory.BacklogDays)
	}
	fmt.Fprintf(&b, " · spillover %d today\n", r.Memory.SpilloverToday)

	fmt.Fprintf(&b, "MODELS    %d in %dh window", len(r.Models.Models), r.Models.WindowHours)
	if len(r.Models.Down) > 0 {
		fmt.Fprintf(&b, " · DOWN: %s", strings.Join(r.Models.Down, ", "))
	}
	b.WriteString("\n")

	if len(r.Frontier) > 0 {
		parts := make([]string, 0, len(r.Frontier))
		for _, fi := range r.Frontier {
			parts = append(parts, fmt.Sprintf("%s×%d", fi.Skill, fi.NoOps))
		}
		fmt.Fprintf(&b, "FRONTIER  %s\n", strings.Join(parts, " · "))
	}

	if len(r.Failures) > 0 {
		parts := make([]string, 0, len(r.Failures))
		for _, fc := range r.Failures {
			parts = append(parts, fmt.Sprintf("%s×%d", fc.Pattern, fc.Count))
		}
		fmt.Fprintf(&b, "FAILURES  %s (24h)\n", strings.Join(parts, " · "))
	} else {
		b.WriteString("FAILURES  none (24h)\n")
	}

	return b.String()
}

func humanAge(hours float64) string {
	switch {
	case hours < 1:
		return fmt.Sprintf("%dm", int(hours*60))
	case hours < 48:
		return fmt.Sprintf("%dh", int(hours))
	default:
		return fmt.Sprintf("%dd", int(hours/24))
	}
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
