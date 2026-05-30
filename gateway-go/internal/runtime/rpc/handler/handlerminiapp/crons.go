// crons.go — miniapp.crons.* RPC handlers.
//
// Surfaces the cron job registry in a flat, mobile-friendly shape so the
// Mini App's 더보기 > ⚡ 자동 작업 view can render a list without the
// per-row plumbing the operator-tool `cron.listPage` RPC exposes (sort
// dirs, queries, pagination params). Heavy filter/sort still lives in
// cron.listPage; this handler is the read-only "what's wired up right
// now" surface for end users.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CronService is the subset of *cron.Service this handler needs. Defined
// here so tests can drop in a fake without booting the full Service.
//   - ListPage backs miniapp.crons.list.
//   - Job backs miniapp.crons.get (the per-row detail) — nil when absent.
//   - Update/Remove/EnqueueRun back the mutation methods (update/remove/run).
//     Update re-normalizes and reschedules the job after the patch; it does
//     NOT validate the schedule, so the handler must parse + reject bad
//     schedules before calling it.
type CronService interface {
	ListPage(opts cron.ListPageOptions) cron.ListPageResult
	Job(id string) *cron.StoreJob
	Update(ctx context.Context, id string, patch func(*cron.StoreJob)) error
	Remove(id string) error
	EnqueueRun(ctx context.Context, id string, mode string) error
}

// CronsDeps holds the cron service. Lazy factory so the gateway boots
// cleanly when crons aren't yet wired (e.g., during initial bring-up);
// the handler then surfaces UNAVAILABLE per call instead of crashing.
type CronsDeps struct {
	Service func() (CronService, error)
}

const (
	defaultCronListLimit  = 50
	maxCronListLimit      = 200
	maxCronPayloadPreview = 120 // runes
)

// CronsMethods returns the miniapp.crons.* handler map. Returns nil if
// no factory is provided so method_registry can register conditionally.
func CronsMethods(deps CronsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Service == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.crons.list":   cronsList(deps),
		"miniapp.crons.get":    cronsGet(deps),
		"miniapp.crons.update": cronsUpdate(deps),
		"miniapp.crons.run":    cronsRun(deps),
		"miniapp.crons.remove": cronsRemove(deps),
	}
}

// MiniappCronRow is one row in a cron job listing. Exported so tests can
// reference the field set; not consumed outside this package today.
type MiniappCronRow struct {
	ID                string `json:"id"`
	Name              string `json:"name,omitempty"`
	Enabled           bool   `json:"enabled"`
	Schedule          string `json:"schedule"`    // human-readable summary, Korean
	PayloadKind       string `json:"payloadKind"` // agentTurn | systemEvent
	PayloadPreview    string `json:"payloadPreview,omitempty"`
	NextRunAtMs       int64  `json:"nextRunAtMs,omitempty"`
	ConsecutiveErrors int    `json:"consecutiveErrors,omitempty"`
	AutoDisabledAtMs  int64  `json:"autoDisabledAtMs,omitempty"`
	LastError         string `json:"lastError,omitempty"`
}

