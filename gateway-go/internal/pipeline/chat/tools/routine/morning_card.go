package routine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const morningLetterNote = "delivery is the complete, authoritative Korean message; return it verbatim. sections are inspection-only and must not be re-fetched or reformatted"

type morningLetterEnvelope struct {
	Date      string                `json:"date"`
	Timestamp string                `json:"timestamp"`
	Note      string                `json:"note,omitempty"`
	Delivery  string                `json:"delivery,omitempty"`
	Sections  morningLetterSections `json:"sections"`
}

type morningLetterSections struct {
	Weather          weatherData               `json:"weather"`
	Exchange         exchangeData              `json:"exchange"`
	Copper           copperData                `json:"copper"`
	Calendar         calendarData              `json:"calendar"`
	Deadlines        deadlineData              `json:"deadlines"`
	ProjectSignals   morningProjectSignalsData `json:"project_signals"`
	Email            emailData                 `json:"email"`
	OpenQuestions    openQuestionsData         `json:"open_questions"`
	GroupwarePending groupwarePendingData      `json:"groupware_pending"`
	GroupwareCC      groupwareCCData           `json:"groupware_cc"`
}

func collectMorningLetter(ctx context.Context, opts MorningLetterOpts, now time.Time) (morningLetterEnvelope, []any) {
	groupwareCollector := opts.GroupwareCollector
	if groupwareCollector == nil {
		groupwareCollector = fetchGroupwarePending
	}
	groupwareCCCollector := opts.GroupwareCCCollector
	if groupwareCCCollector == nil {
		groupwareCCCollector = fetchGroupwareCC
	}

	collectors := []letterCollector{
		{0, func(ctx context.Context) any { return fetchWeather(ctx) }},
		{1, func(ctx context.Context) any { return fetchExchangeRates(ctx) }},
		{2, func(ctx context.Context) any { return fetchCopper(ctx) }},
		{3, func(ctx context.Context) any { return fetchCalendar(ctx) }},
		{4, func(ctx context.Context) any { return fetchEmail(ctx) }},
		{5, func(_ context.Context) any { return fetchDeadlines(opts.WikiDir, now) }},
		{6, func(_ context.Context) any { return fetchOpenQuestions(opts.WikiDir, now) }},
		{7, groupwareCollector},
		{8, groupwareCCCollector},
		{9, func(_ context.Context) any { return fetchMorningProjectSignals(opts.WikiDir, now) }},
	}
	results := collectLetterSections(ctx, 10, collectors)
	return morningLetterEnvelope{
		Date:      koreanDate(now),
		Timestamp: now.Format(time.RFC3339),
		Note:      morningLetterNote,
		Sections: morningLetterSections{
			Weather:          letterSection[weatherData](results[0]),
			Exchange:         letterSection[exchangeData](results[1]),
			Copper:           letterSection[copperData](results[2]),
			Calendar:         letterSection[calendarData](results[3]),
			Email:            letterSection[emailData](results[4]),
			Deadlines:        letterSection[deadlineData](results[5]),
			OpenQuestions:    letterSection[openQuestionsData](results[6]),
			GroupwarePending: letterSection[groupwarePendingData](results[7]),
			GroupwareCC:      letterSection[groupwareCCData](results[8]),
			ProjectSignals:   letterSection[morningProjectSignalsData](results[9]),
		},
	}, results
}

func letterSection[T any](value any) T {
	section, _ := value.(T)
	return section
}

// Escapers mirror the deterministic weekly-card boundary. Raw text escapes
// '<', '&', and backticks so external mail/wiki text cannot create a tag or
// terminate the surrounding deneb-ui fence. Attribute values also escape quotes.
var (
	morningAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	morningRawEscaper  = strings.NewReplacer("&", "&amp;", "<", "&lt;", "`", "&#96;")
)

func composeMorningLetterCard(env morningLetterEnvelope, now time.Time) string {
	return composeMorningLetterCardWithNarrative(env, morningLetterNarrative{}, now)
}

