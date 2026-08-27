package cron

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestNormalizeJobInputContract(t *testing.T) {
	NormalizeJobInput(nil)
	created := time.Now().Add(-time.Hour).UnixMilli()
	job := StoreJob{
		AgentID:     " agent ",
		Schedule:    StoreSchedule{Kind: " CRON ", Expr: " 0 9 * * * ", Tz: " Asia/Seoul ", StaggerMs: -1},
		Payload:     StorePayload{Kind: " AGENTTURN ", Message: " command ", Model: " model ", Thinking: " high ", TimeoutSeconds: -1},
		Delivery:    &JobDeliveryConfig{Channel: " Deliver ", To: " target ", AccountID: " account "},
		CreatedAtMs: created,
	}
	before := time.Now().UnixMilli()
	NormalizeJobInput(&job)
	after := time.Now().UnixMilli()
	if job.Schedule.Kind != "cron" || job.Schedule.Expr != "0 9 * * *" || job.Schedule.Tz != "Asia/Seoul" || job.Schedule.StaggerMs != 0 {
		t.Fatalf("schedule = %+v", job.Schedule)
	}
	if job.Payload.Kind != "agentTurn" || job.Payload.Message != "command" || job.Payload.Model != "model" || job.Payload.Thinking != "high" || job.Payload.TimeoutSeconds != 0 {
		t.Fatalf("payload = %+v", job.Payload)
	}
	if job.Delivery.Channel != "" || job.Delivery.To != "target" || job.Delivery.AccountID != "account" || job.AgentID != "agent" {
		t.Fatalf("delivery/agent = %+v/%q", job.Delivery, job.AgentID)
	}
	if job.CreatedAtMs != created || job.UpdatedAtMs < before || job.UpdatedAtMs > after {
		t.Fatalf("timestamps = %d/%d", job.CreatedAtMs, job.UpdatedAtMs)
	}
}

func TestNormalizeJobInputInferenceMatrix(t *testing.T) {
	for _, tt := range []struct {
		name         string
		schedule     StoreSchedule
		payload      StorePayload
		wantSchedule string
		wantPayload  string
	}{
		{name: "at and message", schedule: StoreSchedule{Kind: "unknown", At: "2027-01-01"}, payload: StorePayload{Message: "run"}, wantSchedule: "at", wantPayload: "agentTurn"},
		{name: "every and event", schedule: StoreSchedule{EveryMs: 1000}, payload: StorePayload{Text: "event"}, wantSchedule: "every", wantPayload: "systemEvent"},
		{name: "cron and model", schedule: StoreSchedule{Expr: "0 * * * *"}, payload: StorePayload{Model: "m"}, wantSchedule: "cron", wantPayload: "agentTurn"},
		{name: "thinking infers", payload: StorePayload{Thinking: "minimal"}, wantPayload: "agentTurn"},
		{name: "whitespace does not infer", schedule: StoreSchedule{At: " "}, payload: StorePayload{Message: " ", Text: " ", Model: " ", Thinking: " "}},
		{name: "system case", payload: StorePayload{Kind: "SYSTEMEVENT"}, wantPayload: "systemEvent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			job := StoreJob{Schedule: tt.schedule, Payload: tt.payload}
			NormalizeJobInput(&job)
			if job.Schedule.Kind != tt.wantSchedule || job.Payload.Kind != tt.wantPayload {
				t.Fatalf("kinds = %q/%q, want %q/%q", job.Schedule.Kind, job.Payload.Kind, tt.wantSchedule, tt.wantPayload)
			}
		})
	}
}

