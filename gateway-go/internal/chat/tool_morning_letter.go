package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
)

// toolMorningLetter returns the morning_letter tool — collects 6 data sections
// in parallel and returns a fully formatted Korean briefing text (< 4000 chars).
//
// Sections: weather (Gwangju), exchange rates, copper price, calendar,
// email summary, SMP power price.
func toolMorningLetter(tools ToolExecutor) ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		now := time.Now().In(kstLocation)

		var (
			mu       sync.Mutex
			sections = make([]section, 6)
		)

		// Each collector writes to its slot. Failures produce fallback text.
		var wg sync.WaitGroup
		collectors := []struct {
			idx  int
			name string
			fn   func(ctx context.Context) string
		}{
			{0, "weather", func(ctx context.Context) string { return collectWeather(ctx) }},
			{1, "exchange", func(ctx context.Context) string { return collectExchangeRates(ctx) }},
			{2, "copper", func(ctx context.Context) string { return collectCopper(ctx, tools) }},
			{3, "calendar", func(ctx context.Context) string { return collectCalendar(ctx) }},
			{4, "email", func(ctx context.Context) string { return collectEmail(ctx) }},
			{5, "smp", func(ctx context.Context) string { return collectSMP(ctx, tools) }},
		}

		for _, c := range collectors {
			wg.Add(1)
			go func(idx int, name string, fn func(context.Context) string) {
				defer wg.Done()
				sectionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				text := fn(sectionCtx)
				mu.Lock()
				sections[idx] = section{name: name, text: text}
				mu.Unlock()
			}(c.idx, c.name, c.fn)
		}
		wg.Wait()

		// Assemble the letter.
		letter := formatMorningLetter(now, sections)

		// If over 4000 chars, truncate email section to 3 items.
		if len(letter) > 4000 {
			sections[4].text = collectEmailShort(ctx)
			letter = formatMorningLetter(now, sections)
		}

		return letter, nil
	}
}

// --- Formatting ---

var kstLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}()

var koreanWeekdays = [...]string{"일", "월", "화", "수", "목", "금", "토"}

// section holds one morning letter data section.
type section = struct {
	name string
	text string
}

func formatMorningLetter(now time.Time, sections []section) string {
	weekday := koreanWeekdays[now.Weekday()]
	header := fmt.Sprintf("🌅 모닝레터 — %d년 %d월 %d일 %s요일",
		now.Year(), int(now.Month()), now.Day(), weekday)

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")

	// Weather
	sb.WriteString("\n☁\ufe0f 날씨 (광주광역시)\n")
	sb.WriteString(sections[0].text)
	sb.WriteString("\n")

	// Exchange rates
	sb.WriteString("\n💱 환율\n")
	sb.WriteString(sections[1].text)
	sb.WriteString("\n")

	// Copper
	sb.WriteString("\n🔶 구리시세\n")
	sb.WriteString(sections[2].text)
	sb.WriteString("\n")

	// Calendar
	sb.WriteString("\n📅 오늘 일정\n")
	sb.WriteString(sections[3].text)
	sb.WriteString("\n")

	// Email
	sb.WriteString("\n📧 전일 메일 주요사항\n")
	sb.WriteString(sections[4].text)
	sb.WriteString("\n")

	// SMP
	sb.WriteString("\n⚡ SMP 전력가격\n")
	sb.WriteString(sections[5].text)
	sb.WriteString("\n")

	sb.WriteString("\n좋은 하루 되세요!")

	return sb.String()
}

// --- Section collectors ---