func composeMorningLetterCardWithNarrative(env morningLetterEnvelope, narrative morningLetterNarrative, now time.Time) string {
	sections := env.Sections
	summary := narrative.Headline
	if summary == "" {
		summary = morningLetterSummary(sections.Weather, sections.Deadlines, sections.OpenQuestions, sections.GroupwarePending)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "좋은 아침이에요 — %s. %s\n\n", morningPlain(env.Date, 40), morningPlain(summary, 90))
	b.WriteString("```deneb-ui\n<column>\n")
	fmt.Fprintf(&b, "  <text style=\"headline\">%s</text>\n", morningRaw(env.Date))
	fmt.Fprintf(&b, "  <text style=\"caption\">%s</text>\n", morningRaw(summary))
	b.WriteString("  <hr/>\n")
	writeMorningTodayFocus(&b, sections.Calendar, sections.Deadlines, sections.GroupwarePending, now)
	writeMorningWeather(&b, sections.Weather, narrative.WeatherNote)
	writeMorningMarket(&b, sections.Exchange, sections.Copper, now)
	writeMorningCalendar(&b, sections.Calendar)
	writeMorningDeadlines(&b, sections.Deadlines)
	if len(narrative.Projects) > 0 {
		writeMorningNarrativeProjects(&b, narrative.Projects)
	} else {
		writeMorningProjects(&b, sections.ProjectSignals)
		writeMorningEmail(&b, sections.Email)
	}
	writeMorningQuoteGroups(&b, sections.Email)
	writeMorningQuestions(&b, sections.OpenQuestions)
	writeMorningPending(&b, sections.GroupwarePending)
	writeMorningCC(&b, sections.GroupwareCC)
	if len(narrative.Risks) > 0 || len(narrative.Suggestions) > 0 {
		writeMorningNarrativeActions(&b, narrative)
	} else {
		writeMorningActions(&b, sections.Weather, sections.Deadlines, sections.OpenQuestions, sections.GroupwarePending)
	}
	b.WriteString("</column>\n```")
	return b.String()
}

type morningProjectSignalsData struct {
	OK    bool                   `json:"ok"`
	Items []morningProjectSignal `json:"items,omitempty"`
}

type morningProjectSignal struct {
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Updated     string `json:"updated,omitempty"`
	Due         string `json:"due,omitempty"`
	DoneLine    string `json:"recent_progress,omitempty"`
	PlannedLine string `json:"next_action,omitempty"`
}

func fetchMorningProjectSignals(wikiDir string, now time.Time) any {
	if wikiDir == "" {
		return morningProjectSignalsData{OK: true}
	}
	weekly := collectWeekly(WeeklyReportOpts{WikiDir: wikiDir}, now)
	projects := make([]weeklyProject, 0, 5)
	for _, group := range weekly.Groups {
		projects = append(projects, group.Projects...)
	}
	slices.SortStableFunc(projects, func(a, b weeklyProject) int {
		return strings.Compare(b.Updated, a.Updated)
	})
	if len(projects) > 5 {
		projects = projects[:5]
	}
	items := make([]morningProjectSignal, 0, len(projects))
	for _, project := range projects {
		items = append(items, morningProjectSignal{
			Title: project.Title, Summary: project.Summary, Updated: project.Updated, Due: project.Due,
			DoneLine: project.DoneLine, PlannedLine: project.PlannedLine,
		})
	}
	return morningProjectSignalsData{OK: true, Items: items}
}

type morningLetterNarrative struct {
	Headline    string                    `json:"headline"`
	WeatherNote string                    `json:"weather_note,omitempty"`
	Projects    []morningProjectNarrative `json:"projects,omitempty"`
	Risks       []string                  `json:"risks,omitempty"`
	Suggestions []string                  `json:"suggestions,omitempty"`
}

type morningProjectNarrative struct {
	Title        string `json:"title"`
	Priority     string `json:"priority"`
	WhatHappened string `json:"what_happened"`
	WhyImportant string `json:"why_important"`
	NextAction   string `json:"next_action"`
}

// RenderMorningLetterCard applies a model-authored semantic slot object to the
// fixed server-side card. Invalid narrative JSON degrades to the deterministic
// facts-only projection; invalid collected data is the only hard failure.
func RenderMorningLetterCard(dataJSON, narrativeJSON string, now time.Time) (string, error) {
	var env morningLetterEnvelope
	if err := json.Unmarshal([]byte(dataJSON), &env); err != nil {
		return "", fmt.Errorf("decode morning letter data: %w", err)
	}
	narrative, _ := parseMorningLetterNarrative(narrativeJSON)
	return composeMorningLetterCardWithNarrative(env, narrative, now.In(kstLocation)), nil
}

