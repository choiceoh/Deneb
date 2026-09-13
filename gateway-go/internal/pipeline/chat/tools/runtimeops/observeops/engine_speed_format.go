package observeops

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
)

// formatEngineSpeed renders the local serving engines' measured days, newest
// first. Every number comes from the engine's own counters, so the caveats a
// reader needs are about what the ENGINE can and cannot see — not about the
// gateway's clock.
func formatEngineSpeed(days []enginespeed.DayStat) string {
	var b strings.Builder
	b.WriteString("engine speed (local serving engines, measured by the engine itself)\n")
	if len(days) == 0 {
		b.WriteString("  (no samples yet — the sampler needs one poll interval, and a live engine)\n")
		return b.String()
	}

	pollSec := 0
	for _, d := range days {
		row := d.Rates()
		label := d.Model
		if label == "" {
			label = d.Endpoint
		}
		if d.PollIntervalSec > pollSec {
			pollSec = d.PollIntervalSec
		}

		fmt.Fprintf(&b, "  %s  %s\n", d.Day, label)
		if !row.Measured() {
			fmt.Fprintf(&b, "      not measured (%d polls; the engine served nothing in this day's samples)\n", d.Polls)
			continue
		}
		fmt.Fprintf(&b, "      decode %s · prefill %s\n",
			rateOrDash(row.DecodeTokensPerSec), rateOrDash(row.PrefillTokensPerSec))
		fmt.Fprintf(&b, "      concurrency: %s while busy · %d peak observed\n",
			concurrencyOrDash(row.ConcurrencyWhileBusy), d.PeakConcurrency)
		fmt.Fprintf(&b, "      %d requests · %d prompt tok · %d generated tok\n",
			row.Requests, row.PromptTokens, row.GeneratedTokens)
		if d.Restarts > 0 {
			fmt.Fprintf(&b, "      %d engine restart(s): those intervals are not in the totals\n", d.Restarts)
		}
	}

	b.WriteString("  note: prefill has the engine's queue wait subtracted; decode is the reciprocal of its\n")
	b.WriteString("        mean per-output-token time. Concurrency-while-busy is request-seconds over the\n")
	b.WriteString("        engine's stepping seconds, so idle time is excluded rather than averaged in.\n")
	if pollSec > 0 {
		fmt.Fprintf(&b, "        Peak is the highest a %ds poll SAW — a shorter spike is invisible to it, so it is a floor.\n", pollSec)
	}
	b.WriteString("        Cloud models are absent by construction: this reads engines, and they expose nothing.\n")
	return b.String()
}

func rateOrDash(tokensPerSec float64) string {
	if tokensPerSec <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f tok/s", tokensPerSec)
}

func concurrencyOrDash(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}