// collectWeather fetches weather data from wttr.in JSON API.
func collectWeather(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://wttr.in/Gwangju,South+Korea?format=j1", nil)
	if err != nil {
		return "조회 실패"
	}
	req.Header.Set("User-Agent", "Deneb-Gateway/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "조회 실패"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024))
	if err != nil {
		return "조회 실패"
	}

	var data struct {
		CurrentCondition []struct {
			TempC       string `json:"temp_C"`
			FeelsLikeC  string `json:"FeelsLikeC"`
			Humidity    string `json:"humidity"`
			LangKo      []struct {
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
	if err := json.Unmarshal(body, &data); err != nil || len(data.CurrentCondition) == 0 {
		return "조회 실패"
	}

	cc := data.CurrentCondition[0]
	condition := "—"
	if len(cc.LangKo) > 0 {
		condition = cc.LangKo[0].Value
	}

	result := fmt.Sprintf("현재 %s°C (체감 %s°C), %s\n", cc.TempC, cc.FeelsLikeC, condition)

	if len(data.Weather) > 0 {
		w := data.Weather[0]
		result += fmt.Sprintf("최저 %s° / 최고 %s°, 습도 %s%%", w.MinTempC, w.MaxTempC, cc.Humidity)

		// Find peak rain chance.
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
			// Convert "600" → "06:00" style.
			t := rainTime
			if len(t) <= 2 {
				t = t + ":00"
			} else if len(t) == 3 {
				t = "0" + string(t[0]) + ":" + t[1:]
			} else if len(t) == 4 {
				t = t[:2] + ":" + t[2:]
			}
			result += fmt.Sprintf("\n🌧 강수확률 %d%% (%s)", maxRain, t)
		}
	}

	return result
}

// collectExchangeRates fetches USD/KRW and EUR/KRW from open.er-api.com.
func collectExchangeRates(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://open.er-api.com/v6/latest/USD", nil)
	if err != nil {
		return "조회 실패"
	}
	req.Header.Set("User-Agent", "Deneb-Gateway/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "조회 실패"
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		return "조회 실패"
	}

	var data struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.Result != "success" {
		return "조회 실패"
	}

	krw, ok := data.Rates["KRW"]
	if !ok {
		return "조회 실패"
	}

	result := fmt.Sprintf("USD/KRW: %s", formatNumber(krw, 2))

	// Derive EUR/KRW: 1 EUR = (1/EUR_per_USD) * KRW_per_USD.
	eurRate, ok := data.Rates["EUR"]
	if ok && eurRate > 0 {
		eurKrw := krw / eurRate
		result += fmt.Sprintf("\nEUR/KRW: %s", formatNumber(eurKrw, 2))
	}

	return result
}

// collectCopper uses web search for LME copper price.
func collectCopper(ctx context.Context, tools ToolExecutor) string {
	query := `{"query": "LME copper price today USD per ton", "count": 3}`
	output, err := tools.Execute(ctx, "web", json.RawMessage(query))
	if err != nil || output == "" {
		return "조회 실패"
	}
	return extractCopperPrice(output)
}

// extractCopperPrice attempts to find a copper price from web search output.
func extractCopperPrice(output string) string {
	// Look for patterns like "$9,500" or "9,500.00" near "copper" or "LME".
	priceRe := regexp.MustCompile(`\$?([\d,]+(?:\.\d{1,2})?)\s*(?:/ton|per\s*(?:metric\s*)?ton|USD)`)
	matches := priceRe.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return fmt.Sprintf("LME: $%s/ton", matches[1])
	}
	// Fallback: return a trimmed excerpt.
	if len(output) > 200 {
		output = output[:200]
	}
	return strings.TrimSpace(output)
}

// collectCalendar runs gcalcli to get today's agenda.
func collectCalendar(ctx context.Context) string {
	if _, err := exec.LookPath("gcalcli"); err != nil {
		return "일정 조회 불가 (gcalcli 미설치)"
	}

	cmd := exec.CommandContext(ctx, "gcalcli", "agenda", "today", "tomorrow",
		"--nostarted", "--details", "length")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "일정 조회 실패"
	}

	text := strings.TrimSpace(string(out))
	if text == "" || strings.Contains(text, "No Events Found") {
		return "오늘 일정 없음"
	}

	// Limit to 8 lines.
	lines := strings.Split(text, "\n")
	if len(lines) > 8 {
		lines = lines[:8]
	}

	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, "• "+line)
		}
	}
	if len(result) == 0 {
		return "오늘 일정 없음"
	}
	return strings.Join(result, "\n")
}

// collectEmail fetches yesterday's important emails via native Gmail API.
func collectEmail(_ context.Context) string {
	return collectEmailWithMax(5)
}

// collectEmailShort fetches emails with reduced count for space savings.
func collectEmailShort(_ context.Context) string {
	return collectEmailWithMax(3)
}

func collectEmailWithMax(max int) string {
	client, err := gmail.GetClient()
	if err != nil {
		return "메일 조회 불가 (인증 정보 없음)"
	}

	msgs, err := client.Search("newer_than:1d", max)
	if err != nil {
		return "메일 조회 실패"
	}
	if len(msgs) == 0 {
		return "어제 수신 메일 없음"
	}

	var lines []string
	for _, m := range msgs {
		from := m.From
		// Shorten "Name <email>" to just "Name".
		if idx := strings.Index(from, "<"); idx > 0 {
			from = strings.TrimSpace(from[:idx])
		}
		lines = append(lines, fmt.Sprintf("• %s — %s", from, m.Subject))
	}
	return strings.Join(lines, "\n")
}

// collectSMP uses web search for Korea SMP electricity price.
func collectSMP(ctx context.Context, tools ToolExecutor) string {
	query := `{"query": "한국 SMP 전력시장 가격 전력거래소 KPX 오늘 육지", "count": 3}`
	output, err := tools.Execute(ctx, "web", json.RawMessage(query))
	if err != nil || output == "" {
		return "조회 불가"
	}
	return extractSMPPrice(output)
}

// extractSMPPrice attempts to extract SMP price from web search output.
func extractSMPPrice(output string) string {
	// Look for patterns like "123.45 원/kWh" or "123.45원".
	priceRe := regexp.MustCompile(`([\d,]+(?:\.\d{1,2})?)\s*원\s*/?(?:kWh)?`)
	matches := priceRe.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return fmt.Sprintf("육지: %s원/kWh", matches[1])
	}
	// Fallback: return a trimmed excerpt.
	if len(output) > 200 {
		output = output[:200]
	}
	return strings.TrimSpace(output)
}

// --- Helpers ---

// formatNumber formats a float with thousand separators and decimal places.
func formatNumber(n float64, decimals int) string {
	// Format with decimals.
	s := fmt.Sprintf("%.*f", decimals, n)

	// Split integer and decimal parts.
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]

	// Add thousand separators.
	var result []byte
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}

	if len(parts) == 2 {
		return string(result) + "." + parts[1]
	}
	return string(result)
}