func TestInferLegacyNameContractAdditional(t *testing.T) {
	longKorean := strings.Repeat("가", 30)
	for _, tt := range []struct {
		name string
		job  StoreJob
		want string
	}{
		{name: "system first line", job: StoreJob{Payload: StorePayload{Kind: "systemEvent", Text: " event title \nrest"}}, want: "event title"},
		{name: "agent first line", job: StoreJob{Payload: StorePayload{Kind: "agentTurn", Message: " command \nrest"}}, want: "command"},
		{name: "unicode truncation", job: StoreJob{Payload: StorePayload{Kind: "agentTurn", Message: longKorean}}, want: strings.Repeat("가", 19) + "…"},
		{name: "cron", job: StoreJob{Schedule: StoreSchedule{Kind: "cron", Expr: "0 9 * * *"}}, want: "Cron: 0 9 * * *"},
		{name: "every", job: StoreJob{Schedule: StoreSchedule{Kind: "every", EveryMs: 5000}}, want: "Every: 5000ms"},
		{name: "at", job: StoreJob{Schedule: StoreSchedule{Kind: "at"}}, want: "One-shot"},
		{name: "fallback", job: StoreJob{}, want: "Cron job"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := InferLegacyName(tt.job)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("InferLegacyName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCronFieldValidationAndExpansion(t *testing.T) {
	for _, tt := range []struct {
		name  string
		field string
		lo    int
		hi    int
		want  []int
		valid bool
	}{
		{name: "wildcard", field: "*", lo: 0, hi: 3, want: []int{0, 1, 2, 3}, valid: true},
		{name: "list", field: "1,3,5", lo: 0, hi: 5, want: []int{1, 3, 5}, valid: true},
		{name: "range", field: "2-4", lo: 0, hi: 5, want: []int{2, 3, 4}, valid: true},
		{name: "step wildcard", field: "*/2", lo: 0, hi: 6, want: []int{0, 2, 4, 6}, valid: true},
		{name: "step range", field: "1-5/2", lo: 0, hi: 6, want: []int{1, 3, 5}, valid: true},
		{name: "single start step", field: "2/2", lo: 0, hi: 6, want: []int{2, 4, 6}, valid: true},
		{name: "fixed low", field: "0", lo: 0, hi: 5, want: []int{0}, valid: true},
		{name: "fixed high", field: "5", lo: 0, hi: 5, want: []int{5}, valid: true},
		{name: "below", field: "-1", lo: 0, hi: 5},
		{name: "above", field: "6", lo: 0, hi: 5},
		{name: "range above", field: "1-9", lo: 0, hi: 5},
		{name: "range reverse", field: "4-2", lo: 0, hi: 5},
		{name: "step zero", field: "*/0", lo: 0, hi: 5},
		{name: "step negative", field: "*/-1", lo: 0, hi: 5},
		{name: "step bad", field: "*/x", lo: 0, hi: 5},
		{name: "range bad", field: "a-b", lo: 0, hi: 5},
		{name: "empty", field: "", lo: 0, hi: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCronField(tt.field, tt.lo, tt.hi)
			if (got != nil) != tt.valid {
				t.Fatalf("valid = %v, want %v (%v)", got != nil, tt.valid, got)
			}
			if !tt.valid {
				return
			}
			var values []int
			for value := range got {
				values = append(values, value)
			}
			sort.Ints(values)
			if !reflect.DeepEqual(values, tt.want) {
				t.Fatalf("values = %v, want %v", values, tt.want)
			}
		})
	}
}

func TestComputeNextEveryMsReturnsFutureTickAndGuardsOverflow(t *testing.T) {
	for _, tt := range []struct {
		name   string
		now    int64
		anchor int64
		every  int64
		want   int64
	}{
		{name: "implicit anchor", now: 1000, every: 100, want: 1100},
		{name: "before anchor", now: 900, anchor: 1000, every: 100, want: 1000},
		{name: "at anchor", now: 1000, anchor: 1000, every: 100, want: 1100},
		{name: "between", now: 1049, anchor: 1000, every: 100, want: 1100},
		{name: "exact boundary", now: 1100, anchor: 1000, every: 100, want: 1200},
		{name: "invalid zero", now: 1000, every: 0},
		{name: "invalid negative", now: 1000, every: -1},
		{name: "overflow", now: math.MaxInt64, anchor: 1, every: math.MaxInt64 / 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := computeNextEveryMs(StoreSchedule{EveryMs: tt.every, AnchorMs: tt.anchor}, tt.now)
			if got != tt.want {
				t.Fatalf("next = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEvaluateCronAliasesNamesSundaySevenAndInvalidBounds(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 11, 10, 30, 20, 0, loc) // Saturday
	for _, tt := range []struct {
		name string
		expr string
		want time.Time
	}{
		{name: "hourly", expr: "@hourly", want: time.Date(2026, 7, 11, 11, 0, 0, 0, loc)},
		{name: "daily", expr: "@daily", want: time.Date(2026, 7, 12, 0, 0, 0, 0, loc)},
		{name: "weekly", expr: "@weekly", want: time.Date(2026, 7, 12, 0, 0, 0, 0, loc)},
		{name: "sunday seven", expr: "0 9 * * 7", want: time.Date(2026, 7, 12, 9, 0, 0, 0, loc)},
		{name: "named day", expr: "0 9 * * SUN", want: time.Date(2026, 7, 12, 9, 0, 0, 0, loc)},
		{name: "named month", expr: "0 0 1 AUG *", want: time.Date(2026, 8, 1, 0, 0, 0, 0, loc)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := evaluateCronExpr(tt.expr, now, loc); !got.Equal(tt.want) {
				t.Fatalf("next = %v, want %v", got, tt.want)
			}
		})
	}
	for _, expr := range []string{"", "* * *", "60 * * * *", "0 24 * * *", "0 0 0 * *", "0 0 * 13 *", "0 0 * * 8", "0-99 * * * *", "*/0 * * * *", "bad * * * *"} {
		if got := evaluateCronExpr(expr, now, loc); !got.IsZero() {
			t.Errorf("invalid %q = %v", expr, got)
		}
	}
}

func TestParseAbsoluteTimeContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  int64
	}{
		{input: "1700000000000", want: 1700000000000},
		{input: " 1700000000000 ", want: 1700000000000},
		{input: "1700000000000.9", want: 1700000000000},
		{input: "1970-01-02", want: 86_400_000},
		{input: "1970-01-01T00:00:01Z", want: 1000},
		{input: "1970-01-01T00:00:01", want: 1000},
		{input: ""},
		{input: "0"},
		{input: "-1"},
		{input: "NaN"},
		{input: "+Inf"},
		{input: "bad"},
	} {
		if got := parseAbsoluteTimeMs(tt.input); got != tt.want {
			t.Errorf("parseAbsoluteTimeMs(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestFormatCronExprKoreanContract(t *testing.T) {
	for _, tt := range []struct{ expr, want string }{
		{expr: "@yearly", want: "매년 1월 1일 00:00"},
		{expr: "@annually", want: "매년 1월 1일 00:00"},
		{expr: "@monthly", want: "매월 1일 00:00"},
		{expr: "@weekly", want: "매주 일요일 00:00"},
		{expr: "@daily", want: "매일 00:00"},
		{expr: "@midnight", want: "매일 00:00"},
		{expr: "@hourly", want: "매시 정각"},
		{expr: "*/5 * * * *", want: "5분마다"},
		{expr: "0 */2 * * *", want: "2시간마다"},
		{expr: "30 9 * * *", want: "매일 09:30"},
		{expr: "30 9 * * 1-5", want: "평일 09:30"},
		{expr: "30 9 * * mon-fri", want: "평일 09:30"},
		{expr: "30 9 * * 0,6", want: "주말 09:30"},
		{expr: "30 9 * * mon", want: "매주 월요일 09:30"},
		{expr: "30 9 15 * *", want: "매월 15일 09:30"},
		{expr: "0 * * * *", want: "매시 정각"},
		{expr: "15 * * * *", want: "매시 15분"},
		{expr: "bad", want: "cron: bad"},
		{expr: "x x * * *", want: "cron: x x * * *"},
	} {
		if got := formatCronExprKorean(tt.expr); got != tt.want {
			t.Errorf("formatCronExprKorean(%q) = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

func TestFormatHumanScheduleContract(t *testing.T) {
	anchor := time.Date(2026, 7, 11, 8, 30, 0, 0, time.Local).UnixMilli()
	for _, tt := range []struct {
		name string
		s    StoreSchedule
		want string
	}{
		{name: "at empty", s: StoreSchedule{Kind: "at"}, want: "일회성"},
		{name: "at parsed", s: StoreSchedule{Kind: "at", At: "2026-07-11T08:30:00Z"}, want: time.UnixMilli(parseAbsoluteTimeMs("2026-07-11T08:30:00Z")).Format("2006-01-02 15:04") + " (일회성)"},
		{name: "at raw", s: StoreSchedule{Kind: "at", At: "someday"}, want: "someday (일회성)"},
		{name: "every invalid", s: StoreSchedule{Kind: "every"}, want: "반복"},
		{name: "every", s: StoreSchedule{Kind: "every", EveryMs: 90_000}, want: "1분 30초마다"},
		{name: "every anchor", s: StoreSchedule{Kind: "every", EveryMs: 60_000, AnchorMs: anchor}, want: "1분마다 (08:30 기준)"},
		{name: "cron local", s: StoreSchedule{Kind: "cron", Expr: "0 9 * * *"}, want: "매일 09:00"},
		{name: "cron utc", s: StoreSchedule{Kind: "cron", Expr: "0 9 * * *", Tz: "UTC"}, want: "매일 09:00 (UTC)"},
		{name: "unknown", s: StoreSchedule{Kind: "custom"}, want: "custom"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatHumanSchedule(tt.s); got != tt.want {
				t.Fatalf("FormatHumanSchedule = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDurationKoreanBoundariesAndNoOverflow(t *testing.T) {
	for _, tt := range []struct {
		ms   int64
		want string
	}{
		{ms: -1, want: "0초"},
		{ms: 0, want: "0초"},
		{ms: 999, want: "0초"},
		{ms: 1000, want: "1초"},
		{ms: 59_999, want: "59초"},
		{ms: 60_000, want: "1분"},
		{ms: 61_000, want: "1분 1초"},
		{ms: 3_600_000, want: "1시간"},
		{ms: 3_661_000, want: "1시간 1분"},
		{ms: 86_400_000, want: "1일"},
		{ms: 90_061_000, want: "1일 1시간 1분"},
	} {
		if got := FormatDurationKorean(tt.ms); got != tt.want {
			t.Errorf("FormatDurationKorean(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
	got := FormatDurationKorean(math.MaxInt64)
	if got == "0초" || strings.HasPrefix(got, "-") {
		t.Fatalf("max duration overflowed: %q", got)
	}
}

func TestParseSmartScheduleContractAdditional(t *testing.T) {
	anchor := "2026-07-11T08:30:00Z"
	for _, tt := range []struct {
		name    string
		spec    string
		opts    SmartScheduleOpts
		kind    string
		every   int64
		expr    string
		at      string
		wantErr string
	}{
		{name: "raw ms", spec: "5000", kind: "every", every: 5000},
		{name: "duration", spec: "30s", kind: "every", every: 30_000},
		{name: "every duration", spec: " Every 5m ", kind: "every", every: 300_000},
		{name: "interval anchor", spec: "1h", opts: SmartScheduleOpts{AnchorTime: anchor}, kind: "every", every: 3_600_000},
		{name: "alias", spec: "@DAILY", opts: SmartScheduleOpts{Tz: "Asia/Seoul", StaggerMs: 100}, kind: "cron", expr: "@daily"},
		{name: "cron", spec: "0 9 * * MON-FRI", kind: "cron", expr: "0 9 * * MON-FRI"},
		{name: "timestamp", spec: "2026-07-11T08:30:00Z", kind: "at", at: "2026-07-11T08:30:00Z"},
		{name: "date", spec: "2026-07-11", kind: "at", at: "2026-07-11"},
		{name: "empty", spec: " ", wantErr: "empty schedule"},
		{name: "bad timezone", spec: "@daily", opts: SmartScheduleOpts{Tz: "Mars/Olympus"}, wantErr: "invalid timezone"},
		{name: "bad anchor", spec: "1h", opts: SmartScheduleOpts{AnchorTime: "bad"}, wantErr: "invalid anchor"},
		{name: "negative", spec: "-1s", wantErr: "must be positive"},
		{name: "unknown", spec: "someday", wantErr: "unrecognized"},
		{name: "invalid cron bounds", spec: "60 * * * *", wantErr: "invalid cron"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSmartScheduleWithOpts(tt.spec, tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got.Kind != tt.kind || got.EveryMs != tt.every || got.Expr != tt.expr || got.At != tt.at {
				t.Fatalf("schedule = %+v/%v", got, err)
			}
			if tt.opts.AnchorTime != "" && got.AnchorMs != parseAbsoluteTimeMs(tt.opts.AnchorTime) {
				t.Fatalf("anchor = %d", got.AnchorMs)
			}
		})
	}
}

func TestLooksLikeCronExprDetectsFieldFormatMatrix(t *testing.T) {
	for _, tt := range []struct {
		fields []string
		want   bool
	}{
		{fields: []string{"0", "9", "*", "*", "MON-FRI"}, want: true},
		{fields: []string{"*/5", "*", "*", "*", "*"}, want: true},
		{fields: []string{"0", "9", "?", "*", "*"}},
		{fields: []string{"0", "9", "한", "*", "*"}},
		{fields: nil, want: true},
	} {
		if got := looksLikeCronExpr(tt.fields); got != tt.want {
			t.Errorf("looksLikeCronExpr(%v) = %v", tt.fields, got)
		}
	}
}

func TestPickSummaryAndTruncateUTF8Contracts(t *testing.T) {
	if got := PickSummaryFromOutput(" \n "); got != "" {
		t.Fatalf("blank summary = %q", got)
	}
	if got := PickSummaryFromOutput("  summary  "); got != "summary" {
		t.Fatalf("summary = %q", got)
	}
	long := strings.Repeat("가", summaryMaxChars+2)
	got := PickSummaryFromOutput(long)
	if utf8.RuneCountInString(got) != summaryMaxChars+1 || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Fatalf("long summary runes=%d suffix=%v", utf8.RuneCountInString(got), strings.HasSuffix(got, "…"))
	}
	for _, tt := range []struct {
		max  int
		want string
	}{{max: -1}, {max: 0}, {max: 1, want: "가"}, {max: 2, want: "가나"}, {max: 9, want: "가나다"}} {
		if got := truncateUTF8("가나다", tt.max); got != tt.want {
			t.Errorf("truncateUTF8(%d) = %q", tt.max, got)
		}
	}
}

func TestShouldSendFailureAlertContractAdditional(t *testing.T) {
	now := int64(10_000)
	alert := &CronFailureAlert{After: 3, CooldownMs: 1000}
	for _, tt := range []struct {
		name   string
		state  JobState
		alert  *CronFailureAlert
		status string
		want   bool
	}{
		{name: "success", state: JobState{ConsecutiveErrors: 3}, alert: alert, status: "ok"},
		{name: "nil", state: JobState{ConsecutiveErrors: 3}, status: "error"},
		{name: "below threshold", state: JobState{ConsecutiveErrors: 2}, alert: alert, status: "error"},
		{name: "threshold", state: JobState{ConsecutiveErrors: 3}, alert: alert, status: "error", want: true},
		{name: "cooldown", state: JobState{ConsecutiveErrors: 3, LastFailureAlertAtMs: now - 999}, alert: alert, status: "error"},
		{name: "cooldown edge", state: JobState{ConsecutiveErrors: 3, LastFailureAlertAtMs: now - 1000}, alert: alert, status: "error", want: true},
		{name: "no gates", state: JobState{}, alert: &CronFailureAlert{}, status: "error", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSendFailureAlert(tt.state, tt.alert, tt.status, now); got != tt.want {
				t.Fatalf("ShouldSendFailureAlert = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKoreanFailureCauseContract(t *testing.T) {
	for input, want := range map[string]string{
		"":                              "원인 미상",
		"job already running":           "이미 실행 중이어서 건너뜀",
		"concurrent execution rejected": "이미 실행 중이어서 건너뜀",
		"delivery target error":         "결과 전달 실패",
		"no agent runner configured":    "실행기가 구성되지 않음",
		"context deadline exceeded":     "시간 초과",
		"timeout while waiting":         "시간 초과",
		"connection refused":            "백엔드 연결 실패",
		"something else":                "내부 오류",
	} {
		if got := koreanFailureCause(input); got != want {
			t.Errorf("koreanFailureCause(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolveJobCommandBestEffortAndSafeStrIgnoresNilTarget(t *testing.T) {
	if got := resolveJobCommand(StoreJob{Payload: StorePayload{Kind: "systemEvent", Text: "event", Message: "message"}}); got != "event" {
		t.Fatalf("system command = %q", got)
	}
	if got := resolveJobCommand(StoreJob{Payload: StorePayload{Kind: "agentTurn", Text: "event", Message: "message"}}); got != "message" {
		t.Fatalf("agent command = %q", got)
	}
	if isBestEffort(nil) || isBestEffort(&JobDeliveryConfig{}) || !isBestEffort(&JobDeliveryConfig{BestEffort: true}) {
		t.Fatal("isBestEffort contract failed")
	}
	if got := safeStr(nil, func(*DeliveryTarget) string { panic("must not call") }); got != "" {
		t.Fatalf("safeStr nil = %q", got)
	}
	target := &DeliveryTarget{Channel: "native"}
	if got := safeStr(target, func(t *DeliveryTarget) string { return t.Channel }); got != "native" {
		t.Fatalf("safeStr = %q", got)
	}
}

func TestSortJobsAscendingDescendingPreservesStableTies(t *testing.T) {
	base := []StoreJob{
		{ID: "b", Name: "beta", UpdatedAtMs: 20, State: JobState{NextRunAtMs: 200}},
		{ID: "a1", Name: "Alpha", UpdatedAtMs: 10, State: JobState{NextRunAtMs: 100}},
		{ID: "a2", Name: "alpha", UpdatedAtMs: 10, State: JobState{NextRunAtMs: 100}},
		{ID: "none", Name: "none", UpdatedAtMs: 30},
	}
	ids := func(jobs []StoreJob) []string {
		out := make([]string, len(jobs))
		for i := range jobs {
			out[i] = jobs[i].ID
		}
		return out
	}
	for _, tt := range []struct {
		by, dir string
		want    []string
	}{
		{by: "name", want: []string{"a1", "a2", "b", "none"}},
		{by: "name", dir: "desc", want: []string{"none", "b", "a1", "a2"}},
		{by: "updatedAtMs", want: []string{"a1", "a2", "b", "none"}},
		{by: "updatedAtMs", dir: "desc", want: []string{"none", "b", "a1", "a2"}},
		{by: "nextRunAtMs", want: []string{"a1", "a2", "b", "none"}},
		{by: "nextRunAtMs", dir: "desc", want: []string{"none", "b", "a1", "a2"}},
		{by: "unknown", want: []string{"a1", "a2", "b", "none"}},
	} {
		jobs := append([]StoreJob(nil), base...)
		sortJobs(jobs, tt.by, tt.dir)
		if got := ids(jobs); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("sort %s/%s = %v, want %v", tt.by, tt.dir, got, tt.want)
		}
	}
}

func contractCronJob(id, name string, enabled bool, next int64) StoreJob {
	return StoreJob{ID: id, Name: name, Enabled: enabled, Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000}, Payload: StorePayload{Kind: "agentTurn", Message: "run " + name}, State: JobState{NextRunAtMs: next}}
}

func TestServiceCreateListUpdateDeleteEmitsEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron", "jobs.json")
	svc := NewService(ServiceConfig{StorePath: path}, nil, nil)
	var mu sync.Mutex
	var events []CronEvent
	svc.OnEvent(func(event CronEvent) { mu.Lock(); events = append(events, event); mu.Unlock() })
	for _, job := range []StoreJob{
		contractCronJob("a", "Alpha", true, 300),
		contractCronJob("b", "Beta", false, 100),
		contractCronJob("c", "Gamma", true, 200),
	} {
		if err := svc.Add(context.Background(), job); err != nil {
			t.Fatalf("Add %s: %v", job.ID, err)
		}
	}
	all, err := svc.List(nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("List all = %d/%v", len(all), err)
	}
	enabled, err := svc.List(&ListOptions{})
	if err != nil || len(enabled) != 2 {
		t.Fatalf("List enabled = %d/%v", len(enabled), err)
	}
	page := svc.ListPage(ListPageOptions{Limit: 1, Offset: 1, SortBy: "name", Query: "a"})
	if page.Total != 2 || page.Limit != 1 || page.Offset != 1 || len(page.Jobs) != 1 || page.Jobs[0].ID != "c" || page.HasMore {
		t.Fatalf("page = %+v", page)
	}
	if got := svc.ListPage(ListPageOptions{Offset: 99}); got.Total != 2 || len(got.Jobs) != 0 || got.Offset != 99 || got.Limit != 50 {
		t.Fatalf("past page = %+v", got)
	}
	if got := svc.Job("a"); got == nil || got.Name != "Alpha" {
		t.Fatalf("Job = %+v", got)
	}
	if got, err := svc.JobByName("Gamma"); err != nil || got == nil || got.ID != "c" {
		t.Fatalf("JobByName = %+v/%v", got, err)
	}
	if err := svc.Update(context.Background(), "a", func(job *StoreJob) { job.Name = "Updated"; job.Payload.Message = " updated " }); err != nil {
		t.Fatal(err)
	}
	if got := svc.Job("a"); got == nil || got.Name != "Updated" || got.Payload.Message != "updated" {
		t.Fatalf("updated = %+v", got)
	}
	if err := svc.Update(context.Background(), "missing", func(*StoreJob) {}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing Update = %v", err)
	}
	if err := svc.Remove("b"); err != nil {
		t.Fatal(err)
	}
	if svc.Job("b") != nil {
		t.Fatal("removed job remains")
	}
	status := svc.Status()
	if status.Running || status.TaskCount != 2 {
		t.Fatalf("status = %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	var types []string
	for _, event := range events {
		types = append(types, event.Type)
		if event.Ts <= 0 {
			t.Errorf("event timestamp = %d", event.Ts)
		}
	}
	if !reflect.DeepEqual(types, []string{"job_added", "job_added", "job_added", "job_removed"}) {
		t.Fatalf("events = %v", types)
	}
}

func TestServiceAddRejectsPastScheduleAndInfersName(t *testing.T) {
	svc := NewService(ServiceConfig{StorePath: filepath.Join(t.TempDir(), "jobs.json")}, nil, nil)
	if err := svc.Add(context.Background(), StoreJob{ID: "past", Enabled: true, Schedule: StoreSchedule{Kind: "at", At: "2000-01-01T00:00:00Z"}, Payload: StorePayload{Kind: "agentTurn", Message: "past"}}); err == nil || !strings.Contains(err.Error(), "invalid schedule") {
		t.Fatalf("past Add = %v", err)
	}
	job := StoreJob{ID: "named", Enabled: true, Schedule: StoreSchedule{Kind: "every", EveryMs: 60_000}, Payload: StorePayload{Kind: "agentTurn", Message: "  first line\nsecond  "}}
	if err := svc.Add(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	got := svc.Job("named")
	if got == nil || got.Name != "first line" || got.State.NextRunAtMs <= time.Now().UnixMilli() || got.CreatedAtMs == 0 || got.UpdatedAtMs == 0 {
		t.Fatalf("job = %+v", got)
	}
}

type contractAgentRunner struct {
	output string
	err    error
	params chan AgentTurnParams
}

func (r *contractAgentRunner) RunAgentTurn(_ context.Context, params AgentTurnParams) (string, error) {
	if r.params != nil {
		r.params <- params
	}
	return r.output, r.err
}

func TestServiceRunMissingDueSkipAndImmediate(t *testing.T) {
	runner := &contractAgentRunner{output: "done", params: make(chan AgentTurnParams, 1)}
	svc := NewService(ServiceConfig{StorePath: filepath.Join(t.TempDir(), "jobs.json"), DefaultChannel: "native", DefaultTo: "main"}, runner, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := svc.Run(context.Background(), "missing", "manual"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing Run = %v", err)
	}
	job := contractCronJob("future", "Future", true, time.Now().Add(time.Hour).UnixMilli())
	job.Delivery = nil
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Run(context.Background(), "future", "due"); err != nil || got.Status != "skipped" {
		t.Fatalf("due Run = %+v/%v", got, err)
	}
	select {
	case <-runner.params:
		t.Fatal("due skip called agent")
	default:
	}
	got, err := svc.Run(context.Background(), "future", "manual")
	if err != nil || got.Status != "ok" || got.Output != "done" {
		t.Fatalf("manual Run = %+v/%v", got, err)
	}
	select {
	case params := <-runner.params:
		if params.Command != "run Future" || params.AgentID != "" || params.SessionKind != session.KindCron {
			t.Fatalf("params = %+v", params)
		}
	case <-time.After(time.Second):
		t.Fatal("agent not called")
	}
}

func TestServiceSettersUpdateConfigAndLateListenerSkipsCurrentEmit(t *testing.T) {
	svc := NewService(ServiceConfig{StorePath: filepath.Join(t.TempDir(), "jobs.json")}, nil, nil)
	runner := &contractAgentRunner{}
	svc.SetAgentRunner(runner)
	svc.SetSubagentPoller(nil)
	svc.SetTranscriptCloner(nil, "client:main")
	handoff := func(context.Context, string, string, string, string) (bool, error) { return true, nil }
	svc.SetMainSessionHandoff(handoff)
	svc.mu.Lock()
	if svc.agent != runner || svc.cfg.MainSessionKey != "client:main" || svc.cfg.MainSessionHandoff == nil {
		t.Fatalf("setters = %+v", svc.cfg)
	}
	svc.mu.Unlock()
	var calls []string
	svc.OnEvent(func(event CronEvent) {
		calls = append(calls, "first:"+event.Type)
		svc.OnEvent(func(event CronEvent) { calls = append(calls, "late:"+event.Type) })
	})
	svc.OnEvent(func(event CronEvent) { calls = append(calls, "second:"+event.Type) })
	svc.emit(CronEvent{Type: "one"})
	svc.emit(CronEvent{Type: "two"})
	want := []string{"first:one", "second:one", "first:two", "second:two", "late:two"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("listener calls = %v", calls)
	}
}

func TestSendFailureAlertWithoutHandoffDoesNotPanic(t *testing.T) {
	svc := NewService(ServiceConfig{StorePath: filepath.Join(t.TempDir(), "jobs.json"), DefaultChannel: "native", DefaultTo: "main"}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	job := contractCronJob("failed", "Failure", true, 0)
	job.FailureAlert = &CronFailureAlert{}
	job.State.ConsecutiveErrors = 3
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	svc.sendFailureAlert(context.Background(), job, RunOutcome{Status: "error", Error: "timeout"})
	got := svc.store.Job(job.ID)
	if got.State.LastFailureAlertAtMs != 0 {
		t.Fatalf("failed delivery should not advance cooldown: %+v", got.State)
	}
}

func TestSendFailureAlertHandoffAndCooldownPersistence(t *testing.T) {
	var gotText string
	var gotFields []string
	svc := NewService(ServiceConfig{
		StorePath: filepath.Join(t.TempDir(), "jobs.json"), DefaultChannel: "native", DefaultTo: "main",
		MainSessionHandoff: func(_ context.Context, channel, to, jobID, text string) (bool, error) {
			gotFields = []string{channel, to, jobID}
			gotText = text
			return true, nil
		},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	job := contractCronJob("failed", "Important", true, 0)
	job.FailureAlert = &CronFailureAlert{}
	job.State.ConsecutiveErrors = 4
	if err := svc.store.AddJob(job); err != nil {
		t.Fatal(err)
	}
	svc.sendFailureAlert(context.Background(), job, RunOutcome{Status: "error", Error: "connection refused"})
	if !reflect.DeepEqual(gotFields, []string{"native", "main", "failed"}) || !strings.Contains(gotText, "Important") || !strings.Contains(gotText, "백엔드 연결 실패") {
		t.Fatalf("handoff = %v %q", gotFields, gotText)
	}
	if got := svc.store.Job(job.ID); got == nil || got.State.LastFailureAlertAtMs <= 0 {
		t.Fatalf("cooldown not persisted: %+v", got)
	}
}

func TestStoreDefaultPathCloneAndCorruptLoad(t *testing.T) {
	// The argument is a resolved STATE dir, not a home dir: production passes
	// $HOME/.deneb and gets the same path as before, while DENEB_STATE_DIR keeps
	// a dev gateway off the operator's real schedule.
	if got := DefaultCronStorePath("/home/user/.deneb"); got != filepath.Join("/home/user/.deneb", "cron", "jobs.json") {
		t.Fatalf("default path = %q", got)
	}
	if got := DefaultCronStorePath("/tmp/deneb-dev-state"); got != filepath.Join("/tmp/deneb-dev-state", "cron", "jobs.json") {
		t.Fatalf("dev state path = %q", got)
	}
	if cloneStoreFile(nil) != nil {
		t.Fatal("nil clone nonnil")
	}
	orig := &CronStoreFile{Version: 1, Jobs: []StoreJob{{ID: "one"}}}
	clone := cloneStoreFile(orig)
	clone.Jobs[0].ID = "changed"
	clone.Jobs = append(clone.Jobs, StoreJob{ID: "two"})
	if orig.Jobs[0].ID != "one" || len(orig.Jobs) != 1 {
		t.Fatalf("clone aliased original: %+v", orig)
	}
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "parse cron store") {
		t.Fatalf("corrupt Load = %v", err)
	}
}

func TestStoreLoadDefaultsVersionAndReturnsIsolatedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	data, _ := json.Marshal(CronStoreFile{Jobs: nil})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || loaded.Jobs == nil || len(loaded.Jobs) != 0 {
		t.Fatalf("loaded = %+v", loaded)
	}
	loaded.Jobs = append(loaded.Jobs, StoreJob{ID: "mutated"})
	again, err := store.Load()
	if err != nil || len(again.Jobs) != 0 {
		t.Fatalf("snapshot mutation leaked = %+v/%v", again, err)
	}
}

func TestStoreMutationErrorsAndIdempotence(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "jobs.json"))
	if err := store.AddJob(contractCronJob("one", "One", true, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetJobEnabled("one", false); err != nil {
		t.Fatal(err)
	}
	if got := store.Job("one"); got == nil || got.Enabled {
		t.Fatalf("enabled = %+v", got)
	}
	state := JobState{ConsecutiveErrors: 2, PendingRerun: true}
	if err := store.UpdateJobState("one", state); err != nil {
		t.Fatal(err)
	}
	if got := store.Job("one"); got == nil || got.State != state {
		t.Fatalf("state = %+v", got)
	}
	for _, err := range []error{store.SetJobEnabled("missing", true), store.UpdateJobState("missing", JobState{})} {
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("missing error = %v", err)
		}
	}
	if err := store.RemoveJob("missing"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveJob("one"); err != nil {
		t.Fatal(err)
	}
	if store.Job("one") != nil {
		t.Fatal("removed remains")
	}
}

func TestCompareInt64Contract(t *testing.T) {
	for _, tt := range []struct {
		a, b int64
		want int
	}{{1, 2, -1}, {2, 1, 1}, {2, 2, 0}, {math.MinInt64, math.MaxInt64, -1}, {math.MaxInt64, math.MinInt64, 1}} {
		if got := compareInt64(tt.a, tt.b); got != tt.want {
			t.Errorf("compareInt64(%d,%d) = %d", tt.a, tt.b, got)
		}
	}
}
