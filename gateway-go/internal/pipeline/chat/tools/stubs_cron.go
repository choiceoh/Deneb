package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- cron tool ---

// ToolCron returns a tool function that manages cron jobs.
// When Service is available, uses persistent storage with full cron expression support.
// Falls back to basic Scheduler for in-memory operation.
func ToolCron(d *toolctx.ChronoDeps) ToolFunc {
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
		}
		if err := jsonutil.UnmarshalInto("cron params", input, &p); err != nil {
			return "", err
		}

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
		nextTime := time.UnixMilli(st.NextRunAtMs).Format("2006-01-02 15:04")
		rel := cron.FormatRelativeTime(st.NextRunAtMs)
		// Find which job is next.
		nextJobName := ""
		for _, j := range jobs {
			if j.Enabled && j.State.NextRunAtMs == st.NextRunAtMs {
				nextJobName = j.Name
				break
			}
		}
		if nextJobName != "" {
			fmt.Fprintf(&sb, "- 다음 실행: %s — %s (%s)", nextJobName, nextTime, rel)
		} else {
			fmt.Fprintf(&sb, "- 다음 실행: %s (%s)", nextTime, rel)
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
		cmd := j.Payload.Message
		if cmd == "" {
			cmd = j.Payload.Text
		}
		if cmd != "" {
			if len(cmd) > 80 {
				cmd = cmd[:77] + "..."
			}
			fmt.Fprintf(&sb, "  명령: %s\n", cmd)
		}
		if j.State.ConsecutiveErrors > 0 {
			fmt.Fprintf(&sb, "  ⚠️ 연속 오류: %d회\n", j.State.ConsecutiveErrors)
		}
	}
	return sb.String(), nil
}

func cronAdd(ctx context.Context, d *toolctx.ChronoDeps, name, schedule, command string, enabled *bool, jobObj map[string]any, opts cronToolOpts) (string, error) {
	// Support nested job object.
	if jobObj != nil {
		if v, ok := jobObj["name"].(string); ok && name == "" {
			name = v
		}
		if v, ok := jobObj["schedule"].(string); ok && schedule == "" {
			schedule = v
		}
		if v, ok := jobObj["command"].(string); ok && command == "" {
			command = v
		}
	}
	if name == "" || schedule == "" || command == "" {
		return "", fmt.Errorf("name, schedule, command 모두 필요합니다. 예: cron add name=daily schedule='0 9 * * *' command='뉴스 확인'")
	}
	const maxCommandLen = 4096
	if len(command) > maxCommandLen {
		return "", fmt.Errorf("command가 최대 길이 %d자를 초과합니다", maxCommandLen)
	}

	if d.Service != nil {
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

		job := cron.StoreJob{
			ID:       name,
			Name:     name,
			Enabled:  isEnabled,
			Schedule: storeSched,
			Payload:  payload,
		}

		// Apply delivery mode.
		if opts.DeliveryMode == "none" {
			// Explicit no-delivery: leave Delivery nil (agent runs silently).
		} else if opts.DeliveryMode == "announce" || opts.DeliveryMode == "" {
			// Default: capture delivery context from the creating session so the
			// cron job knows where to send output (e.g. Telegram chat ID).
			if delivery := toolctx.DeliveryFromContext(ctx); delivery != nil && delivery.To != "" {
				job.Delivery = &cron.JobDeliveryConfig{
					Channel: delivery.Channel,
					To:      delivery.To,
				}
			}
		}

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
			fmt.Fprintf(&sb, "- 다음 실행: %s (%s)\n",
				time.UnixMilli(nextMs).Format("2006-01-02 15:04"),
				cron.FormatRelativeTime(nextMs))
		}
		if payload.RetryCount > 0 {
			fmt.Fprintf(&sb, "- 재시도: 최대 %d회\n", payload.RetryCount)
		}
		return sb.String(), nil
	}

	return "", fmt.Errorf("크론 서비스를 사용할 수 없습니다")
}

func cronUpdate(ctx context.Context, d *toolctx.ChronoDeps, jobID, name, schedule, command string, enabled *bool, opts cronToolOpts) (string, error) {
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
			fmt.Fprintf(&sb, "- 다음 실행: %s (%s)",
				time.UnixMilli(job.State.NextRunAtMs).Format("2006-01-02 15:04"),
				cron.FormatRelativeTime(job.State.NextRunAtMs))
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("업데이트에는 영속 크론 서비스가 필요합니다 (사용 불가)")
}

func cronRemove(d *toolctx.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	if err := d.Service.Remove(jobID); err != nil {
		return "", fmt.Errorf("삭제 실패: %w", err)
	}
	return fmt.Sprintf("✅ 크론 작업 **%s** 삭제 완료.", jobID), nil
}

func cronRun(ctx context.Context, d *toolctx.ChronoDeps, jobID string) (string, error) {
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

func cronGet(d *toolctx.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId가 필요합니다. cron list로 ID를 확인하세요.")
	}
	job := d.Service.Job(jobID)
	if job == nil {
		return fmt.Sprintf("크론 작업 %q을(를) 찾을 수 없습니다.", jobID), nil
	}
	{
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

		cmd := job.Payload.Message
		if cmd == "" {
			cmd = job.Payload.Text
		}
		if cmd != "" {
			if len(cmd) > 120 {
				cmd = cmd[:117] + "..."
			}
			fmt.Fprintf(&sb, "- 명령: %s\n", cmd)
		}

		if job.Enabled && job.State.NextRunAtMs > 0 {
			fmt.Fprintf(&sb, "- 다음 실행: %s (%s)\n",
				time.UnixMilli(job.State.NextRunAtMs).Format("2006-01-02 15:04"),
				cron.FormatRelativeTime(job.State.NextRunAtMs))
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
}

func cronRuns(d *toolctx.ChronoDeps, jobID string, limit int) (string, error) {
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
				errShort := e.Error
				if len(errShort) > 60 {
					errShort = errShort[:57] + "..."
				}
				errStr = fmt.Sprintf(" — %s", errShort)
			}

			summary := ""
			if e.Summary != "" && e.Error == "" {
				s := e.Summary
				if len(s) > 60 {
					s = s[:57] + "..."
				}
				summary = fmt.Sprintf(" — %s", s)
			}

			fmt.Fprintf(&sb, "\n%s [%s] %s (%s%s%s%s)%s%s",
				icon, ts, e.JobID, e.Status, dur, deliveryStr, retryStr, errStr, summary)
		}
		return sb.String(), nil
	}
	return "실행 이력을 사용할 수 없습니다.", nil
}
