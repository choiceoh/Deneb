// weekly_card.go — server-assembled deneb-ui card for the 주간업무보고.
//
// The weekly report is fully gateway-composed (collectWeekly) with no LLM in
// the loop, so unlike the morning letter there is nothing for a model to
// transcribe or hallucinate: this file renders the envelope straight into the
// labeled-HTML card the native/Andromeda clients display in the 업무 feed. The
// formal grid form (PNG/PDF, weekly_report.go) keeps shipping alongside — the
// card is the glanceable in-feed body, the form stays the submission artifact.
package tools

import (
	"fmt"
	"strings"
	"time"
)

// RenderWeeklyReportCard composes the delivery-ready weekly report message: a
// one-line plain head (push preview and parse-failure safety net, mirroring
// the morning letter's shape) followed by a deneb-ui card fence.
func RenderWeeklyReportCard(opts WeeklyReportOpts, now time.Time) string {
	return composeWeeklyCard(collectWeekly(opts, now))
}

// Escapers mirror denebui's server-assembly rules (tools must not import the
// chat subtree): attribute values get the quoted-attr escape; element/markdown
// raw text escapes '<'/'&' plus backtick, so an inner code fence can never
// terminate the outer ```deneb-ui fence early.
var (
	weeklyAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	weeklyRawEscaper  = strings.NewReplacer("&", "&amp;", "<", "&lt;", "`", "&#96;")
)

func composeWeeklyCard(env weeklyEnvelope) string {
	projects := 0
	for _, g := range env.Groups {
		projects += len(g.Projects)
	}

	var b strings.Builder
	// Head line outside the fence: notification/push preview, and what the user
	// still reads if a client ever fails to parse the card.
	fmt.Fprintf(&b, "📋 주간업무보고 — 실시 %s · 예정 %s\n\n", env.WeekDone, env.WeekPlanned)

	b.WriteString("```deneb-ui\n<column>\n")
	fmt.Fprintf(&b, "  <text style=\"headline\">주간업무보고 · %s</text>\n", weeklyRawEscaper.Replace(env.Office))
	fmt.Fprintf(&b, "  <text style=\"caption\">실시 %s · 예정 %s · 보고자 %s</text>\n",
		env.WeekDone, env.WeekPlanned, weeklyRawEscaper.Replace(env.Reporter))
	b.WriteString("  <hr/>\n")

	if projects == 0 {
		b.WriteString("  <text style=\"body\">이번 주 보고할 프로젝트 활동이 없습니다.</text>\n")
	} else {
		fmt.Fprintf(&b, "  <row><stat value=\"%d건\" label=\"진행 프로젝트\"/><stat value=\"%d건\" label=\"현안\"/></row>\n",
			projects, len(env.IssueRows))
		for _, g := range env.Groups {
			title := fmt.Sprintf("%s · %d건", g.Label, len(g.Projects))
			fmt.Fprintf(&b, "  <accordion title=\"%s\">\n<markdown>", weeklyAttrEscaper.Replace(title))
			for i, p := range g.Projects {
				if i > 0 {
					b.WriteString("\n\n")
				}
				name := p.Title
				if p.Capacity != "" {
					name += "(" + p.Capacity + ")"
				}
				fmt.Fprintf(&b, "**%s**\n- 실시: %s\n- 예정: %s",
					weeklyRawEscaper.Replace(name),
					weeklyRawEscaper.Replace(p.DoneLine),
					weeklyRawEscaper.Replace(p.PlannedLine))
			}
			b.WriteString("</markdown>\n  </accordion>\n")
		}
	}

	if len(env.IssueRows) > 0 {
		b.WriteString("  <card>\n    <row><icon name=\"alarm\" size=\"16\"/><text style=\"caption\">현안</text></row>\n")
		for _, is := range env.IssueRows {
			// Urgency tint mirrors the letter's 임박 마감 contract: 기한 초과·D-day
			// carry the error tint, the D-1~3 window stays warning.
			color := "warning"
			if is.Overdue {
				color = "error"
			}
			fmt.Fprintf(&b, "    <row><text style=\"body\">%s</text><badge color=\"%s\">%s</badge></row>\n",
				weeklyRawEscaper.Replace(is.Title), color, weeklyRawEscaper.Replace(is.Badge))
		}
		b.WriteString("  </card>\n")
	}

	b.WriteString("</column>\n```")
	return b.String()
}
