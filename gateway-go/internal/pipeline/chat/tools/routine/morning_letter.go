package routine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// MorningLetterOpts holds optional configuration for the morning letter tool.
type MorningLetterOpts struct {
	DiaryDir             string                    // wiki diary directory; empty = no diary logging
	WikiDir              string                    // wiki root directory; empty = no deadline/open-question scans
	GroupwareCollector   func(context.Context) any // optional test/alternate collector
	GroupwareCCCollector func(context.Context) any // optional test/alternate 수신참조 collector
}

// ToolMorningLetter returns the morning_letter tool — collects 10 data sections
// in parallel and returns both the structured facts and a deterministic,
// delivery-ready deneb-ui card. The card is the authoritative projection; raw
// sections remain available for inspection and backwards compatibility.
//
// Sections: weather (Gwangju), exchange rates, copper price, calendar, email,
// deadlines and recent project signals from wiki, long-open questions, pending
// groupware approvals, and new groupware CC documents.
func ToolMorningLetter(opts ...MorningLetterOpts) toolport.ToolFunc {
	var cfg MorningLetterOpts
	if len(opts) > 0 {
		cfg = opts[0]
	}

	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)
		envelope, results := collectMorningLetter(ctx, cfg, now)
		envelope.Delivery = composeMorningLetterCard(envelope, now)

		out, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal morning letter data: %w", err)
		}

		// Log collected data to diary for wiki knowledge synthesis.
		if cfg.DiaryDir != "" {
			summary := formatMorningDiarySummary(envelope.Date, results)
			_ = wiki.AppendDiaryTo(cfg.DiaryDir, summary) // best-effort: diary append is non-critical
		}

		return string(out), nil
	}
}

// CollectMorningLetterData collects the same facts as ToolMorningLetter without
// the fallback delivery field. Cron passes this compact envelope through one
// model projection turn, then RenderMorningLetterCard applies the semantic
// slots to the fixed server-side card.
func CollectMorningLetterData(ctx context.Context, opts MorningLetterOpts, now time.Time) (string, error) {
	now = now.In(kstLocation)
	envelope, results := collectMorningLetter(ctx, opts, now)
	if opts.DiaryDir != "" {
		_ = wiki.AppendDiaryTo(opts.DiaryDir, formatMorningDiarySummary(envelope.Date, results))
	}
	// The projection model needs facts, not the manual-tool contract, legacy
	// substitution tokens, or backend diagnostics. Keep those out of its prompt.
	envelope.Note = ""
	envelope.Sections.Exchange.USDKRWToken = ""
	envelope.Sections.Exchange.Error = ""
	envelope.Sections.Copper.Token = ""
	envelope.Sections.Copper.Error = ""
	envelope.Sections.Weather.Error = ""
	envelope.Sections.Calendar.Error = ""
	envelope.Sections.Email.Error = ""
	envelope.Sections.GroupwarePending.Error = ""
	envelope.Sections.GroupwareCC.Error = ""
	out, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal morning letter data: %w", err)
	}
	return string(out), nil
}

