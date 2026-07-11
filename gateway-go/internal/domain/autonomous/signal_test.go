package autonomous

import (
	"strings"
	"testing"
	"time"
)

var signalTestNow = time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)

func at(offset time.Duration) time.Time { return signalTestNow.Add(offset) }

// kindsOf returns the set of signal kinds present in a report (count per kind).
func kindsOf(r SignalReport) map[SignalKind]int {
	m := map[SignalKind]int{}
	for _, s := range r.Signals {
		m[s.Kind]++
	}
	return m
}

func TestDetectSignals_Empty(t *testing.T) {
	r := DetectSignals(SignalInputs{Now: signalTestNow}, DefaultSignalConfig())
	if len(r.Signals) != 0 {
		t.Fatalf("expected no signals, got %d", len(r.Signals))
	}
	if r.Score != 0 {
		t.Fatalf("expected score 0, got %d", r.Score)
	}
	if r.ShouldEscalate() {
		t.Fatal("empty inputs must not escalate")
	}
	if r.Summary(3) != "" {
		t.Fatalf("empty report summary must be empty, got %q", r.Summary(3))
	}
}

func TestDetectSignals_VIPMailUnanswered(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{
		Now: signalTestNow,
		Mail: []MailSignalInput{
			{ID: "1", From: "김부장", Subject: "계약서 검토", Important: true, Unread: true, ReceivedAt: at(-10 * time.Minute)},
		},
	}
	r := DetectSignals(in, cfg)
	if kindsOf(r)[SignalVIPMailUnanswered] != 1 {
		t.Fatalf("expected 1 VIP signal, got report: %+v", r.Signals)
	}
	if r.Score != cfg.VIPMailWeight {
		t.Fatalf("expected score %d, got %d", cfg.VIPMailWeight, r.Score)
	}
	if !r.ShouldEscalate() {
		t.Fatal("a single VIP unanswered mail should escalate by default")
	}
	if !strings.Contains(r.Summary(3), "김부장") {
		t.Fatalf("summary should mention sender, got %q", r.Summary(3))
	}
}

func TestDetectSignals_VIPMailAnsweredOrRead_NoSignal(t *testing.T) {
	cfg := DefaultSignalConfig()
	cases := []MailSignalInput{
		{ID: "a", From: "X", Important: true, Unread: true, Answered: true, ReceivedAt: at(-time.Hour)}, // answered
		{ID: "b", From: "X", Important: true, Unread: false, ReceivedAt: at(-time.Hour)},                // already read
	}
	for _, m := range cases {
		r := DetectSignals(SignalInputs{Now: signalTestNow, Mail: []MailSignalInput{m}}, cfg)
		if len(r.Signals) != 0 {
			t.Fatalf("mail %q should produce no signal, got %+v", m.ID, r.Signals)
		}
	}
}

func TestDetectSignals_StaleVsFreshMail(t *testing.T) {
	cfg := DefaultSignalConfig()
	// Fresh, non-important, unanswered mail → no signal.
	fresh := SignalInputs{Now: signalTestNow, Mail: []MailSignalInput{
		{ID: "f", From: "보통발신자", Unread: true, ReceivedAt: at(-1 * time.Hour)},
	}}
	if r := DetectSignals(fresh, cfg); len(r.Signals) != 0 {
		t.Fatalf("fresh ordinary mail should not signal, got %+v", r.Signals)
	}
	// Aged past StaleMailAge → stale signal.
	stale := SignalInputs{Now: signalTestNow, Mail: []MailSignalInput{
		{ID: "s", From: "보통발신자", Subject: "안내", Unread: true, ReceivedAt: at(-5 * time.Hour)},
	}}
	r := DetectSignals(stale, cfg)
	if kindsOf(r)[SignalMailStale] != 1 {
		t.Fatalf("aged mail should be stale, got %+v", r.Signals)
	}
	if r.Score != cfg.StaleMailWeight {
		t.Fatalf("expected stale weight %d, got %d", cfg.StaleMailWeight, r.Score)
	}
	// One stale mail alone is below the default threshold (no over-notification).
	if r.ShouldEscalate() {
		t.Fatal("a single stale ordinary mail should not escalate by default")
	}
}

func TestDetectSignals_StaleMailAccumulatesToThreshold(t *testing.T) {
	cfg := DefaultSignalConfig() // StaleMailWeight=15, threshold=40 → need 3
	var mail []MailSignalInput
	for i := range 3 {
		mail = append(mail, MailSignalInput{ID: string(rune('a' + i)), From: "발신자", Unread: true, ReceivedAt: at(-6 * time.Hour)})
	}
	r := DetectSignals(SignalInputs{Now: signalTestNow, Mail: mail}, cfg)
	if kindsOf(r)[SignalMailStale] != 3 {
		t.Fatalf("expected 3 stale signals, got %+v", r.Signals)
	}
	if !r.ShouldEscalate() {
		t.Fatalf("3 stale mails (score %d) should cross threshold %d", r.Score, r.Threshold)
	}
}

