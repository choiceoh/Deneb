// render.go — plain-text rendering for Report.
// Consumed by the insights RPC handler (insights.generate) as the
// human-readable companion to the structured Report.

package insights

import (
	"fmt"
	"strings"
)

// RenderPlain renders a report as plain text (for CLI, logs, smoke tests).
// No length cap.
func RenderPlain(r *Report) string {
	if r == nil {
		return "리포트 생성 실패.\n"
	}
	var b strings.Builder
	b.Grow(1024)

	fmt.Fprintf(&b, "📊 사용 리포트 — 최근 %d일\n", r.Days)
	fmt.Fprintf(&b, "기준 시각: %s\n", r.GeneratedAt.Format("2006-01-02 15:04 MST"))

	if r.Empty {
		b.WriteString("집계 대상 세션이 없습니다.\n")
		for _, note := range r.SchemaNotes {
			fmt.Fprintf(&b, "· %s\n", note)
		}
		return b.String()
	}

	b.WriteString("\n[요약]\n")
	fmt.Fprintf(&b, "  총 세션 : %d (진행 중 %d)\n", r.Overview.Sessions, r.Overview.ActiveNow)
	fmt.Fprintf(&b, "  입력 토큰: %s\n", formatCount(r.Overview.InputTokens))
	fmt.Fprintf(&b, "  출력 토큰: %s\n", formatCount(r.Overview.OutputTokens))
	fmt.Fprintf(&b, "  총 토큰  : %s\n", formatCount(r.Overview.TotalTokens))
	if r.Overview.CostUSD > 0 {
		fmt.Fprintf(&b, "  예상 비용: $%.4f\n", r.Overview.CostUSD)
	}

	if len(r.Models) > 0 {
		b.WriteString("\n[상위 모델]\n")
		for _, m := range r.Models {
			fmt.Fprintf(&b, "  %-24s %3d세션  %s\n",
				truncate(m.Model, 24), m.Sessions, formatCount(m.TotalTokens))
		}
	}

	if len(r.Tools) > 0 {
		b.WriteString("\n[상위 도구]\n")
		for i, t := range r.Tools {
			if i >= 10 {
				break
			}
			line := fmt.Sprintf("  %-20s %4d회", truncate(t.Name, 20), t.Calls)
			if t.AvgMs > 0 {
				line += fmt.Sprintf("  평균 %dms", t.AvgMs)
			}
			if t.ErrorRate > 0 {
				line += fmt.Sprintf("  에러 %.0f%%", t.ErrorRate*100)
			}
			b.WriteString(line + "\n")
		}
	}

	if len(r.TopSessions) > 0 {
		b.WriteString("\n[상위 세션]\n")
		for _, s := range r.TopSessions {
			fmt.Fprintf(&b, "  %-26s %s\n", truncate(s.Key, 26), formatCount(s.TotalTokens))
		}
	}

	if len(r.Providers) > 0 {
		b.WriteString("\n[프로바이더 (재시작 이후)]\n")
		for _, p := range r.Providers {
			fmt.Fprintf(&b, "  %-18s %5d호출  in %s / out %s\n",
				truncate(p.Provider, 18), p.Calls,
				formatCount(p.Input), formatCount(p.Output))
		}
	}

	if len(r.SchemaNotes) > 0 {
		b.WriteString("\n[참고]\n")
		for _, n := range r.SchemaNotes {
			fmt.Fprintf(&b, "  · %s\n", n)
		}
	}
	return b.String()
}

// formatCount pretty-prints large integers (1.2M, 3.4K, 567).
func formatCount(n int64) string {
	if n < 0 {
		return "-" + formatCount(-n)
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// truncate cuts a string to n runes, appending "…" if cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(runes[:n-1]) + "…"
}