// formatMorningDiarySummary builds a concise diary entry from morning letter data.
func formatMorningDiarySummary(dateStr string, results []any) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🌅 모닝레터 수집 (%s)\n\n", dateStr)

	if w, ok := results[0].(weatherData); ok && w.OK {
		fmt.Fprintf(&sb, "- 날씨: %s°C (체감 %s°C), %s, 습도 %s%%", w.TempC, w.FeelsLikeC, w.Condition, w.Humidity)
		if w.MaxRainPct > 0 {
			fmt.Fprintf(&sb, ", 강수확률 %d%% (%s)", w.MaxRainPct, w.MaxRainTime)
		}
		sb.WriteString("\n")
	}

	if x, ok := results[1].(exchangeData); ok && x.OK {
		fmt.Fprintf(&sb, "- 환율: USD/KRW %.0f\n", x.USDKRW)
	}

	if c, ok := results[2].(copperData); ok && c.OK {
		fmt.Fprintf(&sb, "- 동: $%.0f/톤\n", c.PricePerTon)
	}

	if cal, ok := results[3].(calendarData); ok && cal.OK && len(cal.Events) > 0 {
		fmt.Fprintf(&sb, "- 일정: %d건\n", len(cal.Events))
	}

	if len(results) > 9 {
		if projects, ok := results[9].(morningProjectSignalsData); ok && projects.OK && len(projects.Items) > 0 {
			fmt.Fprintf(&sb, "- 최근 프로젝트: %d건\n", len(projects.Items))
		}
	}

	if em, ok := results[4].(emailData); ok && em.OK && len(em.Messages) > 0 {
		fmt.Fprintf(&sb, "- 메일: %d건\n", len(em.Messages))
	}

	if dl, ok := results[5].(deadlineData); ok && dl.OK && len(dl.Items) > 0 {
		fmt.Fprintf(&sb, "- 임박 마감: %d건\n", len(dl.Items))
	}
	if len(results) > 7 {
		if gw, ok := results[7].(groupwarePendingData); ok && gw.OK && gw.Count > 0 {
			if gw.StaleCount > 0 {
				fmt.Fprintf(&sb, "- 미결 전자결재: %d건 (방치 %d건)\n", gw.Count, gw.StaleCount)
			} else {
				fmt.Fprintf(&sb, "- 미결 전자결재: %d건\n", gw.Count)
			}
		}
	}
	if len(results) > 8 {
		if cc, ok := results[8].(groupwareCCData); ok && cc.OK && cc.Count > 0 {
			fmt.Fprintf(&sb, "- 수신참조 신규: %d건\n", cc.Count)
		}
	}

	return sb.String()
}

// --- KST location ---

var kstLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}()

// --- Section data types ---

type weatherData struct {
	OK          bool   `json:"ok"`
	TempC       string `json:"temp_c,omitempty"`
	FeelsLikeC  string `json:"feels_like_c,omitempty"`
	Condition   string `json:"condition,omitempty"`
	Humidity    string `json:"humidity,omitempty"`
	MinTempC    string `json:"min_temp_c,omitempty"`
	MaxTempC    string `json:"max_temp_c,omitempty"`
	MaxRainPct  int    `json:"max_rain_pct,omitempty"`
	MaxRainTime string `json:"max_rain_time,omitempty"`
	Error       string `json:"error,omitempty"`
}

type exchangeData struct {
	OK bool `json:"ok"`
	// Raw floats are for the model to REASON about (trend commentary); the
	// *_token placeholders are what it places in the letter text. The relay
	// substitutes them with the fetched display strings mechanically, so the
	// model never transcribes a digit (2026-07-07: usd_krw 1530.98 became
	// "1,331원" when the LLM reformatted the float itself).
	USDKRW      float64 `json:"usd_krw,omitempty"`
	USDKRWToken string  `json:"usd_krw_token,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type copperData struct {
	OK          bool    `json:"ok"`
	PricePerTon float64 `json:"price_per_ton_usd,omitempty"` // USD/metric ton
	Token       string  `json:"token,omitempty"`             // placeholder for the letter — see exchangeData
	Display     string  `json:"-"`                           // substitution value ("13,786"), recorded by fetchCopper
	Date        string  `json:"date,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type calendarData struct {
	OK     bool     `json:"ok"`
	Events []string `json:"events,omitempty"`
	Error  string   `json:"error,omitempty"`
}

type emailData struct {
	OK       bool         `json:"ok"`
	Messages []emailEntry `json:"messages,omitempty"`
	Error    string       `json:"error,omitempty"`
}

type emailEntry struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type groupwarePendingData struct {
	OK         bool                    `json:"ok"`
	Configured bool                    `json:"configured"`
	Count      int                     `json:"count,omitempty"`
	StaleCount int                     `json:"stale_count,omitempty"` // escalation_level > 0
	Items      []groupwarePendingEntry `json:"items,omitempty"`
	Error      string                  `json:"error,omitempty"`
}

type groupwarePendingEntry struct {
	DocID           string `json:"doc_id"`
	Title           string `json:"title"`
	Drafter         string `json:"drafter,omitempty"`
	Date            string `json:"date,omitempty"`
	AgeHours        int    `json:"age_hours,omitempty"`
	EscalationLevel int    `json:"escalation_level,omitempty"`
	StaleLabel      string `json:"stale_label,omitempty"`
}

