package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// --- cron tool ---

// ToolCron returns a tool function that manages cron jobs.
// When Service is available, uses persistent storage with full cron expression support.
// Falls back to basic Scheduler for in-memory operation.
func ToolCron(d *tooldeps.ChronoDeps) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action       string         `json:"action"`
			JobID        string         `json:"jobId"`
			Job          map[string]any `json:"job"`
			Name         string         `json:"name"`
			Schedule     string         `json:"schedule"`
			Command      string         `json:"command"`
			Text         string         `json:"text"`
			Enabled      *bool          `json:"enabled"`
			Limit        int            `json:"limit"`
			Tz           string         `json:"tz"`
			StaggerMs    int64          `json:"staggerMs"`
			AnchorTime   string         `json:"anchorTime"`
			RetryCount   *int           `json:"retryCount"`
			RetryBackoff *int64         `json:"retryBackoffMs"`
			DeliveryMode string         `json:"deliveryMode"`
			Thinking     string         `json:"thinking"`
		}
		if err := jsonutil.UnmarshalInto("cron params", input, &p); err != nil {
			return "", err
		}
		p.Action = normalizeCronAction(p.Action)

		svc := d.Service

		if svc == nil {
			return "Cron service not available.", nil
		}

		opts := cronToolOpts{
			Tz:           p.Tz,
			StaggerMs:    p.StaggerMs,
			AnchorTime:   p.AnchorTime,
			RetryCount:   p.RetryCount,
			RetryBackoff: p.RetryBackoff,
			DeliveryMode: p.DeliveryMode,
			Thinking:     p.Thinking,
		}

		switch p.Action {
		case "status":
			return cronStatus(svc)

		case "list":
			return cronList(svc)

		case "add":
			return cronAdd(ctx, d, p.Name, p.Schedule, p.Command, p.Enabled, p.Job, opts)

		case "update":
			return cronUpdate(ctx, d, p.JobID, p.Name, p.Schedule, p.Command, p.Enabled, opts)

		case "remove":
			return cronRemove(d, p.JobID)

		case "run":
			return cronRun(ctx, d, p.JobID)

		case "get":
			return cronGet(d, p.JobID)

		case "runs":
			return cronRuns(d, p.JobID, p.Limit)

		case "wake":
			if svc != nil {
				svc.Wake(ctx, "now", p.Text)
			}
			return fmt.Sprintf("Wake event: %s", p.Text), nil

		default:
			return fmt.Sprintf("Unknown cron action: %q. Supported: status, list, add, update, remove, run, get, runs, wake", p.Action), nil
		}
	}
}

// cronToolOpts holds extended options for add/update actions.
type cronToolOpts struct {
	Tz           string
	StaggerMs    int64
	AnchorTime   string
	RetryCount   *int
	RetryBackoff *int64
	DeliveryMode string
	// Thinking sets the job's per-run thinking level ("off" disables the
	// thinking phase — right for routine, well-templated jobs on the
	// dual-mode main model). "default" clears an existing override.
	Thinking string
}

func cronStatus(svc *cron.Service) (string, error) {
	st := svc.Status()
	var sb strings.Builder
	sb.WriteString("**Cron 서비스 상태**\n")

	// Count enabled/disabled.
	jobs, _ := svc.List(&cron.ListOptions{IncludeDisabled: true})
	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	disabled := len(jobs) - enabled
	fmt.Fprintf(&sb, "- 작업: %d개 (활성 %d, 비활성 %d)\n", len(jobs), enabled, disabled)

	if st.Running {
		sb.WriteString("- 상태: 실행 중\n")
	} else {
		sb.WriteString("- 상태: 정지\n")
	}

	// Show next due job.
	if st.NextRunAtMs > 0 {
		nextJobName := ""
		for _, j := range jobs {
			if j.Enabled && j.State.NextRunAtMs == st.NextRunAtMs {
				nextJobName = j.Name
				break
			}
		}
		if nextJobName != "" {
			fmt.Fprintf(&sb, "- 다음 실행: %s — %s", nextJobName, cronNextRun(st.NextRunAtMs))
		} else {
			fmt.Fprintf(&sb, "- 다음 실행: %s", cronNextRun(st.NextRunAtMs))
		}
	}
	return sb.String(), nil
}