// MiniappCronDetail is the full, mobile-friendly view of one cron job —
// what the Mini App's cron detail screen renders when a row is tapped.
// Unlike MiniappCronRow it carries the *full* prompt (not the 120-rune
// preview), the delivery target, the parsed schedule pieces, and the
// execution context (agent, session target, model/thinking/timeout/retry)
// so the operator can see exactly what a job does without dropping to the
// operator-tool `cron.getJob` RPC. Read-only — editing still lives there.
type MiniappCronDetail struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Enabled       bool   `json:"enabled"`
	AgentID       string `json:"agentId,omitempty"`
	SessionTarget string `json:"sessionTarget,omitempty"` // "main" | "isolated" | "current" | "subagent"

	// Schedule — humanized summary plus the raw pieces for the detail rows.
	Schedule     string `json:"schedule"`           // same one-line Korean summary as the list row
	ScheduleSpec string `json:"scheduleSpec"`       // round-trippable spec the edit form pre-fills ("0 9 * * *", "15m", ISO)
	ScheduleKind string `json:"scheduleKind"`       // "at" | "every" | "cron"
	Timezone     string `json:"timezone,omitempty"` // kind=cron
	CronExpr     string `json:"cronExpr,omitempty"` // kind=cron raw expression
	StaggerMs    int64  `json:"staggerMs,omitempty"`

	// Payload — the full instruction and how it runs.
	PayloadKind    string `json:"payloadKind"`      // "agentTurn" | "systemEvent"
	Prompt         string `json:"prompt,omitempty"` // FULL message/text, untruncated
	Model          string `json:"model,omitempty"`
	Thinking       string `json:"thinking,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	LightContext   bool   `json:"lightContext,omitempty"`
	RetryCount     int    `json:"retryCount,omitempty"`

	// Delivery — where the result is sent.
	DeliveryChannel  string `json:"deliveryChannel,omitempty"`
	DeliveryTo       string `json:"deliveryTo,omitempty"`
	DeliveryThreadID string `json:"deliveryThreadId,omitempty"`

	// Failure alert — how many consecutive failures trigger a heads-up.
	FailureAlertAfter int `json:"failureAlertAfter,omitempty"`

	// State — runtime bookkeeping.
	NextRunAtMs        int64  `json:"nextRunAtMs,omitempty"`
	LastSessionKey     string `json:"lastSessionKey,omitempty"`
	LastDeliveryStatus string `json:"lastDeliveryStatus,omitempty"`
	LastError          string `json:"lastError,omitempty"`
	ConsecutiveErrors  int    `json:"consecutiveErrors,omitempty"`
	AutoDisabledAtMs   int64  `json:"autoDisabledAtMs,omitempty"`
	CreatedAtMs        int64  `json:"createdAtMs,omitempty"`
	UpdatedAtMs        int64  `json:"updatedAtMs,omitempty"`
}

func cronsList(deps CronsDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit           int  `json:"limit,omitempty"`
		IncludeDisabled bool `json:"includeDisabled,omitempty"`
	}
	type out struct {
		Jobs  []MiniappCronRow `json:"jobs"`
		Total int              `json:"total"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultCronListLimit
		}
		if limit > maxCronListLimit {
			limit = maxCronListLimit
		}

		svc, err := deps.Service()
		if err != nil {
			return rpcerr.WrapUnavailable("cron service unavailable", err).Response(req.ID)
		}
		// Default sort: enabled-first via nextRunAtMs asc is a useful
		// "what fires next" view — the front of the list is the most
		// relevant to the user looking at automation.
		page := svc.ListPage(cron.ListPageOptions{
			Limit:           limit,
			Offset:          0,
			IncludeDisabled: true, // we surface disabled visibly; client filters by enabled flag if wanted
			SortBy:          "nextRunAtMs",
			SortDir:         "asc",
		})

		rows := make([]MiniappCronRow, 0, len(page.Jobs))
		for _, j := range page.Jobs {
			if !p.IncludeDisabled && !j.Enabled {
				continue
			}
			rows = append(rows, miniappRowFromJob(j))
		}
		return rpcutil.RespondOK(req.ID, out{
			Jobs:  rows,
			Total: page.Total,
		})
	}
}

func cronsGet(deps CronsDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		svc, err := deps.Service()
		if err != nil {
			return rpcerr.WrapUnavailable("cron service unavailable", err).Response(req.ID)
		}
		job := svc.Job(p.ID)
		if job == nil {
			return rpcerr.NotFound("cron job " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, miniappDetailFromJob(*job))
	}
}