func parseMorningLetterNarrative(raw string) (morningLetterNarrative, error) {
	raw = strings.TrimSpace(raw)
	if start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}"); start >= 0 && end >= start {
		raw = raw[start : end+1]
	}
	var narrative morningLetterNarrative
	if err := json.Unmarshal([]byte(raw), &narrative); err != nil {
		return morningLetterNarrative{}, err
	}
	narrative.Headline = morningPlain(narrative.Headline, 90)
	narrative.WeatherNote = morningPlain(narrative.WeatherNote, 120)
	if len(narrative.Projects) > 5 {
		narrative.Projects = narrative.Projects[:5]
	}
	projects := narrative.Projects[:0]
	for i := range narrative.Projects {
		project := &narrative.Projects[i]
		project.Title = morningPlain(project.Title, 60)
		if project.Title == "" {
			continue
		}
		project.Priority = normalizeMorningPriority(project.Priority)
		project.WhatHappened = morningPlain(project.WhatHappened, 180)
		project.WhyImportant = morningPlain(project.WhyImportant, 180)
		project.NextAction = morningPlain(project.NextAction, 180)
		projects = append(projects, *project)
	}
	narrative.Projects = projects
	narrative.Risks = normalizeMorningLines(narrative.Risks, 4)
	narrative.Suggestions = normalizeMorningLines(narrative.Suggestions, 4)
	return narrative, nil
}

func morningLetterSummary(weather weatherData, deadlines deadlineData, questions openQuestionsData, pending groupwarePendingData) string {
	for _, item := range deadlines.Items {
		switch {
		case item.DaysLeft < 0:
			return "🔴 " + morningPlain(item.Title, 48) + " 기한 초과를 먼저 확인하세요."
		case item.DaysLeft == 0:
			return "🔴 " + morningPlain(item.Title, 48) + " 오늘 마감입니다."
		}
	}
	if pending.StaleCount > 0 {
		return fmt.Sprintf("🟠 방치된 전자결재 %d건을 먼저 처리하세요.", pending.StaleCount)
	}
	for _, item := range deadlines.Items {
		if item.DaysLeft <= 3 {
			return fmt.Sprintf("🟠 %s D-%d입니다.", morningPlain(item.Title, 48), item.DaysLeft)
		}
	}
	if weather.OK && weather.MaxRainPct >= 40 {
		return fmt.Sprintf("비 가능성 %d%% — 우산을 챙기세요.", weather.MaxRainPct)
	}
	if len(questions.Items) > 0 {
		return fmt.Sprintf("🟡 오래 열린 확인사항 %d건을 정리했습니다.", len(questions.Items))
	}
	return "오늘 일정과 업무 신호를 한 번에 정리했습니다."
}

func writeMorningWeather(b *strings.Builder, weather weatherData, modelNote string) {
	b.WriteString("  <card>\n")
	fmt.Fprintf(b, "    <row><icon name=\"%s\" size=\"16\"/><text style=\"caption\">날씨 · 광주</text></row>\n", weatherIcon(weather.Condition))
	if !weather.OK {
		b.WriteString("    <text style=\"body\">조회 실패</text>\n  </card>\n")
		return
	}
	fmt.Fprintf(b, "    <row><text style=\"headline\">%s°</text><text style=\"caption\">체감 %s°</text></row>\n", morningRaw(weather.TempC), morningRaw(weather.FeelsLikeC))
	fmt.Fprintf(b, "    <text style=\"caption\">최고 %s° · 최저 %s° · 강수 %d%%</text>\n", morningRaw(weather.MaxTempC), morningRaw(weather.MinTempC), weather.MaxRainPct)
	detail := modelNote
	if detail == "" {
		detail = strings.TrimSpace(weather.Condition)
		if detail == "" {
			detail = "상태 미확인"
		}
		if weather.Humidity != "" {
			detail += " · 습도 " + weather.Humidity + "%"
		}
		if weather.MaxRainPct >= 40 {
			detail += " · 우산 권장"
		}
	}
	if detail = strings.Trim(detail, " ·"); detail != "" {
		fmt.Fprintf(b, "    <text style=\"body\">%s</text>\n", morningRaw(detail))
	}
	b.WriteString("  </card>\n")
}

