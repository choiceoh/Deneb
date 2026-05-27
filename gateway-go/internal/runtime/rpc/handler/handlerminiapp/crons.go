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
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CronLister is the subset of *cron.Service this handler needs. Defined
// here so tests can drop in a fake without booting the full Service.
type CronLister interface {
	ListPage(opts cron.ListPageOptions) cron.ListPageResult
}

// CronsDeps holds the cron service. Lazy factory so the gateway boots
// cleanly when crons aren't yet wired (e.g., during initial bring-up);
// the handler then surfaces UNAVAILABLE per call instead of crashing.
type CronsDeps struct {
	Service func() (CronLister, error)
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
		"miniapp.crons.list": cronsList(deps),
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