func TestDetectSignals_CalendarConflict(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "팀 미팅", Start: at(time.Hour), End: at(2 * time.Hour)},
		{ID: "2", Summary: "고객 콜", Start: at(90 * time.Minute), End: at(150 * time.Minute)},
	}}
	r := DetectSignals(in, cfg)
	if kindsOf(r)[SignalCalendarConflict] != 1 {
		t.Fatalf("expected 1 conflict, got %+v", r.Signals)
	}
	if !r.ShouldEscalate() {
		t.Fatal("a calendar conflict should escalate by default")
	}
	s := r.Summary(3)
	if !strings.Contains(s, "팀 미팅") || !strings.Contains(s, "고객 콜") {
		t.Fatalf("conflict summary should name both events, got %q", s)
	}
}

func TestDetectSignals_AdjacentEventsNoConflict(t *testing.T) {
	cfg := DefaultSignalConfig()
	// Back-to-back (one ends exactly when next starts) is NOT a conflict.
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "A", Start: at(time.Hour), End: at(2 * time.Hour)},
		{ID: "2", Summary: "B", Start: at(2 * time.Hour), End: at(3 * time.Hour)},
	}}
	if r := DetectSignals(in, cfg); kindsOf(r)[SignalCalendarConflict] != 0 {
		t.Fatalf("adjacent events should not conflict, got %+v", r.Signals)
	}
}

func TestDetectSignals_CanceledAndAllDayIgnoredForConflict(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "취소됨", Start: at(time.Hour), End: at(3 * time.Hour), Canceled: true},
		{ID: "2", Summary: "종일", Start: at(time.Hour), End: at(3 * time.Hour), AllDay: true},
		{ID: "3", Summary: "정상", Start: at(time.Hour), End: at(3 * time.Hour)},
	}}
	// Only one timed non-canceled event → no overlapping pair.
	if r := DetectSignals(in, cfg); kindsOf(r)[SignalCalendarConflict] != 0 {
		t.Fatalf("canceled/all-day events must not form conflicts, got %+v", r.Signals)
	}
}

func TestDetectSignals_ThreeWayConflictCountsPairs(t *testing.T) {
	cfg := DefaultSignalConfig()
	// Three events all overlapping the same window → 3 pairs (1-2, 1-3, 2-3).
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "A", Start: at(time.Hour), End: at(4 * time.Hour)},
		{ID: "2", Summary: "B", Start: at(90 * time.Minute), End: at(4 * time.Hour)},
		{ID: "3", Summary: "C", Start: at(2 * time.Hour), End: at(4 * time.Hour)},
	}}
	r := DetectSignals(in, cfg)
	if got := kindsOf(r)[SignalCalendarConflict]; got != 3 {
		t.Fatalf("expected 3 conflict pairs, got %d (%+v)", got, r.Signals)
	}
}

func TestDetectSignals_ImminentEvent(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "이사회", Start: at(20 * time.Minute), End: at(80 * time.Minute), NeedsResponse: true},
		{ID: "2", Summary: "먼 일정", Start: at(5 * time.Hour), End: at(6 * time.Hour)},
	}}
	r := DetectSignals(in, cfg)
	if kindsOf(r)[SignalEventImminent] != 1 {
		t.Fatalf("expected 1 imminent event, got %+v", r.Signals)
	}
	if !r.ShouldEscalate() {
		t.Fatal("an imminent event should escalate by default")
	}
	if !strings.Contains(r.Summary(3), "미회신") {
		t.Fatalf("imminent RSVP-pending event should note 미회신, got %q", r.Summary(3))
	}
}

func TestDetectSignals_PastEventNotImminent(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{Now: signalTestNow, Events: []EventSignalInput{
		{ID: "1", Summary: "이미 시작", Start: at(-10 * time.Minute), End: at(50 * time.Minute)},
	}}
	if r := DetectSignals(in, cfg); kindsOf(r)[SignalEventImminent] != 0 {
		t.Fatalf("already-started event must not be imminent, got %+v", r.Signals)
	}
}

func TestDetectSignals_DeadlineWindow(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{Now: signalTestNow, Deadlines: []DeadlineSignalInput{
		{Label: "보고서 제출", Due: at(3 * time.Hour)}, // within 24h → signal
		{Label: "먼 마감", Due: at(48 * time.Hour)},  // outside → no signal
		{Label: "이미 지남", Due: at(-2 * time.Hour)}, // past → no signal
	}}
	r := DetectSignals(in, cfg)
	if kindsOf(r)[SignalDeadlineApproaching] != 1 {
		t.Fatalf("expected exactly 1 deadline signal, got %+v", r.Signals)
	}
	if !strings.Contains(r.Summary(3), "보고서 제출") {
		t.Fatalf("deadline summary should name the item, got %q", r.Summary(3))
	}
}

