package handlerminiapp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeCronLister struct {
	listPageFn func(opts cron.ListPageOptions) cron.ListPageResult
	jobFn      func(id string) *cron.StoreJob

	// job is the mutable single job backing get/update/remove/run tests.
	// Update mutates it in place so a follow-up Job() reflects the patch,
	// matching how *cron.Service threads a copy back through Service.Job.
	job        *cron.StoreJob
	updateErr  error
	removeErr  error
	enqueueErr error

	enqueuedID   string
	enqueuedMode string
	removedID    string
}

func (f *fakeCronLister) ListPage(opts cron.ListPageOptions) cron.ListPageResult {
	if f.listPageFn == nil {
		return cron.ListPageResult{}
	}
	return f.listPageFn(opts)
}

func (f *fakeCronLister) Job(id string) *cron.StoreJob {
	if f.jobFn != nil {
		return f.jobFn(id)
	}
	if f.job != nil && f.job.ID == id {
		cp := *f.job
		return &cp
	}
	return nil
}

func (f *fakeCronLister) Update(_ context.Context, id string, patch func(*cron.StoreJob)) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.job == nil || f.job.ID != id {
		return fmt.Errorf("job %q not found", id)
	}
	patch(f.job)
	return nil
}

func (f *fakeCronLister) Remove(id string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removedID = id
	if f.job != nil && f.job.ID == id {
		f.job = nil
	}
	return nil
}

func (f *fakeCronLister) EnqueueRun(_ context.Context, id, mode string) error {
	if f.enqueueErr != nil {
		return f.enqueueErr
	}
	f.enqueuedID = id
	f.enqueuedMode = mode
	return nil
}

