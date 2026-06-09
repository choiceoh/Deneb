package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// fullContextEnricher returns an enricher whose deps all produce data, so a
// test can assert every enrichment section renders.
func fullContextEnricher() *briefingEnricher {
	return &briefingEnricher{
		recentMailCount: func(_ context.Context, email string, _ int) (int, error) {
			if email == "p@example.com" {
				return 5, nil
			}
			return 0, nil
		},
		topicMail: func(_ context.Context, _ string, _ int) ([]string, error) {
			return []string{"6월 견적 회신", "납기 일정 문의"}, nil
		},
		wikiNote: func(_ context.Context, query string) string {
			switch query {
			case "박YY":
				return "박YY 부장 — 모듈 공급 담당"
			case "탑솔라 주간회의":
				return "탑솔라 — 태양광 모듈 거래처"
			}
			return ""
		},
		logger: slog.Default(),
	}
}

func meetingEvent() calendar.Event {
	return calendar.Event{
		Summary: "탑솔라 주간회의",
		Attendees: []calendar.Attendee{
			{Email: "self@example.com", Self: true, DisplayName: "Me"},
			{Email: "p@example.com", DisplayName: "박YY", ResponseStatus: "accepted"},
		},
	}
}

func TestBriefingEnricher_Extra_FullContext(t *testing.T) {
	extra := fullContextEnricher().extra(context.Background(), meetingEvent())

	for _, must := range []string{
		"· 박YY",
		"최근 30일 5건",
		"박YY 부장 — 모듈 공급 담당",
		"📧 관련 메일: 6월 견적 회신 / 납기 일정 문의",
		"📌 지난 기록: 탑솔라 — 태양광 모듈 거래처",
	} {
		if !strings.Contains(extra, must) {
			t.Errorf("enrichment missing %q in:\n%s", must, extra)
		}
	}
	// Self attendee must never get a context line.
	if strings.Contains(extra, "Me") {
		t.Errorf("self attendee leaked into enrichment:\n%s", extra)
	}
	// extra must not start with a newline — sendBriefing owns the separator.
	if strings.HasPrefix(extra, "\n") {
		t.Errorf("extra should be trimmed, got leading newline:\n%q", extra)
	}
}

func TestBriefingEnricher_Extra_GracefulDegradation(t *testing.T) {
	enr := &briefingEnricher{
		recentMailCount: func(context.Context, string, int) (int, error) {
			return -1, errors.New("no oauth")
		},
		topicMail: func(context.Context, string, int) ([]string, error) {
			return nil, errors.New("no oauth")
		},
		wikiNote: func(context.Context, string) string { return "" },
		logger:   slog.Default(),
	}
	if extra := enr.extra(context.Background(), meetingEvent()); extra != "" {
		t.Errorf("all-sources-failing enrichment should be empty, got:\n%s", extra)
	}
}

func TestBriefingEnricher_Extra_NilEnricherSafe(t *testing.T) {
	var e *briefingEnricher
	if got := e.extra(context.Background(), meetingEvent()); got != "" {
		t.Errorf("nil enricher should return empty, got %q", got)
	}
}

func TestBriefingEnricher_Extra_PanicRecovered(t *testing.T) {
	enr := &briefingEnricher{
		recentMailCount: func(context.Context, string, int) (int, error) {
			panic("boom")
		},
		logger: slog.Default(),
	}
	// Must not propagate the panic; returns "" so the base briefing still ships.
	if got := enr.extra(context.Background(), meetingEvent()); got != "" {
		t.Errorf("panic path should yield empty enrichment, got %q", got)
	}
}

// A slow dep must be bounded by the enricher timeout, not hang the goroutine.
func TestBriefingEnricher_Extra_TimeoutBounded(t *testing.T) {
	enr := &briefingEnricher{
		recentMailCount: func(ctx context.Context, _ string, _ int) (int, error) {
			<-ctx.Done()
			return -1, ctx.Err()
		},
		wikiNote: func(context.Context, string) string { return "" },
		timeout:  20 * time.Millisecond,
		logger:   slog.Default(),
	}
	start := time.Now()
	extra := enr.extra(context.Background(), meetingEvent())
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("enrichment not bounded by timeout: took %s", elapsed)
	}
	if extra != "" {
		t.Errorf("timed-out lookup should be omitted, got %q", extra)
	}
}