func cronList(svc *cron.Service) (string, error) {
	jobs, err := svc.List(&cron.ListOptions{IncludeDisabled: true})
	if err != nil {
		return "", fmt.Errorf("작업 목록 조회 실패: %w", err)
	}
	if len(jobs) == 0 {
		return "등록된 크론 작업이 없습니다.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**크론 작업 %d개:**\n", len(jobs))
	for _, j := range jobs {
		status := "✅"
		if !j.Enabled {
			status = "⏸️"
		}
		schedDesc := cron.FormatHumanSchedule(j.Schedule)
		nextRun := ""
		if j.Enabled && j.State.NextRunAtMs > 0 {
			rel := cron.FormatRelativeTime(j.State.NextRunAtMs)
			nextRun = fmt.Sprintf(" → %s (%s)",
				time.UnixMilli(j.State.NextRunAtMs).Format("01-02 15:04"), rel)
		}
		fmt.Fprintf(&sb, "\n%s **%s** `%s`\n", status, j.Name, schedDesc)
		if nextRun != "" {
			fmt.Fprintf(&sb, "  다음 실행%s\n", nextRun)
		}
		if cmd := textutil.TruncateRunesWithin(cronPayloadMsg(j.Payload), 80, "…"); cmd != "" {
			fmt.Fprintf(&sb, "  명령: %s\n", cmd)
		}
		if j.State.ConsecutiveErrors > 0 {
			fmt.Fprintf(&sb, "  ⚠️ 연속 오류: %d회\n", j.State.ConsecutiveErrors)
		}
	}
	return sb.String(), nil
}

// cronAddMergeJobObj merges the nested job object's fields into the flat
// name/schedule/command params (flat params win when both are set).
func cronAddMergeJobObj(jobObj map[string]any, name, schedule, command string) (string, string, string) {
	if jobObj == nil {
		return name, schedule, command
	}
	if v, ok := jobObj["name"].(string); ok && name == "" {
		name = v
	}
	if v, ok := jobObj["schedule"].(string); ok && schedule == "" {
		schedule = v
	}
	if v, ok := jobObj["command"].(string); ok && command == "" {
		command = v
	}
	return name, schedule, command
}

// cronBuildPayload assembles the agentTurn payload for a new cron job from the
// command text and the extended add options.
func cronBuildPayload(command string, opts cronToolOpts) cron.StorePayload {
	payload := cron.StorePayload{
		Kind:    "agentTurn",
		Message: command,
	}
	if opts.RetryCount != nil {
		rc := *opts.RetryCount
		if rc > 3 {
			rc = 3
		}
		payload.RetryCount = rc
	}
	if opts.RetryBackoff != nil {
		payload.RetryBackoffMs = *opts.RetryBackoff
	}
	if opts.Thinking != "" && opts.Thinking != "default" {
		payload.Thinking = opts.Thinking
	}
	return payload
}

// cronApplyDeliveryMode applies the add-time delivery mode to the job.
func cronApplyDeliveryMode(ctx context.Context, job *cron.StoreJob, deliveryMode string) {
	if deliveryMode == "none" {
		// Explicit no-delivery: leave Delivery nil (agent runs silently).
		return
	}
	if deliveryMode == "announce" || deliveryMode == "" {
		// Default: capture delivery context from the creating session so the
		// cron job knows where to send output (delivery channel + session). The
		// thread ID is captured too so a cron defined inside a forum topic
		// produces its output in that same topic instead of leaking into
		// General — the user-visible win of M4.
		if delivery := toolport.DeliveryFromContext(ctx); delivery != nil && delivery.To != "" {
			job.Delivery = &cron.JobDeliveryConfig{
				Channel:  delivery.Channel,
				To:       delivery.To,
				ThreadID: delivery.ThreadID,
			}
		}
	}
}