func cronsDepsFor(svc CronService) CronsDeps {
	return CronsDeps{Service: func() (CronService, error) { return svc, nil }}
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
	deps := CronsDeps{Service: func() (CronService, error) {
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

func TestCronsMethods_RegistersGet(t *testing.T) {
	got := CronsMethods(cronsDepsFor(&fakeCronLister{}))
	if _, ok := got["miniapp.crons.get"]; !ok {
		t.Errorf("miniapp.crons.get not registered: %v", got)
	}
}

// --- cronsGet -------------------------------------------------------------

func TestCronsGet_HappyPath(t *testing.T) {
	// Longer than maxCronPayloadPreview (120 runes) so we can prove the
	// detail endpoint returns the full prompt where the list row would
	// have truncated it.
	fullPrompt := "오늘 일정과 미처리 메일을 정리해서 브리핑해줘. 이 지시문은 120자 미리보기로 잘리지 않고 상세 화면에서는 전체가 그대로 보여야 한다. " +
		"길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게 길게."
	svc := &fakeCronLister{
		jobFn: func(id string) *cron.StoreJob {
			if id != "morning-brief" {
				return nil
			}
			return &cron.StoreJob{
				ID:            "morning-brief",
				Name:          "Morning brief",
				Enabled:       true,
				AgentID:       "deneb",
				SessionTarget: cron.SessionTargetSubagent,
				Schedule:      cron.StoreSchedule{Kind: "cron", Expr: "0 9 * * *", Tz: "Asia/Seoul", StaggerMs: 30000},
				Payload: cron.StorePayload{
					Kind:           "agentTurn",
					Message:        fullPrompt,
					Model:          "gpt-x",
					Thinking:       "high",
					TimeoutSeconds: 120,
					RetryCount:     2,
				},
				Delivery:     &cron.JobDeliveryConfig{Channel: "telegram", To: "-100123", ThreadID: "7"},
				FailureAlert: &cron.CronFailureAlert{After: 3},
				State: cron.JobState{
					NextRunAtMs:        1730000000000,
					LastSessionKey:     "sess-abc",
					LastDeliveryStatus: "ok",
				},
				CreatedAtMs: 1720000000000,
				UpdatedAtMs: 1725000000000,
			}
		},
	}
	h := cronsGet(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.get", map[string]any{"id": "morning-brief"}))
	var got map[string]any
	decode(t, resp, &got)
	if got["id"] != "morning-brief" || got["agentId"] != "deneb" {
		t.Fatalf("unexpected: %+v", got)
	}
	if got["sessionTarget"] != "subagent" {
		t.Errorf("sessionTarget = %v", got["sessionTarget"])
	}
	// schedule pieces preserved alongside the humanized summary
	if got["schedule"] != "0 9 * * * (Asia/Seoul)" || got["cronExpr"] != "0 9 * * *" {
		t.Errorf("schedule fields = %v / %v", got["schedule"], got["cronExpr"])
	}
	// FULL prompt, returned verbatim (not the 120-rune list preview).
	prompt, _ := got["prompt"].(string)
	if len([]rune(prompt)) <= maxCronPayloadPreview {
		t.Errorf("test prompt should exceed preview cap, got %d runes", len([]rune(prompt)))
	}
	if prompt != fullPrompt {
		t.Errorf("prompt truncated:\n got %q\nwant %q", prompt, fullPrompt)
	}
	if got["deliveryChannel"] != "telegram" || got["deliveryTo"] != "-100123" || got["deliveryThreadId"] != "7" {
		t.Errorf("delivery = %+v", got)
	}
	if got["lastSessionKey"] != "sess-abc" || got["lastDeliveryStatus"] != "ok" {
		t.Errorf("state = %+v", got)
	}
}

func TestCronsGet_NotFound(t *testing.T) {
	svc := &fakeCronLister{jobFn: func(string) *cron.StoreJob { return nil }}
	h := cronsGet(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.get", map[string]any{"id": "nope"}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND: %+v", resp)
	}
}

func TestCronsGet_MissingID(t *testing.T) {
	h := cronsGet(cronsDepsFor(&fakeCronLister{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.get", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM for missing id: %+v", resp)
	}
}

func TestCronsGet_RequiresAuth(t *testing.T) {
	h := cronsGet(cronsDepsFor(&fakeCronLister{}))
	resp := h(context.Background(), reqWith(t, "miniapp.crons.get", map[string]any{"id": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

// --- cronsUpdate / cronsRun / cronsRemove ---------------------------------

func TestCronsMethods_RegistersMutations(t *testing.T) {
	got := CronsMethods(cronsDepsFor(&fakeCronLister{}))
	for _, m := range []string{"miniapp.crons.update", "miniapp.crons.run", "miniapp.crons.remove"} {
		if _, ok := got[m]; !ok {
			t.Errorf("%s not registered: %v", m, got)
		}
	}
}

func baseJob() *cron.StoreJob {
	return &cron.StoreJob{
		ID:       "job1",
		Name:     "old name",
		Enabled:  true,
		Schedule: cron.StoreSchedule{Kind: "cron", Expr: "0 9 * * *", Tz: "Asia/Seoul"},
		Payload:  cron.StorePayload{Kind: "agentTurn", Message: "old prompt"},
	}
}

func TestCronsUpdate_PatchesFields(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{
		"id":             "job1",
		"name":           "new name",
		"prompt":         "new prompt",
		"model":          "qwen",
		"thinking":       "high",
		"timeoutSeconds": 90,
		"retryCount":     2,
		"delivery":       map[string]any{"channel": "telegram", "to": "-100", "threadId": "5"},
	}))
	var got map[string]any
	decode(t, resp, &got)
	if got["name"] != "new name" || got["prompt"] != "new prompt" || got["model"] != "qwen" {
		t.Fatalf("fields not patched: %+v", got)
	}
	if got["thinking"] != "high" || got["timeoutSeconds"].(float64) != 90 || got["retryCount"].(float64) != 2 {
		t.Errorf("payload fields wrong: %+v", got)
	}
	if got["deliveryChannel"] != "telegram" || got["deliveryTo"] != "-100" || got["deliveryThreadId"] != "5" {
		t.Errorf("delivery not patched: %+v", got)
	}
	// Untouched field stays.
	if svc.job.Schedule.Expr != "0 9 * * *" {
		t.Errorf("schedule should be untouched: %+v", svc.job.Schedule)
	}
}

func TestCronsUpdate_RetryClampedToThree(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1", "retryCount": 99}))
	if svc.job.Payload.RetryCount != 3 {
		t.Errorf("retry = %d, want clamped to 3", svc.job.Payload.RetryCount)
	}
}

func TestCronsUpdate_EnabledToggle(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1", "enabled": false}))
	if svc.job.Enabled {
		t.Errorf("enabled should be false after toggle")
	}
	// Only the toggle was sent — everything else stays put.
	if svc.job.Name != "old name" || svc.job.Payload.Message != "old prompt" {
		t.Errorf("toggle leaked into other fields: %+v", svc.job)
	}
}

func TestCronsUpdate_ReparsesSchedule(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	// Switch from a cron expression to a 15-minute interval.
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1", "schedule": "15m"}))
	var got map[string]any
	decode(t, resp, &got)
	if svc.job.Schedule.Kind != "every" || svc.job.Schedule.EveryMs != 900000 {
		t.Fatalf("schedule not reparsed: %+v", svc.job.Schedule)
	}
	if got["scheduleKind"] != "every" || got["scheduleSpec"] != "15m" {
		t.Errorf("detail schedule fields wrong: %+v", got)
	}
}

func TestCronsUpdate_RejectsBadSchedule(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1", "schedule": "not a schedule!!"}))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Errorf("expected VALIDATION_FAILED: %+v", resp)
	}
	// The job must be left untouched on a rejected schedule.
	if svc.job.Schedule.Expr != "0 9 * * *" {
		t.Errorf("schedule mutated despite rejection: %+v", svc.job.Schedule)
	}
}

func TestCronsUpdate_EmptyScheduleRejected(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsUpdate(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1", "schedule": "   "}))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Errorf("expected VALIDATION_FAILED for empty schedule: %+v", resp)
	}
}

func TestCronsUpdate_NotFound(t *testing.T) {
	svc := &fakeCronLister{} // no job
	h := cronsUpdate(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "ghost", "name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND: %+v", resp)
	}
}

func TestCronsUpdate_MissingID(t *testing.T) {
	h := cronsUpdate(cronsDepsFor(&fakeCronLister{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.update", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM: %+v", resp)
	}
}

func TestCronsUpdate_RequiresAuth(t *testing.T) {
	h := cronsUpdate(cronsDepsFor(&fakeCronLister{}))
	resp := h(context.Background(), reqWith(t, "miniapp.crons.update", map[string]any{"id": "job1"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestCronsRun_Enqueues(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsRun(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.run", map[string]any{"id": "job1"}))
	var got map[string]any
	decode(t, resp, &got)
	if got["enqueued"] != true {
		t.Errorf("expected enqueued=true: %+v", got)
	}
	if svc.enqueuedID != "job1" || svc.enqueuedMode != "manual" {
		t.Errorf("enqueue args wrong: id=%q mode=%q", svc.enqueuedID, svc.enqueuedMode)
	}
}

func TestCronsRun_NotFound(t *testing.T) {
	h := cronsRun(cronsDepsFor(&fakeCronLister{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.run", map[string]any{"id": "ghost"}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND: %+v", resp)
	}
}

func TestCronsRemove_Removes(t *testing.T) {
	svc := &fakeCronLister{job: baseJob()}
	h := cronsRemove(cronsDepsFor(svc))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.remove", map[string]any{"id": "job1"}))
	var got map[string]any
	decode(t, resp, &got)
	if got["removed"] != true || svc.removedID != "job1" {
		t.Errorf("remove failed: got=%+v removedID=%q", got, svc.removedID)
	}
}

func TestCronsRemove_NotFound(t *testing.T) {
	h := cronsRemove(cronsDepsFor(&fakeCronLister{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.crons.remove", map[string]any{"id": "ghost"}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND: %+v", resp)
	}
}

func TestCronsRemove_RequiresAuth(t *testing.T) {
	h := cronsRemove(cronsDepsFor(&fakeCronLister{}))
	resp := h(context.Background(), reqWith(t, "miniapp.crons.remove", map[string]any{"id": "job1"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestScheduleSpec_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   cron.StoreSchedule
		want string
	}{
		{"cron", cron.StoreSchedule{Kind: "cron", Expr: "0 9 * * *"}, "0 9 * * *"},
		{"every 15m", cron.StoreSchedule{Kind: "every", EveryMs: 900000}, "15m"},
		{"every 90m", cron.StoreSchedule{Kind: "every", EveryMs: 5400000}, "1h30m"},
		{"every 30s", cron.StoreSchedule{Kind: "every", EveryMs: 30000}, "30s"},
		{"at", cron.StoreSchedule{Kind: "at", At: "2026-05-27T09:30:00+09:00"}, "2026-05-27T09:30:00+09:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scheduleSpec(c.in); got != c.want {
				t.Errorf("scheduleSpec(%+v) = %q, want %q", c.in, got, c.want)
			}
		})
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