func writeMorningMarket(b *strings.Builder, exchange exchangeData, copper copperData, now time.Time) {
	b.WriteString("  <card>\n    <row><icon name=\"payments\" size=\"16\"/><text style=\"caption\">시장 · 달러와 구리</text></row>\n    <row>")
	written := 0
	if exchange.OK && exchange.USDKRW > 0 {
		fmt.Fprintf(b, "<stat value=\"%s\" label=\"USD/KRW\"/>", morningAttr(formatGroupedInt(exchange.USDKRW)))
		written++
	}
	if copper.OK && copper.PricePerTon > 0 {
		value := copper.Display
		if value == "" {
			value = formatGroupedInt(copper.PricePerTon)
		}
		value += " /t"
		if copper.Date != "" && copper.Date != now.Format("2006-01-02") {
			value += " (" + copper.Date + ")"
		}
		fmt.Fprintf(b, "<stat value=\"%s\" label=\"LME 구리\"/>", morningAttr(value))
		written++
	}
	if written == 0 {
		b.WriteString("<text style=\"body\">조회 실패</text>")
	}
	b.WriteString("</row>\n  </card>\n")
}

func writeMorningCalendar(b *strings.Builder, calendar calendarData) {
	b.WriteString("  <card>\n    <row><icon name=\"calendar\" size=\"16\"/><text style=\"caption\">오늘·내일 일정</text></row>\n")
	switch {
	case !calendar.OK:
		b.WriteString("    <text style=\"body\">조회 실패</text>\n")
	case len(calendar.Events) == 0:
		b.WriteString("    <text style=\"body\">일정 없음</text>\n")
	default:
		writeMorningList(b, calendar.Events, 8)
	}
	b.WriteString("  </card>\n")
}

func writeMorningEmail(b *strings.Builder, email emailData) {
	b.WriteString("  <card>\n    <row><icon name=\"mail\" size=\"16\"/><text style=\"caption\">새 메일 신호</text></row>\n")
	switch {
	case !email.OK:
		b.WriteString("    <text style=\"body\">조회 실패</text>\n")
	case len(email.Messages) == 0:
		b.WriteString("    <text style=\"body\">새 메일 없음</text>\n")
	default:
		items := make([]string, 0, min(len(email.Messages), 5))
		for _, msg := range email.Messages {
			items = append(items, "🔵 "+emailSenderName(msg.From)+" — "+morningPlain(msg.Subject, 90))
			if len(items) == 5 {
				break
			}
		}
		writeMorningList(b, items, 5)
	}
	b.WriteString("  </card>\n")
}

func writeMorningDeadlines(b *strings.Builder, deadlines deadlineData) {
	if len(deadlines.Items) == 0 {
		return
	}
	b.WriteString("  <card>\n    <row><icon name=\"alarm\" size=\"16\"/><text style=\"caption\">임박 마감</text></row>\n")
	for i, item := range deadlines.Items {
		if i == 6 {
			break
		}
		label, color, marker := deadlinePresentation(item.DaysLeft)
		// longpress="deadline_done" (+ data-path): long-press the row in the feed
		// card to mark this deadline handled — the relay derives a matching
		// work-feed action and the gateway stamps the wiki page's due_done.
		// data-path carries the wiki rel path; empty path stays non-actionable.
		lp := ""
		if p := strings.TrimSpace(item.Path); p != "" {
			lp = fmt.Sprintf(" longpress=\"deadline_done\" data-path=\"%s\"", morningAttr(p))
		}
		fmt.Fprintf(b, "    <row%s><text style=\"body\">%s %s</text><badge%s>%s</badge></row>\n",
			lp, marker, morningRaw(item.Title), color, morningRaw(label))
	}
	b.WriteString("  </card>\n")
}