type groupwareCCData struct {
	OK         bool               `json:"ok"`
	Configured bool               `json:"configured"`
	Count      int                `json:"count,omitempty"` // new cc docs inside the highlight window
	Items      []groupwareCCEntry `json:"items,omitempty"`
	Error      string             `json:"error,omitempty"`
}

type groupwareCCEntry struct {
	DocID      string `json:"doc_id"`
	Title      string `json:"title"`
	Drafter    string `json:"drafter,omitempty"`
	Date       string `json:"date,omitempty"`
	Importance string `json:"importance,omitempty"` // from the radar's cached analysis
	Gist       string `json:"gist,omitempty"`       // 요지 line from the cached analysis
}

type deadlineData struct {
	OK    bool            `json:"ok"`
	Items []deadlineEntry `json:"items,omitempty"`
}

type deadlineEntry struct {
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
	Due      string `json:"due"`       // YYYY-MM-DD
	DaysLeft int    `json:"days_left"` // negative = overdue
	Path     string `json:"path,omitempty"`
}

// --- Section collectors (return structured data for LLM to format) ---

func fetchGroupwarePending(ctx context.Context) any {
	cfg, ok := groupware.FromEnv()
	if !ok {
		return groupwarePendingData{Configured: false}
	}
	docs, err := groupware.ListApprovals(ctx, cfg, "pending", 20)
	if err != nil {
		return groupwarePendingData{Configured: true, Error: err.Error()}
	}
	radar := groupware.LoadRadarDocMetaIndex(groupware.DefaultRadarStatePath(), time.Now())
	items := make([]groupwarePendingEntry, 0, len(docs))
	stale := 0
	for _, doc := range docs {
		entry := groupwarePendingEntry{DocID: doc.DocID, Title: doc.Title, Drafter: doc.Drafter, Date: doc.Date}
		if meta, ok := radar[strings.TrimSpace(doc.DocID)]; ok {
			entry.AgeHours = meta.AgeHours
			entry.EscalationLevel = meta.EscalationLevel
			entry.StaleLabel = meta.StaleLabel
			if meta.EscalationLevel > 0 {
				stale++
			}
		}
		items = append(items, entry)
	}
	return groupwarePendingData{OK: true, Configured: true, Count: len(items), StaleCount: stale, Items: items}
}

// ccHighlightWindow bounds which 수신참조 docs count as "new" for the letter —
// wide enough that weekend arrivals (first seen by Monday's radar scan, after
// the letter) still make the next morning's letter.
const ccHighlightWindow = 36 * time.Hour

// ccHighlightMaxItems bounds the letter section (Count still reports the total).
const ccHighlightMaxItems = 5

// fetchGroupwareCC surfaces 수신참조 docs the radar first saw inside the
// highlight window, enriched from the cached analysis (importance + 요지) —
// zero LLM cost at letter time. No radar state yet → empty (but OK) section.
func fetchGroupwareCC(ctx context.Context) any {
	cfg, ok := groupware.FromEnv()
	if !ok {
		return groupwareCCData{Configured: false}
	}
	firstSeen := groupware.LoadRadarCCFirstSeenIndex(groupware.DefaultRadarStatePath())
	if len(firstSeen) == 0 {
		return groupwareCCData{OK: true, Configured: true}
	}
	docs, err := groupware.ListApprovals(ctx, cfg, "cc", 20)
	if err != nil {
		return groupwareCCData{Configured: true, Error: err.Error()}
	}
	store := groupware.NewApprovalAnalysisStore(groupware.DefaultApprovalAnalysisDir())
	cutoff := time.Now().Add(-ccHighlightWindow).UnixMilli()
	items := make([]groupwareCCEntry, 0, ccHighlightMaxItems)
	recent := 0
	for _, doc := range docs {
		id := strings.TrimSpace(doc.DocID)
		seenAt, tracked := firstSeen[id]
		if id == "" || !tracked || seenAt < cutoff {
			continue
		}
		recent++
		if len(items) >= ccHighlightMaxItems {
			continue
		}
		entry := groupwareCCEntry{DocID: id, Title: doc.Title, Drafter: doc.Drafter, Date: doc.Date}
		if rec, lerr := store.Load(id); lerr == nil && rec != nil {
			entry.Importance = rec.Importance
			entry.Gist = groupware.ApprovalAnalysisGistLine(rec.Analysis)
		}
		items = append(items, entry)
	}
	return groupwareCCData{OK: true, Configured: true, Count: recent, Items: items}
}