func miniappDetailFromJob(j cron.StoreJob) MiniappCronDetail {
	d := MiniappCronDetail{
		ID:            j.ID,
		Name:          j.Name,
		Enabled:       j.Enabled,
		AgentID:       j.AgentID,
		SessionTarget: string(j.SessionTarget),

		Schedule:     formatCronSchedule(j.Schedule),
		ScheduleSpec: scheduleSpec(j.Schedule),
		ScheduleKind: j.Schedule.Kind,
		Timezone:     j.Schedule.Tz,
		CronExpr:     j.Schedule.Expr,
		StaggerMs:    j.Schedule.StaggerMs,

		PayloadKind:    j.Payload.Kind,
		Prompt:         payloadPreviewText(j.Payload), // full text; detail view has room
		Model:          j.Payload.Model,
		Thinking:       j.Payload.Thinking,
		TimeoutSeconds: j.Payload.TimeoutSeconds,
		LightContext:   j.Payload.LightContext,
		RetryCount:     j.Payload.RetryCount,

		NextRunAtMs:        j.State.NextRunAtMs,
		LastSessionKey:     j.State.LastSessionKey,
		LastDeliveryStatus: j.State.LastDeliveryStatus,
		LastError:          j.State.LastDeliveryError,
		ConsecutiveErrors:  j.State.ConsecutiveErrors,
		AutoDisabledAtMs:   j.State.AutoDisabledAtMs,
		CreatedAtMs:        j.CreatedAtMs,
		UpdatedAtMs:        j.UpdatedAtMs,
	}
	if d.Name == "" {
		d.Name = j.ID
	}
	if j.Delivery != nil {
		d.DeliveryChannel = j.Delivery.Channel
		d.DeliveryTo = j.Delivery.To
		d.DeliveryThreadID = j.Delivery.ThreadID
	}
	if j.FailureAlert != nil {
		d.FailureAlertAfter = j.FailureAlert.After
	}
	return d
}