func TestDetectSignals_SummaryCapsReasonsPerKind(t *testing.T) {
	cfg := DefaultSignalConfig()
	var mail []MailSignalInput
	for i := range 5 {
		mail = append(mail, MailSignalInput{ID: string(rune('a' + i)), From: "VIP", Subject: "건", Important: true, Unread: true, ReceivedAt: at(-time.Minute)})
	}
	r := DetectSignals(SignalInputs{Now: signalTestNow, Mail: mail}, cfg)
	if kindsOf(r)[SignalVIPMailUnanswered] != 5 {
		t.Fatalf("expected 5 VIP signals scored, got %+v", r.Signals)
	}
	sum := r.Summary(3)
	lines := strings.Count(sum, "VIP 미응답 메일:")
	if lines != 4 { // 3 reasons + 1 "외 N건" rollup
		t.Fatalf("expected 3 capped reasons + 1 rollup line, got %d lines in:\n%s", lines, sum)
	}
	if !strings.Contains(sum, "외 2건") {
		t.Fatalf("expected rollup '외 2건', got:\n%s", sum)
	}
}

func TestDetectSignals_ThresholdBoundary(t *testing.T) {
	cfg := DefaultSignalConfig()
	cfg.EscalateThreshold = 30
	cfg.StaleMailAge = time.Hour
	cfg.StaleMailWeight = 30
	// Exactly at threshold → escalate (>=).
	in := SignalInputs{Now: signalTestNow, Mail: []MailSignalInput{
		{ID: "1", From: "x", Unread: true, ReceivedAt: at(-2 * time.Hour)},
	}}
	r := DetectSignals(in, cfg)
	if r.Score != 30 || !r.ShouldEscalate() {
		t.Fatalf("score==threshold should escalate; score=%d escalate=%v", r.Score, r.ShouldEscalate())
	}
}

func TestDetectSignals_MixedScore(t *testing.T) {
	cfg := DefaultSignalConfig()
	in := SignalInputs{
		Now: signalTestNow,
		Mail: []MailSignalInput{
			{ID: "1", From: "대표", Subject: "긴급", Important: true, Unread: true, ReceivedAt: at(-30 * time.Minute)},
		},
		Events: []EventSignalInput{
			{ID: "e1", Summary: "A", Start: at(time.Hour), End: at(2 * time.Hour)},
			{ID: "e2", Summary: "B", Start: at(90 * time.Minute), End: at(3 * time.Hour)},
		},
		Deadlines: []DeadlineSignalInput{{Label: "납기", Due: at(2 * time.Hour)}},
	}
	r := DetectSignals(in, cfg)
	want := cfg.VIPMailWeight + cfg.ConflictWeight + cfg.DeadlineWeight
	if r.Score != want {
		t.Fatalf("mixed score = %d, want %d (%+v)", r.Score, want, r.Signals)
	}
	if !r.ShouldEscalate() {
		t.Fatal("mixed high-value signals should escalate")
	}
	// Summary ordering: VIP first, then conflict, then deadline.
	sum := r.Summary(0)
	iVIP := strings.Index(sum, "VIP 미응답 메일")
	iConf := strings.Index(sum, "일정 충돌")
	iDead := strings.Index(sum, "마감 임박")
	if iVIP < 0 || iConf <= iVIP || iDead <= iConf {
		t.Fatalf("summary kind ordering wrong:\n%s", sum)
	}
}

func TestDetectSignals_ZeroWindowsDisableRules(t *testing.T) {
	cfg := DefaultSignalConfig()
	cfg.StaleMailAge = 0
	cfg.ImminentEventWindow = 0
	cfg.DeadlineWindow = 0
	in := SignalInputs{
		Now:       signalTestNow,
		Mail:      []MailSignalInput{{ID: "1", From: "x", Unread: true, ReceivedAt: at(-100 * time.Hour)}},
		Events:    []EventSignalInput{{ID: "e", Summary: "곧", Start: at(time.Minute), End: at(time.Hour)}},
		Deadlines: []DeadlineSignalInput{{Label: "곧 마감", Due: at(time.Minute)}},
	}
	r := DetectSignals(in, cfg)
	// Stale/imminent/deadline all disabled; no important mail; no conflict → empty.
	if len(r.Signals) != 0 {
		t.Fatalf("zero windows should disable stale/imminent/deadline, got %+v", r.Signals)
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "1분"},
		{25 * time.Minute, "25분"},
		{90 * time.Minute, "1시간"},
		{5 * time.Hour, "5시간"},
		{26 * time.Hour, "1일"},
		{72 * time.Hour, "3일"},
		{-45 * time.Minute, "45분"}, // negative normalized
	}
	for _, c := range cases {
		if got := humanizeDuration(c.d); got != c.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
