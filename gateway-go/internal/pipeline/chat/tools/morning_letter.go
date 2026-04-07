package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// MorningLetterOpts holds optional configuration for the morning letter tool.
type MorningLetterOpts struct {
	DiaryDir string // wiki diary directory; empty = no diary logging
}

// ToolMorningLetter returns the morning_letter tool — collects 5 data sections
// in parallel and returns structured JSON for the LLM to compose the final letter.
//
// The LLM receives raw data and is responsible for formatting, tone, and
// contextual interpretation (e.g. "우산 챙기세요" for rain, email importance ranking).
//
// Sections: weather (Gwangju), exchange rates, copper price, calendar, email.
func ToolMorningLetter(_ toolctx.ToolExecutor, opts ...MorningLetterOpts) ToolFunc {
	var diaryDir string
	if len(opts) > 0 {
		diaryDir = opts[0].DiaryDir
	}

	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)

		var (
			mu      sync.Mutex
			results = make([]any, 5)
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
				"weather":  results[0],
				"exchange": results[1],
				"copper":   results[2],
				"calendar": results[3],
				"email":    results[4],
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
			var pct int
			fmt.Sscanf(h.ChanceOfRain, "%d", &pct)
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

// fetchCopper fetches copper price from MetalpriceAPI (USD per metric ton).
// Requires METALPRICEAPI_KEY environment variable.
// API returns price per troy ounce; we convert to per metric ton (* 32150.747).
func fetchCopper(ctx context.Context) any {
	apiKey := os.Getenv("METALPRICEAPI_KEY")
	if apiKey == "" {
		return copperData{Error: "METALPRICEAPI_KEY not set"}
	}

	url := fmt.Sprintf("https://api.metalpriceapi.com/v1/latest?api_key=%s&base=USD&currencies=XCU", apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return copperData{Error: "request build failed"}
	}
	resp, err := httputil.NewClient(30 * time.Second).Do(req)
	if err != nil {
		return copperData{Error: "network error"}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return copperData{Error: "read error"}
	}

	var raw struct {
		Success bool               `json:"success"`
		Date    string             `json:"date"`
		Rates   map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || !raw.Success {
		return copperData{Error: "parse error or API failure"}
	}

	// The API returns copper price per troy ounce (USDXCU = USD per ounce).
	// Convert to USD per metric ton: 1 metric ton = 32,150.747 troy ounces.
	const troyOuncesPerTon = 32150.747
	pricePerOz, ok := raw.Rates["USDXCU"]
	if !ok {
		// Fallback: try XCU (inverse rate: 1 USD = X ounces of copper).
		if xcuRate, ok2 := raw.Rates["XCU"]; ok2 && xcuRate > 0 {
			pricePerOz = 1.0 / xcuRate
		} else {
			return copperData{Error: "XCU rate not found"}
		}
	}

	return copperData{
		OK:          true,
		PricePerTon: pricePerOz * troyOuncesPerTon,
		Date:        raw.Date,
	}
}

func fetchCalendar(ctx context.Context) any {
	if _, err := exec.LookPath("gcalcli"); err != nil {
		return calendarData{Error: "gcalcli not installed"}
	}

	cmd := exec.CommandContext(ctx, "gcalcli", "agenda", "today", "tomorrow",
		"--nostarted", "--details", "length")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return calendarData{Error: "gcalcli failed"}
	}

	text := strings.TrimSpace(string(out))
	if text == "" || strings.Contains(text, "No Events Found") {
		return calendarData{OK: true}
	}

	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	var events []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			events = append(events, line)
		}
	}
	return calendarData{OK: true, Events: events}
}

func fetchEmail(ctx context.Context) any {
	client, err := gmail.DefaultClient()
	if err != nil {
		return emailData{Error: "no credentials"}
	}

	msgs, err := client.Search(ctx, "newer_than:1d", 10)
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