func fetchWeather(ctx context.Context) any {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://wttr.in/Gwangju,South+Korea?format=j1", nil)
	if err != nil {
		return weatherData{Error: "request build failed"}
	}
	resp, err := httputil.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return weatherData{Error: "network error"}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return weatherData{Error: "read error"}
	}

	var raw struct {
		CurrentCondition []struct {
			TempC      string `json:"temp_C"`
			FeelsLikeC string `json:"FeelsLikeC"`
			Humidity   string `json:"humidity"`
			LangKo     []struct {
				Value string `json:"value"`
			} `json:"lang_ko"`
		} `json:"current_condition"`
		Weather []struct {
			MinTempC string `json:"mintempC"`
			MaxTempC string `json:"maxtempC"`
			Hourly   []struct {
				ChanceOfRain string `json:"chanceofrain"`
				Time         string `json:"time"`
			} `json:"hourly"`
		} `json:"weather"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || len(raw.CurrentCondition) == 0 {
		return weatherData{Error: "parse error"}
	}

	cc := raw.CurrentCondition[0]
	d := weatherData{
		OK:         true,
		TempC:      cc.TempC,
		FeelsLikeC: cc.FeelsLikeC,
		Humidity:   cc.Humidity,
	}
	if len(cc.LangKo) > 0 {
		d.Condition = cc.LangKo[0].Value
	}
	if len(raw.Weather) > 0 {
		w := raw.Weather[0]
		d.MinTempC = w.MinTempC
		d.MaxTempC = w.MaxTempC

		maxRain := 0
		rainTime := ""
		for _, h := range w.Hourly {
			pct, err := strconv.Atoi(strings.TrimSpace(h.ChanceOfRain))
			if err != nil {
				continue
			}
			if pct > maxRain {
				maxRain = pct
				rainTime = h.Time
			}
		}
		if maxRain >= 30 {
			d.MaxRainPct = maxRain
			d.MaxRainTime = normalizeWttrTime(rainTime)
		}
	}
	return d
}

func fetchExchangeRates(ctx context.Context) any {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://open.er-api.com/v6/latest/USD", nil)
	if err != nil {
		return exchangeData{Error: "request build failed"}
	}
	resp, err := httputil.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return exchangeData{Error: "network error"}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return exchangeData{Error: "read error"}
	}

	var raw struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.Result != "success" {
		return exchangeData{Error: "parse error"}
	}

	krw, ok := raw.Rates["KRW"]
	if !ok {
		return exchangeData{Error: "KRW rate not found"}
	}

	// Morning letter shows USD/KRW + copper only (operator preference); EUR is
	// deliberately not surfaced. Not emitting the token at the source keeps it
	// out of the letter regardless of what the composing turn loads.
	d := exchangeData{OK: true, USDKRW: krw, USDKRWToken: market.LetterTokenUSDKRW}
	tokens := map[string]string{market.LetterTokenUSDKRW: formatGroupedInt(krw)}
	market.RecordLetterTokens(tokens)
	return d
}

// formatGroupedInt renders the bare grouped number the relay substitutes for a
// letter token ("1,531"). Digits only — units stay in the model's own prose
// ("달러당 {{market:usd_krw}}원"), so the substitution can never double them.
func formatGroupedInt(v float64) string {
	return textutil.GroupThousands(fmt.Sprintf("%.0f", v))
}

// fetchCopper fetches the COMEX copper futures price (HG=F) from Yahoo Finance
// and returns it as USD per metric ton. Keyless and free: MetalpriceAPI's XCU
// symbol requires a paid plan ("XCU query requires a paid plan"), so we read the
// publicly available COMEX quote instead. COMEX copper tracks LME closely; the
// exchange basis is immaterial for a daily brief.
func fetchCopper(ctx context.Context) any {
	const yahooURL = "https://query1.finance.yahoo.com/v8/finance/chart/HG=F?interval=1d&range=5d"
	req, err := http.NewRequestWithContext(ctx, "GET", yahooURL, nil)
	if err != nil {
		return copperData{Error: "request build failed"}
	}
	// Yahoo rejects the default Go user agent; present a browser-like UA.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Deneb/1.0)")

	resp, err := httputil.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return copperData{Error: "network error"}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return copperData{Error: "read error"}
	}
	d := parseYahooCopper(body)
	if d.OK && d.Display != "" {
		market.RecordLetterTokens(map[string]string{market.LetterTokenCopper: d.Display})
	}
	return d
}

// parseYahooCopper extracts the latest price from a Yahoo Finance chart response
// for HG=F (COMEX copper, quoted in USD per pound) and converts it to USD per
// metric ton. Split from the HTTP call so the unit conversion is testable.
func parseYahooCopper(body []byte) copperData {
	var raw struct {
		Chart struct {
			Result []struct {
				Meta struct {
					RegularMarketPrice float64 `json:"regularMarketPrice"`
					Currency           string  `json:"currency"`
					RegularMarketTime  int64   `json:"regularMarketTime"`
				} `json:"meta"`
			} `json:"result"`
			Error any `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return copperData{Error: "parse error"}
	}
	if raw.Chart.Error != nil || len(raw.Chart.Result) == 0 {
		return copperData{Error: "no copper data"}
	}
	meta := raw.Chart.Result[0].Meta
	if meta.RegularMarketPrice <= 0 {
		return copperData{Error: "copper price unavailable"}
	}
	if currency := strings.ToUpper(strings.TrimSpace(meta.Currency)); currency != "" && currency != "USD" {
		return copperData{Error: "unexpected copper currency"}
	}

	// HG=F is quoted in USD per pound; 1 metric ton = 2,204.6226 pounds.
	const poundsPerTon = 2204.6226
	out := copperData{
		OK:          true,
		PricePerTon: meta.RegularMarketPrice * poundsPerTon,
		Token:       market.LetterTokenCopper,
	}
	out.Display = textutil.GroupThousands(fmt.Sprintf("%.0f", out.PricePerTon))
	if meta.RegularMarketTime > 0 {
		out.Date = time.Unix(meta.RegularMarketTime, 0).In(kstLocation).Format("2006-01-02")
	}
	return out
}

