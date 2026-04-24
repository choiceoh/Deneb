// render.go — Telegram MarkdownV2 and plain-text rendering for Report.
//
// MarkdownV2 reserved characters: _ * [ ] ( ) ~ ` > # + - = | { } . !
// We always wrap tabular data in ```-fenced code blocks where escaping is not
// required (Telegram treats code-block contents literally). Labels outside the
// code block are escaped via escapeMDv2.
//
// Telegram's hard cap is 4096 chars per message — we cap at 3800 to leave
// headroom for the caller's chunk prefix, emoji width and MarkdownV2 escape
// overhead. If a report would exceed the cap we trim sections and append a
// "…" marker; the renderer never returns something longer than the limit.

package insights

import (
	"fmt"
	"strings"
)

// maxTelegramBody is the soft limit for rendered MarkdownV2 output.
// Telegram's hard limit is 4096; we stay conservative to account for
// downstream wrapping and emoji char width.
const maxTelegramBody = 3800

// RenderMarkdownV2 renders a report as Telegram-safe MarkdownV2.
// Labels are escaped; tabular data lives in code fences (no escaping needed).
// The output is guaranteed to be <= maxTelegramBody characters.
func RenderMarkdownV2(r *Report) string {
	if r == nil {
		return escapeMDv2("리포트 생성 실패.")
	}

	var b strings.Builder
	b.Grow(1024)

	// Header.
	b.WriteString("*")
	b.WriteString(escapeMDv2(fmt.Sprintf("📊 사용 리포트 — 최근 %d일", r.Days)))
	b.WriteString("*")
	b.WriteByte('\n')
	b.WriteString(escapeMDv2(fmt.Sprintf("기준 시각: %s", r.GeneratedAt.Format("2006-01-02 15:04"))))
	b.WriteByte('\n')

	if r.Empty {
		b.WriteString(escapeMDv2("집계 대상 세션이 없습니다."))
		appendSchemaNotes(&b, r.SchemaNotes)
		return capBody(b.String())
	}

	// Overview — labels are escaped; values go into a code block.
	b.WriteString("\n*")
	b.WriteString(escapeMDv2("요약"))
	b.WriteString("*\n")
	b.WriteString("```\n")
	fmt.Fprintf(&b, "총 세션 : %d (진행 중 %d)\n", r.Overview.Sessions, r.Overview.ActiveNow)
	fmt.Fprintf(&b, "입력 토큰: %s\n", formatCount(r.Overview.InputTokens))
	fmt.Fprintf(&b, "출력 토큰: %s\n", formatCount(r.Overview.OutputTokens))
	fmt.Fprintf(&b, "총 토큰  : %s\n", formatCount(r.Overview.TotalTokens))
	if r.Overview.CostUSD > 0 {
		fmt.Fprintf(&b, "예상 비용: $%.4f\n", r.Overview.CostUSD)
	}
	b.WriteString("```\n")

	// Top models.
	if len(r.Models) > 0 {
		b.WriteString("\n*")
		b.WriteString(escapeMDv2("상위 모델"))
		b.WriteString("*\n")
		b.WriteString("```\n")
		for _, m := range r.Models {
			fmt.Fprintf(&b, "%-24s %3d세션  %s\n",
				truncate(m.Model, 24),
				m.Sessions,
				formatCount(m.TotalTokens),
			)
		}
		b.WriteString("```\n")
	}

	// Top tools (only if aggregator wired — otherwise schemaNotes explains).
	if len(r.Tools) > 0 {
		limit := len(r.Tools)
		if limit > 10 {
			limit = 10
		}
		b.WriteString("\n*")
		b.WriteString(escapeMDv2("상위 도구"))
		b.WriteString("*\n")
		b.WriteString("```\n")
		for i := range limit {
			t := r.Tools[i]
			line := fmt.Sprintf("%-20s %4d회", truncate(t.Name, 20), t.Calls)
			if t.ErrorRate > 0 {
				line += fmt.Sprintf("  에러 %.0f%%", t.ErrorRate*100)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("```\n")
	}

	// Top sessions.
	if len(r.TopSessions) > 0 {
		b.WriteString("\n*")
		b.WriteString(escapeMDv2("상위 세션"))
		b.WriteString("*\n")
		b.WriteString("```\n")
		for _, s := range r.TopSessions {
			label := s.Key
			if s.Channel != "" {
				label = fmt.Sprintf("%s[%s]", s.Key, s.Channel)
			}
			fmt.Fprintf(&b, "%-26s %s\n",
				truncate(label, 26),
				formatCount(s.TotalTokens),
			)
		}
		b.WriteString("```\n")
	}

	// Providers (since-restart totals from usage tracker).
	if len(r.Providers) > 0 {
		b.WriteString("\n*")
		b.WriteString(escapeMDv2("프로바이더 (재시작 이후)"))
		b.WriteString("*\n")
		b.WriteString("```\n")
		for _, p := range r.Providers {
			fmt.Fprintf(&b, "%-18s %5d호출  in %s / out %s\n",
				truncate(p.Provider, 18),
				p.Calls,
				formatCount(p.Input),
				formatCount(p.Output),
			)
		}
		b.WriteString("```\n")
	}

	appendSchemaNotes(&b, r.SchemaNotes)
	return capBody(b.String())
}

// RenderPlain renders a report as plain text (for CLI, logs, smoke tests).
// No MarkdownV2 escaping; no length cap.
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
			fmt.Fprintf(&b, "  %-20s %4d회\n", truncate(t.Name, 20), t.Calls)
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

func appendSchemaNotes(b *strings.Builder, notes []string) {
	if len(notes) == 0 {
		return
	}
	b.WriteString("\n_")
	b.WriteString(escapeMDv2("참고"))
	b.WriteString("_\n")
	for _, n := range notes {
		b.WriteString(escapeMDv2("· "))
		b.WriteString(escapeMDv2(n))
		b.WriteByte('\n')
	}
}

// capBody truncates rendered text to maxTelegramBody with a truncation marker.
// It prefers to cut at a newline to avoid slicing through a code fence.
func capBody(s string) string {
	if len(s) <= maxTelegramBody {
		return s
	}
	cut := maxTelegramBody - 32
	if cut < 0 {
		cut = 0
	}
	// Back off to the previous newline so we don't slice inside a code block.
	if idx := strings.LastIndexByte(s[:cut], '\n'); idx >= 0 {
		cut = idx
	}
	trimmed := s[:cut]
	// If we sliced inside an odd number of ``` fences, close the fence.
	if strings.Count(trimmed, "```")%2 == 1 {
		trimmed += "\n```"
	}
	trimmed += "\n" + escapeMDv2("… (길이 제한으로 일부만 표시)")
	return trimmed
}

// mdv2Special are the chars that MUST be escaped in MarkdownV2 text.
// Source: https://core.telegram.org/bots/api#markdownv2-style
const mdv2Special = "_*[]()~`>#+-=|{}.!\\"

// escapeMDv2 escapes every MarkdownV2 reserved character with a leading backslash.
// Use this for labels that appear OUTSIDE a code block.
func escapeMDv2(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range s {
		if strings.ContainsRune(mdv2Special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
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
