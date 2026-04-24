// Package insights aggregates recent session/model/tool usage into a concise
// report that can be rendered for Telegram or CLI output.
//
// The engine is read-only — it never mutates session or usage state. Deneb's
// session manager keeps a bounded in-memory window (GC retention: 1h for most
// kinds, 24h for cron); cost data is not currently tracked and reports zero.
// When future schema adds persistence or cost, the report shapes already hold
// the fields so rendering will light up automatically.
//
// Inspired by Hermes' InsightsEngine (agent/insights.py), adapted for Deneb's
// single-user, in-memory, Korean-first deployment.
package insights

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// SessionLister is the narrow interface the engine needs from the session
// manager. Using an interface keeps tests fast (no Manager goroutines needed)
// and avoids coupling to future manager expansions.
type SessionLister interface {
	List() []*session.Session
	Count() int
}

// UsageSource is the narrow interface the engine needs from the usage tracker.
// Both the *usage.Tracker and a test fake can satisfy it.
type UsageSource interface {
	Status() *usage.StatusReport
}

// Engine produces insight reports from session + usage state.
// Zero value is not usable; always construct via New.
type Engine struct {
	sessions SessionLister
	usage    UsageSource // may be nil if no tracker is wired
	now      func() time.Time

	// toolAggregator is an optional hook for future tool-call tracking.
	// Today Deneb does not persist per-tool invocation counts, so this
	// returns nil by default.
	toolAggregator func(ctx context.Context, since time.Time) []ToolStat

	// mu guards the optional hooks from concurrent override (tests).
	mu sync.RWMutex
}

// New constructs an Engine. `sessions` is required; `u` may be nil.
// The clock defaults to dentime.Now so the report's GeneratedAt and window
// boundaries are reported in the operator's configured zone.
func New(sessions SessionLister, u UsageSource) *Engine {
	return &Engine{
		sessions: sessions,
		usage:    u,
		now:      dentime.Now,
	}
}

// SetToolAggregator installs a custom tool aggregator (used by tests, or by
// future code that tracks per-tool usage). nil disables tool stats.
func (e *Engine) SetToolAggregator(fn func(ctx context.Context, since time.Time) []ToolStat) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.toolAggregator = fn
}

// Overview captures top-level aggregates for the window.
type Overview struct {
	Sessions     int       `json:"sessions"`
	Messages     int       `json:"messages"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	TotalTokens  int64     `json:"totalTokens"`
	CostUSD      float64   `json:"costUsd"`
	Since        time.Time `json:"since"`
	ActiveNow    int       `json:"activeNow"`
}

// ModelStat aggregates usage by model name.
type ModelStat struct {
	Model        string  `json:"model"`
	Sessions     int     `json:"sessions"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	TotalTokens  int64   `json:"totalTokens"`
	CostUSD      float64 `json:"costUsd"`
}

// ToolStat aggregates tool-call counts. ErrorRate is 0..1; zero when unknown.
type ToolStat struct {
	Name      string  `json:"name"`
	Calls     int     `json:"calls"`
	ErrorRate float64 `json:"errorRate"`
}

// SessionStat summarizes a single session for "top sessions" ranking.
type SessionStat struct {
	Key          string `json:"key"`
	Channel      string `json:"channel,omitempty"`
	Model        string `json:"model,omitempty"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	TotalTokens  int64  `json:"totalTokens"`
	Status       string `json:"status,omitempty"`
	Kind         string `json:"kind,omitempty"`
	DurationMs   int64  `json:"durationMs,omitempty"`
}

// ProviderStat captures per-provider totals (from usage.Tracker).
// Deneb's usage tracker is reset on gateway restart, so this reflects
// "since restart" rather than "last N days" — labeled in the renderer.
type ProviderStat struct {
	Provider   string `json:"provider"`
	Calls      int64  `json:"calls"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheRead  int64  `json:"cacheRead"`
	CacheWrite int64  `json:"cacheWrite"`
}

