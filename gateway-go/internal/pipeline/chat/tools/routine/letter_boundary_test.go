package routine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

func waitLetterSignal(t *testing.T, ch <-chan int, label string) int {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return 0
	}
}

func TestCollectLetterSectionsRunsConcurrentlyAndPreservesSlots(t *testing.T) {
	started := make(chan int, 3)
	release := make(chan struct{})
	collectors := make([]letterCollector, 0, 3)
	for index := 0; index < 3; index++ {
		collectors = append(collectors, letterCollector{
			index: index,
			load: func(context.Context) any {
				started <- index
				<-release
				return fmt.Sprintf("section-%d", index)
			},
		})
	}

	done := make(chan []any, 1)
	go func() {
		done <- collectLetterSections(context.Background(), 3, collectors)
	}()

	seen := map[int]bool{}
	for range collectors {
		seen[waitLetterSignal(t, started, "collector start")] = true
	}
	if len(seen) != 3 {
		t.Fatalf("collectors did not start independently: %v", seen)
	}
	close(release)

	select {
	case got := <-done:
		for index, value := range got {
			want := fmt.Sprintf("section-%d", index)
			if value != want {
				t.Errorf("results[%d] = %#v, want %q", index, value, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parallel collection did not finish")
	}
}

func TestCollectLetterSectionsPropagatesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	collector := letterCollector{index: 0, load: func(ctx context.Context) any {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}

	done := make(chan []any, 1)
	go func() {
		done <- collectLetterSections(ctx, 1, []letterCollector{collector})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("collector never entered")
	}
	cancel()

	select {
	case got := <-done:
		err, ok := got[0].(error)
		if len(got) != 1 || !ok || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled result = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancellation did not release collector")
	}
}

func TestCollectLetterSectionsRejectsInvalidCollectors(t *testing.T) {
	var called atomic.Int32
	load := func(context.Context) any {
		called.Add(1)
		return "unexpected"
	}
	collectors := []letterCollector{
		{index: -1, load: load},
		{index: 2, load: load},
		{index: 0, load: nil},
	}
	got := collectLetterSections(context.Background(), 2, collectors)
	if len(got) != 2 || got[0] != nil || got[1] != nil {
		t.Fatalf("invalid collectors populated results: %#v", got)
	}
	if called.Load() != 0 {
		t.Fatalf("invalid collector invoked %d time(s)", called.Load())
	}
	if got := collectLetterSections(context.Background(), -4, collectors); len(got) != 0 {
		t.Fatalf("negative count returned %d slots", len(got))
	}
}

func TestCollectLetterSectionsConcurrentCallsStayIsolated(t *testing.T) {
	const calls = 32
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for call := 0; call < calls; call++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collectors := []letterCollector{
				{index: 0, load: func(context.Context) any { return call }},
				{index: 1, load: func(context.Context) any { return -call }},
			}
			got := collectLetterSections(context.Background(), 2, collectors)
			if got[0] != call || got[1] != -call {
				errs <- fmt.Errorf("call %d received %#v", call, got)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestKoreanDateFormatsAcrossWeekAndTimezone(t *testing.T) {
	start := time.Date(2026, 7, 5, 0, 0, 0, 0, kstLocation)
	wants := []string{"일", "월", "화", "수", "목", "금", "토"}
	for offset, weekday := range wants {
		got := koreanDate(start.AddDate(0, 0, offset))
		want := fmt.Sprintf("2026년 7월 %d일 %s요일", 5+offset, weekday)
		if got != want {
			t.Errorf("day %d = %q, want %q", offset, got, want)
		}
	}

	utc := time.Date(2026, 7, 11, 15, 30, 0, 0, time.UTC)
	if got := koreanDate(utc); got != "2026년 7월 11일 토요일" {
		t.Fatalf("koreanDate must describe the supplied instant's location: %q", got)
	}
}

func TestFormatLetterCalendarSortsCopyStableAndConvertsToKST(t *testing.T) {
	late := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	early := time.Date(2026, 7, 11, 23, 0, 0, 0, time.UTC)
	events := []calendar.Event{
		{ID: "late", Summary: "후순위", Start: late},
		{ID: "first", Summary: "동시 A", Location: " 광주 ", Start: early},
		{ID: "second", Summary: "동시 B", Start: early},
	}
	originalIDs := []string{events[0].ID, events[1].ID, events[2].ID}

	got := formatLetterCalendar(events, 3)
	want := []string{
		"07/12 08:00 — 동시 A @ 광주 ",
		"07/12 08:00 — 동시 B",
		"07/12 10:00 — 후순위",
	}
	if len(got) != len(want) {
		t.Fatalf("calendar len = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("calendar[%d] = %q, want %q", i, got[i], want[i])
		}
		if events[i].ID != originalIDs[i] {
			t.Fatalf("formatLetterCalendar mutated caller order: %+v", events)
		}
	}
	if got := formatLetterCalendar(events, -1); len(got) != 0 {
		t.Fatalf("negative cap returned %v", got)
	}
}

func TestLetterSectionJSONDoesNotLeakInternalDisplay(t *testing.T) {
	payload := struct {
		Copper copperData  `json:"copper"`
		Email  emailData   `json:"email"`
		Empty  weatherData `json:"empty"`
	}{
		Copper: copperData{OK: true, PricePerTon: 13786, Token: "token", Display: "13,786"},
		Email:  emailData{OK: true, Messages: []emailEntry{{From: "a@example.test", Subject: "hello"}}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "13,786") || strings.Contains(text, "Display") {
		t.Fatalf("internal copper display leaked into JSON: %s", text)
	}
	for _, want := range []string{`"price_per_ton_usd":13786`, `"subject":"hello"`, `"empty":{"ok":false}`} {
		if !strings.Contains(text, want) {
			t.Errorf("serialized payload missing %s: %s", want, text)
		}
	}
}

func TestParseYahooCopperCurrencyAndTimestampBoundary(t *testing.T) {
	t.Run("empty currency remains compatible", func(t *testing.T) {
		body := []byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":4.5,"regularMarketTime":1783782000}}],"error":null}}`)
		got := parseYahooCopper(body)
		if !got.OK || got.Date == "" {
			t.Fatalf("legacy response = %+v", got)
		}
	})

	for _, currency := range []string{"EUR", " krw ", "USDT"} {
		t.Run(strings.TrimSpace(currency), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"chart":{"result":[{"meta":{"regularMarketPrice":4.5,"currency":%q}}],"error":null}}`, currency))
			got := parseYahooCopper(body)
			if got.OK || got.Error != "unexpected copper currency" {
				t.Fatalf("currency %q accepted: %+v", currency, got)
			}
		})
	}
}

func TestNormalizeWttrTimeExternalBoundaries(t *testing.T) {
	cases := map[string]string{
		"":        "",
		" 600 ":   "06:00",
		"0":       "00:00",
		"06":      "06:00",
		"1200":    "12:00",
		"2400":    "24:00",
		"12345":   "12345",
		"unknown": "unknown",
		"6:00":    "6:00",
	}
	for input, want := range cases {
		if got := normalizeWttrTime(input); got != want {
			t.Errorf("normalizeWttrTime(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFetchEmailRejectsPartialCredentialsBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name string
		user string
		pass string
	}{
		{name: "both empty"},
		{name: "user only", user: "operator"},
		{name: "pass only", pass: "password"},
		{name: "whitespace", user: " \n ", pass: "\t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DENEB_ARCHIVE_IMAP_USER", tc.user)
			t.Setenv("DENEB_ARCHIVE_IMAP_PASS", tc.pass)
			got, ok := fetchEmail(context.Background()).(emailData)
			if !ok || got.OK || got.Error != "mail archive not configured" {
				t.Fatalf("fetchEmail = %#v", got)
			}
		})
	}
}

func writeRoutineWikiPage(t *testing.T, root, rel string, meta wiki.Frontmatter, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rel, err)
	}
	page := &wiki.Page{Meta: meta, Body: body}
	if err := os.WriteFile(path, page.Render(), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}

func TestFetchDeadlinesReturnsItemsWithinImportanceWindow(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 18, 30, 0, 0, kstLocation)
	base := wiki.Frontmatter{Category: "프로젝트", Importance: deadlineMinImportance}
	write := func(rel, title, due string, mutate func(*wiki.Frontmatter)) {
		meta := base
		meta.Title = title
		meta.Due = due
		if mutate != nil {
			mutate(&meta)
		}
		writeRoutineWikiPage(t, root, rel, meta, "body")
	}

	write("프로젝트/overdue-boundary.md", "초과 경계", "2026-07-04", nil)
	write("프로젝트/today.md", "오늘", "2026-07-11", nil)
	write("프로젝트/ahead-boundary.md", "예정 경계", "2026-07-25", nil)
	write("프로젝트/too-old.md", "너무 오래됨", "2026-07-03", nil)
	write("프로젝트/too-far.md", "너무 멂", "2026-07-26", nil)
	write("프로젝트/low.md", "낮은 중요도", "2026-07-12", func(m *wiki.Frontmatter) { m.Importance = 0.89 })
	write("프로젝트/archived.md", "보관", "2026-07-12", func(m *wiki.Frontmatter) { m.Archived = true })
	write("프로젝트/bad-date.md", "잘못된 날짜", "2026-99-99", nil)
	write("프로젝트/index.md", "인덱스", "2026-07-12", nil)
	if err := os.WriteFile(filepath.Join(root, "broken.md"), []byte("---\nunclosed"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := fetchDeadlines(root, now).(deadlineData)
	if !got.OK || len(got.Items) != 3 {
		t.Fatalf("deadlines = %+v", got)
	}
	wantTitles := []string{"초과 경계", "오늘", "예정 경계"}
	wantDays := []int{-7, 0, 14}
	for i := range wantTitles {
		if got.Items[i].Title != wantTitles[i] || got.Items[i].DaysLeft != wantDays[i] {
			t.Errorf("items[%d] = %+v, want %q D%d", i, got.Items[i], wantTitles[i], wantDays[i])
		}
		if filepath.IsAbs(got.Items[i].Path) {
			t.Errorf("deadline path must stay root-relative: %q", got.Items[i].Path)
		}
	}
	if empty := fetchDeadlines("", now).(deadlineData); !empty.OK || len(empty.Items) != 0 {
		t.Fatalf("disabled wiki = %+v", empty)
	}
}

func TestFetchOpenQuestionsReturnsCappedOldestWithoutNonRepPages(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, kstLocation)
	for index := 0; index < 8; index++ {
		asked := time.Date(2026, 6, 1+index, 0, 0, 0, 0, kstLocation).Format("2006-01-02")
		name := fmt.Sprintf("project-%02d", index)
		writeRoutineWikiPage(t, root, "프로젝트/"+name+"/대표.md", wiki.Frontmatter{Title: name},
			fmt.Sprintf("## 미해결 질문\n- %s question-%02d\n", asked, index))
	}
	writeRoutineWikiPage(t, root, "프로젝트/archive/대표.md", wiki.Frontmatter{Title: "archive", Archived: true},
		"## 미해결 질문\n- 2020-01-01 archived question\n")
	writeRoutineWikiPage(t, root, "프로젝트/project-00/로그.md", wiki.Frontmatter{Title: "log"},
		"## 미해결 질문\n- 2020-01-01 log question\n")

	got := fetchOpenQuestions(root, now).(openQuestionsData)
	if !got.OK || len(got.Items) != openQuestionsMaxItems {
		t.Fatalf("open questions = %+v", got)
	}
	for index, item := range got.Items {
		want := fmt.Sprintf("question-%02d", index)
		if item.Question != want {
			t.Errorf("items[%d] = %+v, want %q", index, item, want)
		}
		if strings.Contains(item.Question, "archived") || strings.Contains(item.Question, "log") {
			t.Errorf("excluded page leaked: %+v", item)
		}
	}
}

func TestLetterDiarySummariesIncludeOnlyHealthyNonEmptySections(t *testing.T) {
	morning := formatMorningDiarySummary("2026-07-11", []any{
		weatherData{OK: true, TempC: "29", FeelsLikeC: "31", Condition: "비", Humidity: "80", MaxRainPct: 70, MaxRainTime: "15:00"},
		exchangeData{OK: true, USDKRW: 1531},
		copperData{OK: false, PricePerTon: 13_786},
		calendarData{OK: true, Events: []string{"meeting"}},
		emailData{OK: true},
		deadlineData{OK: true, Items: []deadlineEntry{{Title: "due"}}},
		openQuestionsData{OK: true},
	})
	for _, want := range []string{"날씨: 29°C", "강수확률 70%", "환율: USD/KRW 1531", "일정: 1건", "임박 마감: 1건"} {
		if !strings.Contains(morning, want) {
			t.Errorf("morning summary missing %q: %s", want, morning)
		}
	}
	for _, unwanted := range []string{"동:", "메일:"} {
		if strings.Contains(morning, unwanted) {
			t.Errorf("morning summary included unhealthy/empty %q: %s", unwanted, morning)
		}
	}

	evening := formatEveningDiarySummary("2026-07-11", []any{
		calendarData{OK: true, Events: []string{"one", "two"}},
		emailData{OK: false, Messages: []emailEntry{{Subject: "must not count"}}},
		deadlineData{OK: true},
	})
	if !strings.Contains(evening, "일정: 2건") || strings.Contains(evening, "메일:") || strings.Contains(evening, "마감:") {
		t.Fatalf("evening summary = %s", evening)
	}
}
