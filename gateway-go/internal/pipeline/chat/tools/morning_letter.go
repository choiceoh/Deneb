package tools

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
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// MorningLetterOpts holds optional configuration for the morning letter tool.
type MorningLetterOpts struct {
	DiaryDir string // wiki diary directory; empty = no diary logging
	WikiDir  string // wiki root directory; empty = no deadline scan
}

// ToolMorningLetter returns the morning_letter tool — collects 6 data sections
// in parallel and returns structured JSON for the LLM to compose the final letter.
//
// The LLM receives raw data and is responsible for formatting, tone, and
// contextual interpretation (e.g. "우산 챙기세요" for rain, email importance ranking).
//
// Sections: weather (Gwangju), exchange rates, copper price, calendar, email,
// deadlines (upcoming due dates scanned from wiki pages).
func ToolMorningLetter(_ toolctx.ToolExecutor, opts ...MorningLetterOpts) ToolFunc {
	var diaryDir, wikiDir string
	if len(opts) > 0 {
		diaryDir = opts[0].DiaryDir
		wikiDir = opts[0].WikiDir
	}

	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)

		var (
			mu      sync.Mutex
			results = make([]any, 6)
		)

		type collector struct {
			idx int
			fn  func(ctx context.Context) any
		}
		collectors := []collector{
			{0, func(ctx context.Context) any { return fetchWeather(ctx) }},
			{1, func(ctx context.Context) any { return fetchExchangeRates(ctx) }},
			{2, func(ctx context.Context) any { return fetchCopper(ctx) }},
			{3, func(ctx context.Context) any { return fetchCalendar(ctx) }},
			{4, func(ctx context.Context) any { return fetchEmail(ctx) }},
			{5, func(_ context.Context) any { return fetchDeadlines(wikiDir, now) }},
		}

		var wg sync.WaitGroup
		for _, c := range collectors {
			wg.Add(1)
			go func(idx int, fn func(context.Context) any) {
				defer wg.Done()
				sectionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				data := fn(sectionCtx)
				mu.Lock()
				results[idx] = data
				mu.Unlock()
			}(c.idx, c.fn)
		}
		wg.Wait()

		weekday := [...]string{"일", "월", "화", "수", "목", "금", "토"}[now.Weekday()]
		dateStr := fmt.Sprintf("%d년 %d월 %d일 %s요일", now.Year(), int(now.Month()), now.Day(), weekday)
		envelope := map[string]any{
			"date":      dateStr,
			"timestamp": now.Format(time.RFC3339),
			"sections": map[string]any{
				"weather":   results[0],
				"exchange":  results[1],
				"copper":    results[2],
				"calendar":  results[3],
				"email":     results[4],
				"deadlines": results[5],
			},
		}

		out, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal morning letter data: %w", err)
		}

		// Log collected data to diary for wiki knowledge synthesis.
		if diaryDir != "" {
			summary := formatMorningDiarySummary(dateStr, results)
			_ = wiki.AppendDiaryTo(diaryDir, summary) // best-effort: diary append is non-critical
		}

		return string(out), nil
	}
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
		fmt.Fprintf(&sb, "- 환율: USD/KRW %.0f", x.USDKRW)
		if x.EURKRW > 0 {
			fmt.Fprintf(&sb, ", EUR/KRW %.0f", x.EURKRW)
		}
		sb.WriteString("\n")
	}

	if c, ok := results[2].(copperData); ok && c.OK {
		fmt.Fprintf(&sb, "- 동: $%.0f/톤\n", c.PricePerTon)
	}

	if cal, ok := results[3].(calendarData); ok && cal.OK && len(cal.Events) > 0 {
		fmt.Fprintf(&sb, "- 일정: %d건\n", len(cal.Events))
	}

	if em, ok := results[4].(emailData); ok && em.OK && len(em.Messages) > 0 {
		fmt.Fprintf(&sb, "- 메일: %d건\n", len(em.Messages))
	}

	if dl, ok := results[5].(deadlineData); ok && dl.OK && len(dl.Items) > 0 {
		fmt.Fprintf(&sb, "- 임박 마감: %d건\n", len(dl.Items))
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
	OK     bool    `json:"ok"`
	USDKRW float64 `json:"usd_krw,omitempty"`
	EURKRW float64 `json:"eur_krw,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type copperData struct {
	OK          bool    `json:"ok"`
	PricePerTon float64 `json:"price_per_ton_usd,omitempty"` // USD/metric ton
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

	d := exchangeData{OK: true, USDKRW: krw}
	if eurRate, ok := raw.Rates["EUR"]; ok && eurRate > 0 {
		d.EURKRW = krw / eurRate
	}
	return d
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
	return parseYahooCopper(body)
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

	// HG=F is quoted in USD per pound; 1 metric ton = 2,204.6226 pounds.
	const poundsPerTon = 2204.6226
	out := copperData{
		OK:          true,
		PricePerTon: meta.RegularMarketPrice * poundsPerTon,
	}
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
	out := make([]string, 0, len(events))
	for _, e := range events {
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
		Addr:      mailArchiveAddr(),
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
		if page.Meta.Due == "" || page.Meta.Archived {
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

// normalizeWttrTime converts wttr.in time format ("600", "1200") to "06:00", "12:00".
func normalizeWttrTime(t string) string {
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