// cronsUpdate patches an existing job. Every field is optional (pointer):
// nil means "leave untouched", so the same method backs both the full edit
// form and the one-field enable/disable toggle. Schedule is validated here
// (Service.Update does not), so a bad spec is rejected before any write.
func cronsUpdate(deps CronsDeps) rpcutil.HandlerFunc {
	type deliveryParams struct {
		Channel  *string `json:"channel,omitempty"`
		To       *string `json:"to,omitempty"`
		ThreadID *string `json:"threadId,omitempty"`
	}
	type params struct {
		ID             string          `json:"id"`
		Name           *string         `json:"name,omitempty"`
		Enabled        *bool           `json:"enabled,omitempty"`
		Schedule       *string         `json:"schedule,omitempty"` // smart spec ("0 9 * * *", "15m", ISO)
		Tz             *string         `json:"tz,omitempty"`
		Prompt         *string         `json:"prompt,omitempty"`
		Model          *string         `json:"model,omitempty"`
		Thinking       *string         `json:"thinking,omitempty"`
		TimeoutSeconds *int            `json:"timeoutSeconds,omitempty"`
		RetryCount     *int            `json:"retryCount,omitempty"`
		AgentID        *string         `json:"agentId,omitempty"`
		Delivery       *deliveryParams `json:"delivery,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		svc, err := deps.Service()
		if err != nil {
			return rpcerr.WrapUnavailable("cron service unavailable", err).Response(req.ID)
		}
		existing := svc.Job(p.ID)
		if existing == nil {
			return rpcerr.NotFound("cron job " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}

		// Pre-parse the schedule when the spec or tz changed. Service.Update
		// only recomputes the next-run time; it never rejects a malformed
		// schedule, so validating here is the only guard against a job that
		// silently never fires. Existing stagger/anchor are re-applied so an
		// edit to the expression alone doesn't drop them.
		var newSchedule *cron.StoreSchedule
		if p.Schedule != nil || p.Tz != nil {
			spec := strings.TrimSpace(derefStr(p.Schedule, scheduleSpec(existing.Schedule)))
			if spec == "" {
				return rpcerr.ValidationFailed("schedule cannot be empty").Response(req.ID)
			}
			parsed, perr := cron.ParseSmartScheduleWithOpts(spec, cron.SmartScheduleOpts{
				Tz:         strings.TrimSpace(derefStr(p.Tz, existing.Schedule.Tz)),
				StaggerMs:  existing.Schedule.StaggerMs,
				AnchorTime: anchorISO(existing.Schedule.AnchorMs),
			})
			if perr != nil {
				return rpcerr.Newf(protocol.ErrValidationFailed, "invalid schedule: %v", perr).Response(req.ID)
			}
			newSchedule = &parsed
		}

		uerr := svc.Update(ctx, p.ID, func(j *cron.StoreJob) {
			if p.Name != nil {
				j.Name = strings.TrimSpace(*p.Name)
			}
			if p.Enabled != nil {
				j.Enabled = *p.Enabled
			}
			if newSchedule != nil {
				j.Schedule = *newSchedule
			}
			if p.Prompt != nil {
				// Write into the field matching the existing payload kind so
				// the detail view and the runner read the same place.
				if j.Payload.Kind == "systemEvent" {
					j.Payload.Text = *p.Prompt
				} else {
					j.Payload.Message = *p.Prompt
				}
			}
			if p.Model != nil {
				j.Payload.Model = strings.TrimSpace(*p.Model)
			}
			if p.Thinking != nil {
				j.Payload.Thinking = strings.TrimSpace(*p.Thinking)
			}
			if p.TimeoutSeconds != nil {
				j.Payload.TimeoutSeconds = clampNonNeg(*p.TimeoutSeconds)
			}
			if p.RetryCount != nil {
				j.Payload.RetryCount = clampRetry(*p.RetryCount)
			}
			if p.AgentID != nil {
				j.AgentID = strings.TrimSpace(*p.AgentID)
			}
			if p.Delivery != nil {
				if j.Delivery == nil {
					j.Delivery = &cron.JobDeliveryConfig{}
				}
				if p.Delivery.Channel != nil {
					j.Delivery.Channel = strings.TrimSpace(*p.Delivery.Channel)
				}
				if p.Delivery.To != nil {
					j.Delivery.To = strings.TrimSpace(*p.Delivery.To)
				}
				if p.Delivery.ThreadID != nil {
					j.Delivery.ThreadID = strings.TrimSpace(*p.Delivery.ThreadID)
				}
			}
		})
		if uerr != nil {
			return rpcerr.Wrap(protocol.ErrNotFound, uerr).Response(req.ID)
		}

		updated := svc.Job(p.ID)
		if updated == nil {
			return rpcerr.NotFound("cron job " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, miniappDetailFromJob(*updated))
	}
}

// cronsRun queues an immediate run. Async (EnqueueRun) so the phone doesn't
// block on a full agent turn — the result is delivered through the job's
// normal cron delivery path, same as a scheduled fire.
func cronsRun(deps CronsDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		svc, err := deps.Service()
		if err != nil {
			return rpcerr.WrapUnavailable("cron service unavailable", err).Response(req.ID)
		}
		if svc.Job(p.ID) == nil {
			return rpcerr.NotFound("cron job " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		if err := svc.EnqueueRun(ctx, p.ID, "manual"); err != nil {
			return rpcerr.WrapDependencyFailed("enqueue run failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"enqueued": true})
	}
}

// cronsRemove deletes a job. Existence is checked first so removing an
// unknown ID is a clean NOT_FOUND rather than a silent success (the store's
// filter-on-remove returns nil for a missing ID).
func cronsRemove(deps CronsDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		svc, err := deps.Service()
		if err != nil {
			return rpcerr.WrapUnavailable("cron service unavailable", err).Response(req.ID)
		}
		if svc.Job(p.ID) == nil {
			return rpcerr.NotFound("cron job " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		if err := svc.Remove(p.ID); err != nil {
			return rpcerr.WrapDependencyFailed("remove failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"removed": true})
	}
}

// scheduleSpec renders a StoreSchedule back into the string form the smart
// parser accepts, so the edit form can pre-fill an editable, round-trippable
// value: the cron expression, a Go-duration interval, or the "at" timestamp.
func scheduleSpec(s cron.StoreSchedule) string {
	switch s.Kind {
	case "cron":
		return s.Expr
	case "every":
		return formatEveryMs(s.EveryMs)
	case "at":
		return s.At
	default:
		return ""
	}
}

// formatEveryMs renders an interval as a compact Go-duration string ("15m",
// "1h30m", "30s") — accepted by ParseSmartSchedule's parseIntervalMs.
func formatEveryMs(ms int64) string {
	if ms <= 0 {
		return ""
	}
	total := ms / 1000 // whole seconds; sub-second intervals are not used
	h := total / 3600
	m := (total % 3600) / 60
	sec := total % 60
	out := ""
	if h > 0 {
		out += fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		out += fmt.Sprintf("%dm", m)
	}
	if sec > 0 {
		out += fmt.Sprintf("%ds", sec)
	}
	if out == "" {
		return "0s"
	}
	return out
}

// anchorISO renders an interval anchor (ms) as RFC3339 for re-application
// through SmartScheduleOpts, or "" when there is no anchor.
func anchorISO(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func derefStr(p *string, def string) string {
	if p != nil {
		return *p
	}
	return def
}

func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// clampRetry bounds retries to [0, 3] — the store's documented ceiling.
func clampRetry(n int) int {
	if n < 0 {
		return 0
	}
	if n > 3 {
		return 3
	}
	return n
}

func miniappRowFromJob(j cron.StoreJob) MiniappCronRow {
	row := MiniappCronRow{
		ID:                j.ID,
		Name:              j.Name,
		Enabled:           j.Enabled,
		Schedule:          formatCronSchedule(j.Schedule),
		PayloadKind:       j.Payload.Kind,
		PayloadPreview:    truncateRunes(payloadPreviewText(j.Payload), maxCronPayloadPreview),
		NextRunAtMs:       j.State.NextRunAtMs,
		ConsecutiveErrors: j.State.ConsecutiveErrors,
		AutoDisabledAtMs:  j.State.AutoDisabledAtMs,
		LastError:         j.State.LastDeliveryError,
	}
	if row.Name == "" {
		row.Name = j.ID
	}
	return row
}

// payloadPreviewText picks the human-meaningful field from a payload —
// message for agentTurn, text for systemEvent. The Mini App row only
// has space for ~one line so we don't try to be clever.
func payloadPreviewText(p cron.StorePayload) string {
	if p.Message != "" {
		return p.Message
	}
	return p.Text
}

// formatCronSchedule renders a StoreSchedule as a short Korean phrase.
// One-shot "at" returns "1회: <local time>"; "every" returns "N분/시간/일마다";
// "cron" returns the expression literal (parsing every cron variant for
// natural language would be a separate effort — operators tend to know
// what their expressions mean).
func formatCronSchedule(s cron.StoreSchedule) string {
	switch s.Kind {
	case "at":
		t, err := time.Parse(time.RFC3339, s.At)
		if err != nil {
			return "1회: " + s.At
		}
		return "1회: " + t.Local().Format("2006-01-02 15:04")
	case "every":
		if s.EveryMs <= 0 {
			return "주기 미정"
		}
		return humanizeInterval(time.Duration(s.EveryMs) * time.Millisecond)
	case "cron":
		if s.Tz != "" {
			return fmt.Sprintf("%s (%s)", s.Expr, s.Tz)
		}
		return s.Expr
	default:
		return s.Kind
	}
}

func humanizeInterval(d time.Duration) string {
	if d < time.Minute {
		secs := int(d.Round(time.Second).Seconds())
		return fmt.Sprintf("%d초마다", secs)
	}
	if d < time.Hour {
		mins := int(d.Round(time.Minute).Minutes())
		return fmt.Sprintf("%d분마다", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Round(time.Hour).Hours())
		return fmt.Sprintf("%d시간마다", hours)
	}
	days := int(d.Round(24*time.Hour).Hours() / 24)
	return fmt.Sprintf("%d일마다", days)
}
