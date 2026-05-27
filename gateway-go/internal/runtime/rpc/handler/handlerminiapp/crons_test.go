package handlerminiapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeCronLister struct {
	listPageFn func(opts cron.ListPageOptions) cron.ListPageResult
}

func (f *fakeCronLister) ListPage(opts cron.ListPageOptions) cron.ListPageResult {
	if f.listPageFn == nil {
		return cron.ListPageResult{}
	}
	return f.listPageFn(opts)
}

func cronsDepsFor(svc CronLister) CronsDeps {
	return CronsDeps{Service: func() (CronLister, error) { return svc, nil }}
}

func TestCronsList_HappyPath(t *testing.T) {
	svc := &fakeCronLister{
		listPageFn: func(opts cron.ListPageOptions) cron.ListPageResult {
			if opts.SortBy != "nextRunAtMs" || opts.SortDir != "asc" {
				t.Errorf("sort wrong: %+v", opts)
			}
			return cron.ListPageResult{
				Total: 2,
				Jobs: []cron.StoreJob{
					{
						ID:       "morning-brief",
						Name:     "Morning brief",
						Enabled:  true,
						Schedule: cron.StoreSchedule{Kind: "cron", Expr: "0 9 * * *", Tz: "Asia/Seoul"},
						Payload:  cron.StorePayload{Kind: "agentTurn", Message: "오늘 일정 정리"},
						State:    cron.JobState{NextRunAtMs: 1730000000000},
					},
					{
						ID:       "heartbeat",
						Enabled:  true,
						Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 120000},
						Payload:  cron.StorePayload{Kind: "systemEvent", Text: "tick"},
					},
				},
			}
		},
	}
	h := cronsList(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.list", map[string]any{}))
	var got struct {
		Jobs  []map[string]any `json:"jobs"`
		Total int              `json:"total"`
	}
	decode(t, resp, &got)
	if got.Total != 2 || len(got.Jobs) != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.Jobs[0]["id"] != "morning-brief" {
		t.Errorf("id = %v", got.Jobs[0]["id"])
	}
	// schedule humanized: cron with tz → "<expr> (<tz>)"
	if got.Jobs[0]["schedule"] != "0 9 * * * (Asia/Seoul)" {
		t.Errorf("schedule = %v", got.Jobs[0]["schedule"])
	}
	// every: 120000ms → "2분마다"
	if got.Jobs[1]["schedule"] != "2분마다" {
		t.Errorf("schedule = %v", got.Jobs[1]["schedule"])
	}
	// Name falls back to ID when empty.
	if got.Jobs[1]["name"] != "heartbeat" {
		t.Errorf("name fallback failed: %+v", got.Jobs[1])
	}
}

func TestCronsList_ExcludeDisabledByDefault(t *testing.T) {
	svc := &fakeCronLister{
		listPageFn: func(_ cron.ListPageOptions) cron.ListPageResult {
			return cron.ListPageResult{
				Total: 2,
				Jobs: []cron.StoreJob{
					{ID: "on", Enabled: true, Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60000}},
					{ID: "off", Enabled: false, Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60000}},
				},
			}
		},
	}
	h := cronsList(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.list", map[string]any{}))
	var got struct {
		Jobs []map[string]any `json:"jobs"`
	}
	decode(t, resp, &got)
	if len(got.Jobs) != 1 || got.Jobs[0]["id"] != "on" {
		t.Errorf("disabled leaked: %+v", got.Jobs)
	}
}

func TestCronsList_IncludeDisabled(t *testing.T) {
	svc := &fakeCronLister{
		listPageFn: func(_ cron.ListPageOptions) cron.ListPageResult {
			return cron.ListPageResult{
				Total: 2,
				Jobs: []cron.StoreJob{
					{ID: "on", Enabled: true, Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60000}},
					{ID: "off", Enabled: false, Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60000}},
				},
			}
		},
	}
	h := cronsList(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.list", map[string]any{"includeDisabled": true}))
	var got struct {
		Jobs []map[string]any `json:"jobs"`
	}
	decode(t, resp, &got)
	if len(got.Jobs) != 2 {
		t.Errorf("expected both jobs: %+v", got.Jobs)
	}
}

func TestCronsList_LimitClamp(t *testing.T) {
	var seenLimit int
	svc := &fakeCronLister{
		listPageFn: func(opts cron.ListPageOptions) cron.ListPageResult {
			seenLimit = opts.Limit
			return cron.ListPageResult{}
		},
	}
	h := cronsList(cronsDepsFor(svc))
	h(authedCtx(), reqWith(t, "miniapp.crons.list", map[string]any{"limit": 9999}))
	if seenLimit != maxCronListLimit {
		t.Errorf("limit = %d, want clamped to %d", seenLimit, maxCronListLimit)
	}
}

func TestCronsList_RequiresAuth(t *testing.T) {
	h := cronsList(cronsDepsFor(&fakeCronLister{}))
	resp := h(context.Background(), reqWith(t, "miniapp.crons.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestCronsList_ServiceUnavailable(t *testing.T) {
	deps := CronsDeps{Service: func() (CronLister, error) {
		return nil, errors.New("not wired")
	}}
	h := cronsList(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE: %+v", resp)
	}
}

func TestCronsMethods_NilFactoryReturnsNil(t *testing.T) {
	if got := CronsMethods(CronsDeps{Service: nil}); got != nil {
		t.Errorf("CronsMethods(nil) = %v, want nil", got)
	}
}

// --- formatCronSchedule / humanizeInterval --------------------------------

func TestFormatCronSchedule_All(t *testing.T) {
	cases := []struct {
		name string
		in   cron.StoreSchedule
		want string
	}{
		{
			name: "at parses RFC3339 to local",
			in:   cron.StoreSchedule{Kind: "at", At: "2026-05-27T09:30:00+09:00"},
			want: "1회: " + mustParseLocal("2026-05-27T09:30:00+09:00"),
		},
		{
			name: "at unparseable returns raw",
			in:   cron.StoreSchedule{Kind: "at", At: "not-a-time"},
			want: "1회: not-a-time",
		},
		{name: "every 30s", in: cron.StoreSchedule{Kind: "every", EveryMs: 30000}, want: "30초마다"},
		{name: "every 2m", in: cron.StoreSchedule{Kind: "every", EveryMs: 120000}, want: "2분마다"},
		{name: "every 1h", in: cron.StoreSchedule{Kind: "every", EveryMs: 3600 * 1000}, want: "1시간마다"},
		{name: "every 1d", in: cron.StoreSchedule{Kind: "every", EveryMs: 24 * 3600 * 1000}, want: "1일마다"},
		{name: "every 0 → 미정", in: cron.StoreSchedule{Kind: "every", EveryMs: 0}, want: "주기 미정"},
		{name: "cron expr no tz", in: cron.StoreSchedule{Kind: "cron", Expr: "*/5 * * * *"}, want: "*/5 * * * *"},
		{name: "cron expr with tz", in: cron.StoreSchedule{Kind: "cron", Expr: "0 9 * * *", Tz: "Asia/Seoul"}, want: "0 9 * * * (Asia/Seoul)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatCronSchedule(c.in)
			if got != c.want {
				t.Errorf("formatCronSchedule(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func mustParseLocal(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		panic(err)
	}
	return t.Local().Format("2006-01-02 15:04")
}