// fetchCalendar reads today + tomorrow from the native local calendar store —
// the same store the calendar tool writes — replacing the old gcalcli shell-out
// that was never installed on the host (every letter logged "gcalcli not
// installed").
func fetchCalendar(_ context.Context) any {
	store, err := localcal.Default()
	if err != nil {
		return calendarData{Error: "calendar unavailable"}
	}
	now := time.Now().In(kstLocation)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, kstLocation)
	to := from.Add(48 * time.Hour) // today + tomorrow
	return calendarData{OK: true, Events: formatLetterCalendar(store.ListRange(from, to), 10)}
}

// formatLetterCalendar renders calendar events as "MM/DD HH:MM — 제목 [@장소]"
// lines (chronological, capped at max). Split out so it is unit-testable without
// a live store.
func formatLetterCalendar(events []calendar.Event, max int) []string {
	ordered := append([]calendar.Event(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start.Before(ordered[j].Start) })
	out := make([]string, 0, len(ordered))
	for _, e := range ordered {
		if len(out) >= max {
			break
		}
		line := e.Start.In(kstLocation).Format("01/02 15:04") + " — " + e.Summary
		if strings.TrimSpace(e.Location) != "" {
			line += " @" + e.Location
		}
		out = append(out, line)
	}
	return out
}

func fetchEmail(ctx context.Context) any {
	configuredMailboxes := mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
	mailboxes := mailarchive.SelectMailboxes("INBOX", configuredMailboxes)
	cfg := mailarchive.Config{
		Addr:      mailarchive.AddressFromEnv(),
		User:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_USER")),
		Pass:      strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_PASS")),
		Mailboxes: mailboxes,
	}
	if cfg.User == "" || cfg.Pass == "" {
		return emailData{Error: "mail archive not configured"}
	}

	msgs, err := mailarchive.ListContextMessages(ctx, cfg, time.Now().AddDate(0, 0, -1), mailarchive.ContextOptions{
		Mailboxes: mailboxes,
		Limit:     10,
		BodyRunes: 0,
	})
	if err != nil {
		return emailData{Error: err.Error()}
	}
	if len(msgs) == 0 {
		return emailData{OK: true}
	}

	entries := make([]emailEntry, len(msgs))
	for i, m := range msgs {
		entries[i] = emailEntry{
			From:    m.From,
			Subject: m.Subject,
			Date:    m.Date,
			Snippet: m.Snippet,
		}
	}
	return emailData{OK: true, Messages: entries}
}