func TestBriefingEnricher_AttendeeLine_NoEmailUsesWikiOnly(t *testing.T) {
	enr := &briefingEnricher{
		// recentMailCount must NOT be consulted for a name-only attendee
		// (no '@'), so make it fail loudly if called.
		recentMailCount: func(context.Context, string, int) (int, error) {
			t.Fatal("recentMailCount called for attendee without an email")
			return 0, nil
		},
		wikiNote: func(context.Context, string) string { return "메모 있음" },
		logger:   slog.Default(),
	}
	line := enr.attendeeLine(context.Background(), calendar.Attendee{DisplayName: "이름만"})
	if !strings.Contains(line, "이름만") || !strings.Contains(line, "메모 있음") {
		t.Errorf("expected wiki-only line, got %q", line)
	}
}

func TestExternalAttendees_FiltersAndCaps(t *testing.T) {
	att := []calendar.Attendee{
		{DisplayName: "Me", Self: true},
		{DisplayName: "A", ResponseStatus: "accepted"},
		{DisplayName: "B", ResponseStatus: "declined"},
		{DisplayName: "C", ResponseStatus: "tentative"},
		{DisplayName: "D"},
	}
	capped := externalAttendees(att, 2)
	if len(capped) != 2 || capped[0].DisplayName != "A" || capped[1].DisplayName != "C" {
		t.Errorf("expected [A C] capped at 2, got %+v", capped)
	}
	all := externalAttendees(att, 0)
	if len(all) != 3 {
		t.Errorf("expected 3 external (Me/self + B/declined dropped), got %+v", all)
	}
}

func TestBriefTopicQuery_SanitizesAndFloors(t *testing.T) {
	cases := map[string]string{
		"[정기] 탑솔라 주간회의":  "정기 탑솔라 주간회의",
		`프로젝트: "킥오프"`:    "프로젝트 킥오프",
		"  여백  많은   제목 ": "여백 많은 제목",
		"x":              "", // single rune → too short to search
		"":               "",
		"   ":            "",
	}
	for in, want := range cases {
		if got := briefTopicQuery(in); got != want {
			t.Errorf("briefTopicQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// sendBriefing must ship base briefing + enrichment as one body, with the
// enrichment additive (base lines preserved).
func TestSendBriefing_AppendsEnrichment(t *testing.T) {
	s := makeService(t)
	var captured string
	s.deliver = func(text string) (bool, error) {
		captured = text
		return true, nil
	}
	s.enricher = fullContextEnricher()

	ev := calendar.Event{
		Summary: "탑솔라 주간회의",
		Start:   time.Date(2026, 6, 9, 14, 0, 0, 0, s.displayLoc),
		Attendees: []calendar.Attendee{
			{Email: "p@example.com", DisplayName: "박YY", ResponseStatus: "accepted"},
		},
	}
	if err := s.sendBriefing(context.Background(), ev); err != nil {
		t.Fatalf("sendBriefing: %v", err)
	}
	// Base briefing preserved.
	for _, must := range []string{"D-15분", "탑솔라 주간회의", "14:00"} {
		if !strings.Contains(captured, must) {
			t.Errorf("base briefing missing %q in:\n%s", must, captured)
		}
	}
	// Enrichment appended.
	if !strings.Contains(captured, "📧 관련 메일:") {
		t.Errorf("enrichment not appended:\n%s", captured)
	}
}

// With no enricher set, sendBriefing ships exactly the base briefing.
func TestSendBriefing_NoEnricherShipsBaseOnly(t *testing.T) {
	s := makeService(t)
	var captured string
	s.deliver = func(text string) (bool, error) {
		captured = text
		return true, nil
	}
	ev := calendar.Event{Summary: "회의", Start: time.Date(2026, 6, 9, 14, 0, 0, 0, s.displayLoc)}
	if err := s.sendBriefing(context.Background(), ev); err != nil {
		t.Fatalf("sendBriefing: %v", err)
	}
	if captured != s.formatBriefing(ev) {
		t.Errorf("expected base briefing only, got:\n%s", captured)
	}
}
