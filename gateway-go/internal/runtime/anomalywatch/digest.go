package anomalywatch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// selfLogPrefix is how this lane names itself in the journal, and is the single
// source for BOTH the log messages it writes and the filter that keeps them out
// of its own window. Written in one place on purpose: the moment the two
// diverge, the lane starts eating its own output again.
const selfLogPrefix = "anomaly-watch:"

// maxDigestLines bounds what one pass hands the model. The window is Warn and
// above, so a healthy gateway produces far fewer than this; the cap exists for
// the unhealthy case, where an error storm would otherwise push the prompt past
// the model's context and lose the very lines worth reading.
const maxDigestLines = 120

// maxEvidenceChars bounds one rendered line. Log lines carrying a stack or a
// serialized payload would otherwise consume the whole budget alone.
const maxEvidenceChars = 400

// Digest is the bounded window a pass reasons over.
type Digest struct {
	Examined Examined
	// Text is what the model reads. Rendered rather than raw JSON because the
	// repeat counts below are the reading that matters most, and a model given
	// 200 raw lines reliably reports the loudest repeated error as 200 separate
	// anomalies.
	Text string
}

// BuildDigest renders a window from lines the caller already fetched.
//
// Lines arrive as an argument rather than the ring being read here for the same
// reason RuntimeErrorMiningTask takes an ErrorLines func: the ring is reached
// through the log capture, which the caller owns, and a package that snapshots
// runtime wiring at construction is how a lane ends up reading nil forever.
//
// Lines are grouped by message and counted rather than listed individually.
// That grouping is doing real work: recurrence is the single strongest signal
// separating a transient blip from a defect, and it is exactly the distinction
// a flat log dump destroys.
func BuildDigest(lines []observe.LogLine) Digest {
	// Drop this lane's own journal lines before anything else.
	//
	// Findings are logged at Warn so they reach the journal, and the window is
	// Warn and above — so without this filter every finding becomes evidence
	// for the next pass, which logs it again. Observed on the first full day:
	// a genesis parse failure was re-reported three hours running, the third
	// time quoting the lane's own earlier report rather than the original
	// error. That is output becoming its own input, and it inflates recurrence
	// counts, which is exactly the signal this digest leans on hardest.
	lines = dropSelfLines(lines)

	d := Digest{}
	if len(lines) == 0 {
		// An empty window is a real reading, not a missing one, and the model
		// is told so explicitly — otherwise it invents an explanation for the
		// silence it was handed.
		d.Text = "이 창에서 WARN 이상 로그가 한 줄도 없었다."
		return d
	}

	type group struct {
		msg    string
		level  string
		count  int
		sample observe.LogLine
	}
	groups := map[string]*group{}
	order := []string{}
	for _, l := range lines {
		d.Examined.LogLines++
		switch strings.ToLower(l.Level) {
		case "error":
			d.Examined.Errors++
		case "warn", "warning":
			d.Examined.Warns++
		}
		key := l.Level + "|" + l.Msg
		g, ok := groups[key]
		if !ok {
			g = &group{msg: l.Msg, level: l.Level, sample: l}
			groups[key] = g
			order = append(order, key)
		}
		g.count++
	}
	d.Examined.DistinctMessages = len(groups)

	// Loudest first: a reader scanning a truncated digest should meet the
	// recurring problem before the one-off.
	sort.SliceStable(order, func(i, j int) bool {
		return groups[order[i]].count > groups[order[j]].count
	})

	var b strings.Builder
	for _, key := range order {
		g := groups[key]
		fmt.Fprintf(&b, "[%s]", strings.ToUpper(g.level))
		if g.count > 1 {
			fmt.Fprintf(&b, "×%d", g.count)
		}
		fmt.Fprintf(&b, " %s", g.msg)
		if attrs := renderAttrs(g.sample.Attrs); attrs != "" {
			fmt.Fprintf(&b, " | %s", attrs)
		}
		b.WriteString("\n")
	}
	d.Text = truncate(b.String(), maxDigestLines*maxEvidenceChars)
	return d
}

// dropSelfLines removes the lane's own log output from a window.
func dropSelfLines(lines []observe.LogLine) []observe.LogLine {
	kept := lines[:0:0]
	for _, l := range lines {
		if strings.HasPrefix(l.Msg, selfLogPrefix) {
			continue
		}
		kept = append(kept, l)
	}
	return kept
}

// renderAttrs flattens a line's structured attributes in a stable order, so the
// same event renders identically across passes — a model comparing two windows
// should not see attribute reordering as a change.
func renderAttrs(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+truncate(attrs[k], 120))
	}
	return truncate(strings.Join(parts, " "), maxEvidenceChars)
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