func writeMorningProjects(b *strings.Builder, projects morningProjectSignalsData) {
	if len(projects.Items) == 0 {
		return
	}
	b.WriteString("  <card>\n    <row><icon name=\"work\" size=\"16\"/><text style=\"caption\">최근 프로젝트 맥락</text></row>\n    <ul>")
	for _, item := range projects.Items {
		line := "🔵 " + item.Title
		if item.DoneLine != "" {
			line += " — " + item.DoneLine
		}
		if item.PlannedLine != "" {
			line += " · 다음: " + item.PlannedLine
		}
		fmt.Fprintf(b, "<li>%s</li>", morningRaw(line))
	}
	b.WriteString("</ul>\n  </card>\n")
}

func writeMorningNarrativeProjects(b *strings.Builder, projects []morningProjectNarrative) {
	for _, project := range projects {
		if project.Title == "" {
			continue
		}
		marker, color, label := morningPriorityPresentation(project.Priority)
		b.WriteString("  <card>\n")
		fmt.Fprintf(b, "    <row><icon name=\"work\" size=\"16\"/><text style=\"headline\">%s %s</text><badge%s>%s</badge></row>\n",
			marker, morningRaw(project.Title), color, label)
		if project.WhatHappened != "" {
			fmt.Fprintf(b, "    <text style=\"body\">%s</text>\n", morningRaw(project.WhatHappened))
		}
		if project.WhyImportant != "" {
			fmt.Fprintf(b, "    <text style=\"caption\">중요: %s</text>\n", morningRaw(project.WhyImportant))
		}
		if project.NextAction != "" {
			fmt.Fprintf(b, "    <text style=\"body\">→ %s</text>\n", morningRaw(project.NextAction))
		}
		b.WriteString("  </card>\n")
	}
}

func writeMorningQuestions(b *strings.Builder, questions openQuestionsData) {
	if len(questions.Items) == 0 {
		return
	}
	b.WriteString("  <card>\n    <row><icon name=\"help\" size=\"16\"/><text style=\"caption\">확인 필요</text></row>\n    <ul>")
	for _, item := range questions.Items {
		age := ""
		if item.AgeDays >= 0 {
			age = fmt.Sprintf(" · %d일째", item.AgeDays)
		}
		fmt.Fprintf(b, "<li>🟡 %s — %s%s</li>", morningRaw(item.Project), morningRaw(item.Question), age)
	}
	b.WriteString("</ul>\n  </card>\n")
}

func writeMorningPending(b *strings.Builder, pending groupwarePendingData) {
	if pending.Count == 0 {
		return
	}
	fmt.Fprintf(b, "  <card>\n    <row><icon name=\"assignment\" size=\"16\"/><text style=\"caption\">미결 전자결재 · %d건</text></row>\n    <ul>", pending.Count)
	for i, item := range pending.Items {
		if i == 5 {
			break
		}
		marker := "🔵"
		stale := ""
		if item.StaleLabel != "" || item.EscalationLevel > 0 {
			marker = "🟠"
			stale = " · " + item.StaleLabel
		}
		fmt.Fprintf(b, "<li>%s %s — %s%s</li>", marker, morningRaw(item.Title), morningRaw(item.Drafter), morningRaw(stale))
	}
	b.WriteString("</ul>\n  </card>\n")
}

func writeMorningCC(b *strings.Builder, cc groupwareCCData) {
	if cc.Count == 0 {
		return
	}
	fmt.Fprintf(b, "  <card>\n    <row><icon name=\"visibility\" size=\"16\"/><text style=\"caption\">수신참조 신규 · %d건</text></row>\n    <ul>", cc.Count)
	for _, item := range cc.Items {
		detail := item.Gist
		if detail == "" {
			detail = item.Drafter
		}
		fmt.Fprintf(b, "<li>🔵 %s — %s</li>", morningRaw(item.Title), morningRaw(detail))
	}
	b.WriteString("</ul>\n  </card>\n")
}

