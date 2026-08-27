package server

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/anomalywatch"
)

// handleAnomalies serves the anomaly-watch ledger — what the local model
// noticed in the gateway's own logs while nobody was reading them.
//
// This route is the reason the lane exists. The watcher writes; a reader
// checking the runtime hours or days later reads. Loopback-only like the other
// introspection routes, and markdown by default because the reader is usually
// an agent in a shell, not a browser.
//
//	?since=24h   window to report (default 24h)
//	?format=json structured entries instead of the digest
func (s *Server) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	window := 24 * time.Hour
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			window = d
		}
	}
	entries, err := anomalywatch.Since(config.ResolveStateDir(), time.Now().Add(-window))
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if r.URL.Query().Get("format") == "json" {
		s.writeJSON(w, http.StatusOK, map[string]any{"window": window.String(), "entries": entries})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(renderAnomalyDigest(entries, window)))
}

// findingGroup is one distinct problem, however many passes reported it.
type findingGroup struct {
	severity, summary, evidence, why string
	count                            int
	first, last                      string
}

// collapseFindings groups by evidence — the one field the model is required to
// copy verbatim, and therefore the only one stable enough to key on. Summaries
// are freshly written each pass and drift wording every time ("보냈다" /
// "보냈는데" / "보낸다"), so grouping by summary would collapse nothing.
//
// Severity takes the HIGHEST any pass assigned: a problem one pass called low
// and another called medium is worth the reader's attention at medium.
func collapseFindings(entries []anomalywatch.Entry) []findingGroup {
	order := []string{}
	groups := map[string]*findingGroup{}
	for _, e := range entries {
		for _, f := range e.Findings {
			key := evidenceKey(f.Evidence)
			g, ok := groups[key]
			if !ok {
				g = &findingGroup{
					severity: f.Severity, summary: f.Summary,
					evidence: f.Evidence, why: f.WhyItMatters,
					first: e.At, last: e.At,
				}
				groups[key] = g
				order = append(order, key)
			}
			g.count++
			if e.At < g.first {
				g.first = e.At
			}
			if e.At > g.last {
				g.last = e.At
				// Keep the newest wording; the standing problem may have
				// changed shape since it was first seen.
				g.summary, g.why = f.Summary, f.WhyItMatters
			}
			if severityRank(f.Severity) > severityRank(g.severity) {
				g.severity = f.Severity
			}
		}
	}
	out := make([]findingGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].severity) != severityRank(out[j].severity) {
			return severityRank(out[i].severity) > severityRank(out[j].severity)
		}
		return out[i].count > out[j].count
	})
	return out
}

// evidenceKey normalizes a quote enough that the same log line groups across
// passes despite the model trimming it differently, without merging genuinely
// different lines: digits are folded (ids, byte counts, timestamps vary run to
// run) and the distinctive head is what keys.
func evidenceKey(evidence string) string {
	folded := digitRun.ReplaceAllString(strings.TrimSpace(evidence), "N")
	r := []rune(folded)
	if len(r) > 80 {
		r = r[:80]
	}
	return string(r)
}

var digitRun = regexp.MustCompile(`[0-9]+`)

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

// shortStamp trims an RFC3339 stamp to what a reader scanning a day actually
// reads.
func shortStamp(at string) string {
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("01-02 15:04")
	}
	return at
}

// describeCoverage states what the pass actually saw, not what it asked for.
// A pass over a truncated window reporting only its line count reads as a quiet
// runtime, which is the opposite of what it means.
func describeCoverage(e anomalywatch.Entry) string {
	if e.Examined.Partial {
		return fmt.Sprintf("%d줄 · 창 %d분 중 %d분만 관측(재시작)",
			e.Examined.LogLines, e.WindowMinutes, e.Examined.CoveredMinutes)
	}
	return fmt.Sprintf("%d줄 · %d분 창", e.Examined.LogLines, e.WindowMinutes)
}

// partialCoverage returns the minutes each truncated pass actually saw.
func partialCoverage(entries []anomalywatch.Entry) []int {
	var out []int
	for _, e := range entries {
		if e.Examined.Partial {
			out = append(out, e.Examined.CoveredMinutes)
		}
	}
	return out
}