func cronAdd(ctx context.Context, d *tooldeps.ChronoDeps, name, schedule, command string, enabled *bool, jobObj map[string]any, opts cronToolOpts) (string, error) {
	// Support nested job object.
	name, schedule, command = cronAddMergeJobObj(jobObj, name, schedule, command)
	if name == "" || schedule == "" || command == "" {
		return "", fmt.Errorf("name, schedule, command 모두 필요합니다. 예: cron add name=daily schedule='0 9 * * *' command='뉴스 확인'")
	}
	const maxCommandLen = 4096
	if len(command) > maxCommandLen {
		return "", fmt.Errorf("command가 최대 길이 %d자를 초과합니다", maxCommandLen)
	}

	if d.Service == nil {
		return "", fmt.Errorf("크론 서비스를 사용할 수 없습니다")
	}

	smartOpts := cron.SmartScheduleOpts{
		Tz:         opts.Tz,
		StaggerMs:  opts.StaggerMs,
		AnchorTime: opts.AnchorTime,
	}
	storeSched, err := cron.ParseSmartScheduleWithOpts(schedule, smartOpts)
	if err != nil {
		return "", fmt.Errorf("잘못된 스케줄: %w", err)
	}
	isEnabled := true
	if enabled != nil {
		isEnabled = *enabled
	}
	payload := cronBuildPayload(command, opts)

	job := cron.StoreJob{
		ID:       name,
		Name:     name,
		Enabled:  isEnabled,
		Schedule: storeSched,
		Payload:  payload,
	}

	// Apply delivery mode.
	cronApplyDeliveryMode(ctx, &job, opts.DeliveryMode)

	// The store's AddJob is "replace if exists" (cron/store.go), so an add that
	// reuses an ID silently discards the operator's existing automation —
	// schedule, command, delivery mode, retry config, run history — while the
	// result line still says 추가. Capture what is about to be lost so the
	// result can say so.
	replaced := cronReplacedJobNotice(d, job.ID, storeSched, command)

	if err := d.Service.Add(ctx, job); err != nil {
		return "", fmt.Errorf("크론 작업 추가 실패: %w", err)
	}

	// Build response.
	schedDesc := cron.FormatHumanSchedule(storeSched)
	nextMs := cron.ComputeNextRunAtMs(storeSched, time.Now().UnixMilli())
	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ 크론 작업 **%s** 추가 완료\n", name)
	fmt.Fprintf(&sb, "- 스케줄: %s\n", schedDesc)
	if nextMs > 0 {
		fmt.Fprintf(&sb, "- 다음 실행: %s\n", cronNextRun(nextMs))
	}
	if payload.RetryCount > 0 {
		fmt.Fprintf(&sb, "- 재시도: 최대 %d회\n", payload.RetryCount)
	}
	sb.WriteString(replaced)
	return sb.String(), nil
}

// cronReplacedJobNotice reports the job an add is about to overwrite.
//
// cron add is the only write path that can destroy an operator-configured
// automation without naming it: the store replaces by ID, and the tool answers
// "추가 완료" either way. calendar create warns about the slot it collides with
// and wiki write names the sections it dropped; this is the same duty on the
// higher-stakes surface — a replaced morning-letter or audit job simply stops
// happening, with nothing in the result to notice.
//
// It does not block: the operator asked for the add, and cron update exists for
// deliberate edits. Empty when the ID is free, so an ordinary add stays quiet.
func cronReplacedJobNotice(d *tooldeps.ChronoDeps, jobID string, newSched cron.StoreSchedule, newCommand string) string {
	if d == nil || d.Service == nil {
		return ""
	}
	existing := d.Service.Job(jobID)
	if existing == nil {
		return ""
	}
	var changes []string
	if oldSched := cron.FormatHumanSchedule(existing.Schedule); oldSched != cron.FormatHumanSchedule(newSched) {
		changes = append(changes, fmt.Sprintf("스케줄 %q → %q", oldSched, cron.FormatHumanSchedule(newSched)))
	}
	// cronPayloadMsg is the same reader cron list/get use, so the notice cannot
	// disagree with what the operator saw a moment ago.
	if oldCommand := strings.TrimSpace(cronPayloadMsg(existing.Payload)); oldCommand != strings.TrimSpace(newCommand) {
		changes = append(changes, fmt.Sprintf("명령 %q → %q",
			textutil.TruncateRunesWithin(oldCommand, 60, "…"),
			textutil.TruncateRunesWithin(strings.TrimSpace(newCommand), 60, "…")))
	}
	notice := fmt.Sprintf("\n⚠️ 같은 ID의 기존 작업을 교체했다 (추가가 아니다): %s", existing.Name)
	if len(changes) > 0 {
		notice += "\n  " + strings.Join(changes, "\n  ")
	}
	notice += "\n의도한 수정이 아니면 cron update로 되돌리고, 새 작업이면 다른 name을 써라.\n"
	return notice
}