// deadlineMinImportance gates which wiki-page deadlines reach the operator.
// Routine deadlines are handled by working-level staff, so the morning letter and
// weekly 현안 surface only high-importance ones — "아주 중요한, 아주 가끔" (operator
// direction 2026-07). Importance is 0.0–1.0 (wiki page.go); 0.9 keeps only top-tier
// active projects (~6 of ~40 dated pages) so at most 1–2 land in a 14-day window.
// Pages with no importance (e.g. 거래처 원장 payment deadlines, staff-owned) fall
// below the bar and stay silent. Tune here if too quiet/noisy.
const deadlineMinImportance = 0.9

// fetchDeadlines scans wiki pages for upcoming `due` dates and returns those
// within the alert window (up to 7 days overdue through 14 days ahead),
// nearest-first. Surfaces payment deadlines and milestones the operator must
// not miss. Returns an empty (but OK) result when wiki is disabled.
func fetchDeadlines(wikiDir string, now time.Time) any {
	if wikiDir == "" {
		return deadlineData{OK: true}
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var items []deadlineEntry
	_ = filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		switch filepath.Base(path) {
		case "index.md", "_index.md", "log.md":
			return nil
		}
		page, parseErr := wiki.ParsePageFile(path)
		if parseErr != nil {
			return nil //nolint:nilerr // unreadable page — skip
		}
		if page.Meta.Due == "" || page.Meta.Archived || page.Meta.Importance < deadlineMinImportance {
			return nil
		}
		due, parseErr := time.ParseInLocation("2006-01-02", page.Meta.Due, now.Location())
		if parseErr != nil {
			return nil //nolint:nilerr // malformed due date — skip
		}
		days := int(due.Sub(today).Hours() / 24)
		if days < -7 || days > 14 {
			return nil
		}
		rel, _ := filepath.Rel(wikiDir, path)
		items = append(items, deadlineEntry{
			Title:    page.Meta.Title,
			Category: page.Meta.Category,
			Due:      page.Meta.Due,
			DaysLeft: days,
			Path:     rel,
		})
		return nil
	})

	sort.Slice(items, func(i, j int) bool { return items[i].DaysLeft < items[j].DaysLeft })
	return deadlineData{OK: true, Items: items}
}

// openQuestionsData carries project 미해결 질문 that stayed open long enough to
// escalate — the letter tells the operator what internal sources could not
// answer, so it can be asked of a person instead of waiting another cycle.
type openQuestionsData struct {
	OK    bool                `json:"ok"`
	Items []wiki.OpenQuestion `json:"items,omitempty"`
}

// openQuestionsStaleDays is how long a question may sit before the letter
// escalates it — one full wiki-research rotation has a chance to close it first.
const openQuestionsStaleDays = 7

// openQuestionsMaxItems bounds the letter section (oldest first beyond it).
const openQuestionsMaxItems = 6

func fetchOpenQuestions(wikiDir string, now time.Time) any {
	if wikiDir == "" {
		return openQuestionsData{OK: true}
	}
	items := wiki.CollectStaleOpenQuestions(wikiDir, openQuestionsStaleDays, now)
	if len(items) > openQuestionsMaxItems {
		items = items[:openQuestionsMaxItems]
	}
	return openQuestionsData{OK: true, Items: items}
}

// normalizeWttrTime converts wttr.in time format ("600", "1200") to "06:00", "12:00".
func normalizeWttrTime(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return t
		}
	}
	switch len(t) {
	case 1:
		return "0" + t + ":00"
	case 2:
		return t + ":00"
	case 3:
		return "0" + string(t[0]) + ":" + t[1:]
	case 4:
		return t[:2] + ":" + t[2:]
	default:
		return t
	}
}