// windowMinutesOf reports the window the passes asked for, so the shortfall is
// legible as a ratio rather than a bare number.
func windowMinutesOf(entries []anomalywatch.Entry) int {
	for _, e := range entries {
		if e.WindowMinutes > 0 {
			return e.WindowMinutes
		}
	}
	return 0
}

func joinInts(v []int) string {
	parts := make([]string, 0, len(v))
	for _, n := range v {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, "·")
}

// renderAnomalyDigest collapses passes into the reading a person wants.
//
// Passes with nothing to report are counted rather than listed: they are what
// makes the ledger trustworthy (they prove the lane ran), but printing 24 of
// them would bury the one pass that found something.
func renderAnomalyDigest(entries []anomalywatch.Entry, window time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## anomaly-watch — 최근 %s\n", window)
	if len(entries) == 0 {
		// Distinguishing these two is the whole discipline of this lane: an
		// empty ledger means the WATCHER was silent, which is never the same
		// news as a quiet runtime.
		b.WriteString("점검 기록이 없다. 창이 조용했던 게 아니라 **감시 레인이 돌지 않았다**는 뜻이다 — anomaly-watch 등록 여부를 확인할 것.\n")
		return b.String()
	}
	var clean, gaps int
	var withFindings []anomalywatch.Entry
	for _, e := range entries {
		switch {
		case e.Gap != "":
			gaps++
		case len(e.Findings) == 0:
			clean++
		default:
			withFindings = append(withFindings, e)
		}
	}
	fmt.Fprintf(&b, "점검 %d회 · 이상 %d회 · 무소견 %d회 · 판정불가 %d회\n",
		len(entries), len(withFindings), clean, gaps)
	// A run of truncated windows is its own finding: it means the gateway is
	// restarting often enough that the watch rarely sees a full window, so a
	// stretch of clean passes is much weaker evidence than its count suggests.
	// Coverage is stated on the summary line rather than per pass, because the
	// findings below are collapsed across passes and no longer have a pass to
	// hang it on. It must stay explicit somewhere: "N줄 검사" over a window the
	// process was not alive for reads as a quiet runtime, which is the opposite
	// of what it means.
	if partials := partialCoverage(entries); len(partials) > 0 {
		fmt.Fprintf(&b, "⚠ 그중 %d회는 **창이 잘렸다** — 창 %d분 중 %s분만 관측(재시작). 무소견을 정상으로 읽지 말 것\n",
			len(partials), windowMinutesOf(entries), joinInts(partials))
	}
	b.WriteString("\n")

	if len(withFindings) == 0 {
		b.WriteString("이상 관측 없음.\n")
	} else {
		// Collapsed by evidence, not listed per pass. An hourly lane re-reports
		// a standing problem every hour, and over a day that buried three
		// distinct defects under seven copies of the loudest one. What a reader
		// needs is the DISTINCT set plus how long each has stood.
		for _, g := range collapseFindings(withFindings) {
			fmt.Fprintf(&b, "- **[%s]** %s", strings.ToUpper(g.severity), g.summary)
			if g.count > 1 {
				fmt.Fprintf(&b, " · **%d회** (%s ~ %s)", g.count, shortStamp(g.first), shortStamp(g.last))
			} else {
				fmt.Fprintf(&b, " · %s", shortStamp(g.last))
			}
			b.WriteString("\n")
			// The quote stays inline: it is what makes dismissing a false
			// positive cost seconds instead of an investigation.
			fmt.Fprintf(&b, "  > `%s`\n", g.evidence)
			if g.why != "" {
				fmt.Fprintf(&b, "  %s\n", g.why)
			}
		}
		b.WriteString("\n")
	}
	if gaps > 0 {
		b.WriteString("### 판정불가\n")
		for _, e := range entries {
			if e.Gap != "" {
				fmt.Fprintf(&b, "- %s — %s\n", e.At, e.Gap)
			}
		}
	}
	return b.String()
}
