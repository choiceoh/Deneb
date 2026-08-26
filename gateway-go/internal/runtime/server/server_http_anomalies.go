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
	fmt.Fprintf(&b, "점검 %d회 · 이상 %d회 · 무소견 %d회 · 판정불가 %d회\n\n",
		len(entries), len(withFindings), clean, gaps)

	if len(withFindings) == 0 {
		b.WriteString("이상 관측 없음.\n")
	}
	for _, e := range withFindings {
		fmt.Fprintf(&b, "### %s (%d줄 검사)\n", e.At, e.Examined.LogLines)
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