func writeMorningActions(b *strings.Builder, weather weatherData, deadlines deadlineData, questions openQuestionsData, pending groupwarePendingData) {
	actions := make([]string, 0, 4)
	for _, item := range deadlines.Items {
		if item.DaysLeft <= 3 {
			label, _, marker := deadlinePresentation(item.DaysLeft)
			actions = append(actions, fmt.Sprintf("%s %s %s — 담당과 완료 시점을 재확인", marker, item.Title, label))
		}
		if len(actions) == 2 {
			break
		}
	}
	if pending.StaleCount > 0 && len(actions) < 4 {
		actions = append(actions, fmt.Sprintf("🟠 방치 결재 %d건 — 오늘 첫 업무 블록에서 처리", pending.StaleCount))
	}
	if len(questions.Items) > 0 && len(actions) < 4 {
		actions = append(actions, "🟡 "+questions.Items[0].Project+" 확인사항 — 답변 주체를 지정")
	}
	if weather.OK && weather.MaxRainPct >= 40 && len(actions) < 4 {
		actions = append(actions, fmt.Sprintf("🔵 강수 %d%% — 외근 일정과 이동 시간을 점검", weather.MaxRainPct))
	}
	if len(actions) == 0 {
		return
	}
	b.WriteString("  <card>\n    <row><icon name=\"priority_high\" size=\"16\"/><text style=\"caption\">주의 · 후행 제안</text></row>\n")
	writeMorningList(b, actions, 4)
	b.WriteString("  </card>\n")
}

func writeMorningNarrativeActions(b *strings.Builder, narrative morningLetterNarrative) {
	b.WriteString("  <card>\n    <row><icon name=\"priority_high\" size=\"16\"/><text style=\"caption\">주의 · 후행 제안</text></row>\n    <ul>")
	for _, risk := range narrative.Risks {
		fmt.Fprintf(b, "<li>⚠️ %s</li>", morningRaw(risk))
	}
	for _, suggestion := range narrative.Suggestions {
		fmt.Fprintf(b, "<li>💡 %s</li>", morningRaw(suggestion))
	}
	b.WriteString("</ul>\n  </card>\n")
}

func writeMorningList(b *strings.Builder, items []string, maxItems int) {
	b.WriteString("    <ul>")
	for i, item := range items {
		if i == maxItems {
			break
		}
		fmt.Fprintf(b, "<li>%s</li>", morningRaw(item))
	}
	b.WriteString("</ul>\n")
}

func deadlinePresentation(days int) (label, colorAttr, marker string) {
	switch {
	case days < 0:
		return "기한 초과", ` color="error"`, "🔴"
	case days == 0:
		return "D-day", ` color="error"`, "🔴"
	case days <= 3:
		return fmt.Sprintf("D-%d", days), ` color="warning"`, "🟠"
	default:
		return fmt.Sprintf("D-%d", days), "", "🟡"
	}
}

func normalizeMorningPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "urgent", "긴급":
		return "urgent"
	case "due", "deadline", "마감임박":
		return "due"
	case "confirm", "확인필요":
		return "confirm"
	default:
		return "info"
	}
}

func morningPriorityPresentation(priority string) (marker, colorAttr, label string) {
	switch normalizeMorningPriority(priority) {
	case "urgent":
		return "🔴", ` color="error"`, "긴급"
	case "due":
		return "🟠", ` color="warning"`, "마감임박"
	case "confirm":
		return "🟡", "", "확인필요"
	default:
		return "🔵", "", "정보공유"
	}
}

func normalizeMorningLines(lines []string, maxItems int) []string {
	if len(lines) > maxItems {
		lines = lines[:maxItems]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = morningPlain(line, 180); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func weatherIcon(condition string) string {
	lower := strings.ToLower(condition)
	switch {
	case strings.Contains(lower, "rain"), strings.Contains(lower, "drizzle"), strings.Contains(lower, "비"), strings.Contains(lower, "눈"):
		return "water_drop"
	case strings.Contains(lower, "cloud"), strings.Contains(lower, "overcast"), strings.Contains(lower, "흐림"), strings.Contains(lower, "구름"):
		return "cloud"
	default:
		return "sunny"
	}
}

func emailSenderName(from string) string {
	from = strings.TrimSpace(from)
	if i := strings.Index(from, "<"); i > 0 {
		from = strings.TrimSpace(from[:i])
	}
	from = strings.Trim(from, `"'`)
	return morningPlain(from, 36)
}

func morningPlain(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "`", "'")
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		runes := []rune(s)
		s = string(runes[:maxRunes-1]) + "…"
	}
	return s
}

func morningRaw(s string) string {
	return morningRawEscaper.Replace(morningPlain(s, 180))
}

func morningAttr(s string) string {
	return morningAttrEscaper.Replace(morningPlain(s, 100))
}