func cronUpdate(ctx context.Context, d *tooldeps.ChronoDeps, jobID, name, schedule, command string, enabled *bool, opts cronToolOpts) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	if d.Service != nil {
		err := d.Service.Update(ctx, jobID, func(j *cron.StoreJob) {
			if name != "" {
				j.Name = name
			}
			if schedule != "" {
				smartOpts := cron.SmartScheduleOpts{
					Tz:         opts.Tz,
					StaggerMs:  opts.StaggerMs,
					AnchorTime: opts.AnchorTime,
				}
				if storeSched, err := cron.ParseSmartScheduleWithOpts(schedule, smartOpts); err == nil {
					j.Schedule = storeSched
				}
			}
			if command != "" {
				j.Payload.Message = command
				j.Payload.Kind = "agentTurn"
			}
			if enabled != nil {
				j.Enabled = *enabled
			}
			if opts.RetryCount != nil {
				rc := *opts.RetryCount
				if rc > 3 {
					rc = 3
				}
				j.Payload.RetryCount = rc
			}
			if opts.RetryBackoff != nil {
				j.Payload.RetryBackoffMs = *opts.RetryBackoff
			}
			switch opts.Thinking {
			case "":
				// not specified — keep as-is
			case "default":
				j.Payload.Thinking = "" // clear the override
			default:
				j.Payload.Thinking = opts.Thinking
			}
		})
		if err != nil {
			return "", fmt.Errorf("업데이트 실패: %w", err)
		}
		job := d.Service.Job(jobID)
		if job == nil {
			return fmt.Sprintf("✅ 크론 작업 **%s** 업데이트 완료.", jobID), nil
		}
		schedDesc := cron.FormatHumanSchedule(job.Schedule)
		status := "활성"
		if !job.Enabled {
			status = "비활성"
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "✅ 크론 작업 **%s** 업데이트 완료\n", job.Name)
		fmt.Fprintf(&sb, "- 스케줄: %s\n", schedDesc)
		fmt.Fprintf(&sb, "- 상태: %s\n", status)
		if job.State.NextRunAtMs > 0 {
			fmt.Fprintf(&sb, "- 다음 실행: %s", cronNextRun(job.State.NextRunAtMs))
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("업데이트에는 영속 크론 서비스가 필요합니다 (사용 불가)")
}

func cronRemove(d *tooldeps.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	if err := d.Service.Remove(jobID); err != nil {
		return "", fmt.Errorf("삭제 실패: %w", err)
	}
	return fmt.Sprintf("✅ 크론 작업 **%s** 삭제 완료.", jobID), nil
}

func cronRun(ctx context.Context, d *tooldeps.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	outcome, err := d.Service.Run(ctx, jobID, "force")
	if err != nil {
		return "", fmt.Errorf("실행 실패: %w", err)
	}
	dur := cron.FormatDurationKorean(outcome.DurationMs)
	if outcome.Error != "" {
		return fmt.Sprintf("❌ **%s** 실행 실패 (%s): %s", jobID, dur, outcome.Error), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "✅ **%s** 실행 완료 (%s)", jobID, dur)
	if outcome.Retries > 0 {
		fmt.Fprintf(&sb, " [재시도 %d회]", outcome.Retries)
	}
	return sb.String(), nil
}

func cronGet(d *tooldeps.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	job := d.Service.Job(jobID)
	if job == nil {
		return fmt.Sprintf("크론 작업 %q을(를) 찾을 수 없습니다.", jobID), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**크론 작업: %s** (id=%s)\n", job.Name, job.ID)

	status := "✅ 활성"
	if !job.Enabled {
		status = "⏸️ 비활성"
		if job.State.AutoDisabledAtMs > 0 {
			status = "🚫 자동 비활성 (연속 오류)"
		}
	}
	fmt.Fprintf(&sb, "- 상태: %s\n", status)
	fmt.Fprintf(&sb, "- 스케줄: %s\n", cron.FormatHumanSchedule(job.Schedule))

	if cmd := textutil.TruncateRunesWithin(cronPayloadMsg(job.Payload), 120, "…"); cmd != "" {
		fmt.Fprintf(&sb, "- 명령: %s\n", cmd)
	}

	if job.Enabled && job.State.NextRunAtMs > 0 {
		fmt.Fprintf(&sb, "- 다음 실행: %s\n", cronNextRun(job.State.NextRunAtMs))
	}

	if job.State.LastSessionKey != "" {
		fmt.Fprintf(&sb, "- 마지막 세션: %s\n", job.State.LastSessionKey)
	}

	if job.State.ConsecutiveErrors > 0 {
		fmt.Fprintf(&sb, "- ⚠️ 연속 오류: %d회\n", job.State.ConsecutiveErrors)
	}

	// Delivery info.
	if job.State.LastDeliveryStatus != "" {
		deliveryIcon := "📤"
		if job.State.LastDeliveryStatus == "not-delivered" {
			deliveryIcon = "📤❌"
		}
		fmt.Fprintf(&sb, "- %s 배달: %s", deliveryIcon, job.State.LastDeliveryStatus)
		if job.State.LastDeliveryError != "" {
			fmt.Fprintf(&sb, " (%s)", job.State.LastDeliveryError)
		}
		sb.WriteString("\n")
	}

	// Retry config.
	if job.Payload.RetryCount > 0 {
		fmt.Fprintf(&sb, "- 재시도: 최대 %d회", job.Payload.RetryCount)
		if job.Payload.RetryBackoffMs > 0 {
			fmt.Fprintf(&sb, " (백오프 %s)", cron.FormatDurationKorean(job.Payload.RetryBackoffMs))
		}
		sb.WriteString("\n")
	}

	// Timestamps.
	if job.CreatedAtMs > 0 {
		fmt.Fprintf(&sb, "- 생성: %s", time.UnixMilli(job.CreatedAtMs).Format("2006-01-02 15:04"))
	}
	return sb.String(), nil
}

func cronRuns(d *tooldeps.ChronoDeps, jobID string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if d.RunLog != nil {
		var page cron.RunLogPageResult
		if jobID != "" {
			page = d.RunLog.ReadPage(jobID, cron.RunLogReadOpts{Limit: limit, SortDir: "desc"})
		} else {
			page = d.RunLog.ReadPageAll(cron.RunLogReadOpts{Limit: limit, SortDir: "desc"})
		}
		if len(page.Entries) == 0 {
			if jobID != "" {
				return fmt.Sprintf("%q 작업의 실행 이력이 없습니다.", jobID), nil
			}
			return "크론 실행 이력이 없습니다.", nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "**실행 이력** (%d/%d건):\n", len(page.Entries), page.Total)
		for _, e := range page.Entries {
			ts := time.UnixMilli(e.Ts).Format("01-02 15:04")

			// Status icon.
			icon := "✅"
			switch e.Status {
			case "error":
				icon = "❌"
			case "timeout":
				icon = "⏱️"
			case "skipped":
				icon = "⏭️"
			}

			dur := ""
			if e.DurationMs > 0 {
				dur = ", " + cron.FormatDurationKorean(e.DurationMs)
			}

			// Delivery suffix.
			deliveryStr := ""
			if e.DeliveryStatus == "delivered" {
				deliveryStr = ", 전달됨"
			} else if e.DeliveryStatus == "not-delivered" {
				deliveryStr = ", 전달 실패"
				if e.DeliveryError != "" {
					deliveryStr += ": " + e.DeliveryError
				}
			}

			// Retry suffix.
			retryStr := ""
			if e.Retries > 0 {
				retryStr = fmt.Sprintf(", 재시도 %d회", e.Retries)
			}

			errStr := ""
			if e.Error != "" {
				errStr = fmt.Sprintf(" — %s", textutil.TruncateRunesWithin(e.Error, 60, "…"))
			}

			summary := ""
			if e.Summary != "" && e.Error == "" {
				summary = fmt.Sprintf(" — %s", textutil.TruncateRunesWithin(e.Summary, 60, "…"))
			}

			fmt.Fprintf(&sb, "\n%s [%s] %s (%s%s%s%s)%s%s",
				icon, ts, e.JobID, e.Status, dur, deliveryStr, retryStr, errStr, summary)
		}
		return sb.String(), nil
	}
	return "실행 이력을 사용할 수 없습니다.", nil
}

// cronNextRun formats a millisecond timestamp as "2006-01-02 15:04 (X 후/전)".
func cronNextRun(ms int64) string {
	return time.UnixMilli(ms).Format("2006-01-02 15:04") + " (" + cron.FormatRelativeTime(ms) + ")"
}

// cronPayloadMsg returns the message text from a cron payload (Message, falling back to Text).
func cronPayloadMsg(p cron.StorePayload) string {
	if p.Message != "" {
		return p.Message
	}
	return p.Text
}
