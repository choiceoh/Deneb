package server

import (
	"fmt"
	"net/http"
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

func countPartial(entries []anomalywatch.Entry) int {
	n := 0
	for _, e := range entries {
		if e.Examined.Partial {
			n++
		}
	}
	return n
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
	if partial := countPartial(entries); partial > 0 {
		fmt.Fprintf(&b, "⚠ 그중 %d회는 **창이 잘렸다**(재시작으로 로그 링이 비어 있었음) — 무소견을 정상으로 읽지 말 것\n", partial)
	}
	b.WriteString("\n")

	if len(withFindings) == 0 {
		b.WriteString("이상 관측 없음.\n")
	}
	for _, e := range withFindings {
		fmt.Fprintf(&b, "### %s (%s)\n", e.At, describeCoverage(e))
		for _, f := range e.Findings {
			fmt.Fprintf(&b, "- **[%s]** %s\n", strings.ToUpper(f.Severity), f.Summary)
			// The quote is indented as a block so the reader can confirm or
			// dismiss without leaving the page — the property that makes a
			// false positive cost seconds instead of an investigation.
			fmt.Fprintf(&b, "  > `%s`\n", f.Evidence)
			if f.WhyItMatters != "" {
				fmt.Fprintf(&b, "  %s\n", f.WhyItMatters)
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