// Report is the full insights payload.
type Report struct {
	Days        int            `json:"days"`
	Since       time.Time      `json:"since"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Empty       bool           `json:"empty"`
	Overview    Overview       `json:"overview"`
	Models      []ModelStat    `json:"models"`
	Tools       []ToolStat     `json:"tools"`
	TopSessions []SessionStat  `json:"topSessions"`
	Providers   []ProviderStat `json:"providers"`
	UptimeSince string         `json:"uptimeSince,omitempty"`
	SchemaNotes []string       `json:"schemaNotes,omitempty"`
}

// Generate returns a report for the last `days` days. Values <= 0 default to 30.
// The call never returns a partial error: schema gaps (no cost, no tool counts)
// are reported as zero plus a note in Report.SchemaNotes.
func (e *Engine) Generate(ctx context.Context, days int) (*Report, error) {
	if days <= 0 {
		days = 30
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := e.now()
	since := now.Add(-time.Duration(days) * 24 * time.Hour)

	all := e.sessions.List()
	windowed := filterSessions(all, since)

	r := &Report{
		Days:        days,
		Since:       since,
		GeneratedAt: now,
	}

	if len(windowed) == 0 {
		r.Empty = true
	}

	r.Overview = computeOverview(windowed, since, e.sessions.Count())
	r.Models = computeModelStats(windowed)
	r.TopSessions = computeTopSessions(windowed, 5)

	// Tool stats: only if a custom aggregator is wired. Deneb's default
	// pipeline does not persist per-tool invocation counts, so this is
	// empty unless future code installs a hook.
	e.mu.RLock()
	agg := e.toolAggregator
	e.mu.RUnlock()
	if agg != nil {
		r.Tools = agg(ctx, since)
	}

	// Providers (from usage tracker — "since restart", not windowed).
	if e.usage != nil {
		if rep := e.usage.Status(); rep != nil {
			r.UptimeSince = rep.StartedAt
			r.Providers = toProviderStats(rep.Providers)
		}
	}

	r.SchemaNotes = buildSchemaNotes(r)
	return r, nil
}

// filterSessions returns sessions active within the window. A session is in-window
// when either StartedAt, EndedAt, or UpdatedAt (fallback) is >= since.
func filterSessions(sessions []*session.Session, since time.Time) []*session.Session {
	cutoff := since.UnixMilli()
	out := make([]*session.Session, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if s.Kind.IsInternal() {
			// Skip cron/subagent — those are system-internal, not user usage.
			continue
		}
		ref := s.UpdatedAt
		if s.StartedAt != nil && *s.StartedAt > 0 {
			ref = *s.StartedAt
		}
		if ref == 0 {
			continue
		}
		if ref >= cutoff {
			out = append(out, s)
		}
	}
	return out
}

// computeOverview aggregates session counts, token totals, and active-run count.
func computeOverview(sessions []*session.Session, since time.Time, active int) Overview {
	o := Overview{
		Since:     since,
		Sessions:  len(sessions),
		ActiveNow: active,
	}
	for _, s := range sessions {
		if s.InputTokens != nil {
			o.InputTokens += *s.InputTokens
		}
		if s.OutputTokens != nil {
			o.OutputTokens += *s.OutputTokens
		}
		if s.TotalTokens != nil {
			o.TotalTokens += *s.TotalTokens
		}
	}
	// If TotalTokens wasn't populated per-session, derive from I/O.
	if o.TotalTokens == 0 {
		o.TotalTokens = o.InputTokens + o.OutputTokens
	}
	// Messages: Deneb does not currently store per-session message counts in
	// the Session struct. Leave as 0 until a counter is added.
	return o
}

// computeModelStats groups sessions by model. Models without a name become "unknown".
func computeModelStats(sessions []*session.Session) []ModelStat {
	byModel := make(map[string]*ModelStat)
	for _, s := range sessions {
		m := s.Model
		if m == "" {
			m = "unknown"
		}
		stat, ok := byModel[m]
		if !ok {
			stat = &ModelStat{Model: m}
			byModel[m] = stat
		}
		stat.Sessions++
		if s.InputTokens != nil {
			stat.InputTokens += *s.InputTokens
		}
		if s.OutputTokens != nil {
			stat.OutputTokens += *s.OutputTokens
		}
		if s.TotalTokens != nil {
			stat.TotalTokens += *s.TotalTokens
		} else {
			stat.TotalTokens += ptrInt64(s.InputTokens) + ptrInt64(s.OutputTokens)
		}
	}

	out := make([]ModelStat, 0, len(byModel))
	for _, v := range byModel {
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		return out[i].Sessions > out[j].Sessions
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// computeTopSessions returns the top-N sessions by total tokens (with a stable
// tiebreaker by updatedAt).
func computeTopSessions(sessions []*session.Session, limit int) []SessionStat {
	if limit <= 0 {
		return nil
	}
	out := make([]SessionStat, 0, len(sessions))
	for _, s := range sessions {
		stat := SessionStat{
			Key:     s.Key,
			Channel: s.Channel,
			Model:   s.Model,
			Status:  string(s.Status),
			Kind:    string(s.Kind),
		}
		if s.InputTokens != nil {
			stat.InputTokens = *s.InputTokens
		}
		if s.OutputTokens != nil {
			stat.OutputTokens = *s.OutputTokens
		}
		if s.TotalTokens != nil {
			stat.TotalTokens = *s.TotalTokens
		} else {
			stat.TotalTokens = stat.InputTokens + stat.OutputTokens
		}
		if s.RuntimeMs != nil {
			stat.DurationMs = *s.RuntimeMs
		}
		out = append(out, stat)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > limit {
		out = out[:limit]
	}
	// Drop zero-token entries to avoid padding the list with empty stubs.
	trimmed := out[:0]
	for _, s := range out {
		if s.TotalTokens == 0 && s.InputTokens == 0 && s.OutputTokens == 0 {
			continue
		}
		trimmed = append(trimmed, s)
	}
	return trimmed
}

// toProviderStats converts the usage tracker's map into a sorted slice.
func toProviderStats(m map[string]*usage.ProviderStats) []ProviderStat {
	if len(m) == 0 {
		return nil
	}
	out := make([]ProviderStat, 0, len(m))
	for name, s := range m {
		if s == nil {
			continue
		}
		out = append(out, ProviderStat{
			Provider:   name,
			Calls:      s.Calls,
			Input:      s.Tokens.Input,
			Output:     s.Tokens.Output,
			CacheRead:  s.Tokens.CacheRead,
			CacheWrite: s.Tokens.CacheWrite,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

// buildSchemaNotes surfaces the current schema gaps so the renderer can show
// honest footnotes rather than silently showing zeros.
func buildSchemaNotes(r *Report) []string {
	var notes []string
	if r.Overview.CostUSD == 0 {
		notes = append(notes, "비용 추적 미지원 — 토큰만 표시합니다")
	}
	if len(r.Tools) == 0 {
		notes = append(notes, "도구 사용량 수집 미연결 — 상위 도구는 비어 있습니다")
	}
	if r.Overview.Messages == 0 && r.Overview.Sessions > 0 {
		notes = append(notes, "메시지 수 추적 미지원")
	}
	if r.Overview.Sessions == 0 {
		notes = append(notes, "세션 보관 기간을 지나 집계 대상이 없습니다 (메모리 GC: 기본 1시간)")
	}
	return notes
}

func ptrInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
