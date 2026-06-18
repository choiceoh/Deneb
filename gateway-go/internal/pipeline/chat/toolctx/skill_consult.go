package toolctx

import (
	"context"
	"sync"
)

// SkillConsultLog records which skills the agent consulted (read via the
// `skills` tool's read action) during a single agent run. It feeds the
// Propus Evolver a real usage and success-rate signal: the run loop
// drains it per turn and attributes that turn's outcome (clean vs. a tool
// error) to each skill consulted, calling RecordUsage. Without this the
// Evolver's SkillsNeedingEvolution(minUses, maxSuccessRate) gate sees empty
// stats and never selects anything to improve — the loop runs but never
// converges on the skills that actually matter.
//
// Run-scoped: created once per agent run and shared with the skills tool via
// context (WithSkillConsultLog). Safe for concurrent use because the skills
// tool may execute on a parallel tool goroutine within a turn.
type SkillConsultLog struct {
	mu       sync.Mutex
	names    []string // consult order; may repeat across turns
	recorded int      // high-water mark already drained by the run loop
}

// NewSkillConsultLog creates an empty consult log.
func NewSkillConsultLog() *SkillConsultLog {
	return &SkillConsultLog{}
}

// Add records that skillName was consulted. No-op on a nil receiver or empty
// name so callers need no guard (a run without a recorder still calls Add).
func (l *SkillConsultLog) Add(skillName string) {
	if l == nil || skillName == "" {
		return
	}
	l.mu.Lock()
	l.names = append(l.names, skillName)
	l.mu.Unlock()
}

// DrainNew returns the distinct skills consulted since the previous DrainNew
// call (first-occurrence order). The run loop calls this once per turn so each
// turn's outcome is attributed only to the skills read during that turn; a
// skill read twice in the same window counts once.
func (l *SkillConsultLog) DrainNew() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.recorded >= len(l.names) {
		return nil
	}
	fresh := l.names[l.recorded:]
	l.recorded = len(l.names)
	seen := make(map[string]struct{}, len(fresh))
	out := make([]string, 0, len(fresh))
	for _, n := range fresh {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// WithSkillConsultLog attaches a SkillConsultLog to ctx so the skills tool can
// record consults during a run.
func WithSkillConsultLog(ctx context.Context, l *SkillConsultLog) context.Context {
	return context.WithValue(ctx, ctxKeySkillConsult, l)
}

// SkillConsultLogFromContext extracts the SkillConsultLog from ctx, or nil.
func SkillConsultLogFromContext(ctx context.Context) *SkillConsultLog {
	l, _ := ctx.Value(ctxKeySkillConsult).(*SkillConsultLog)
	return l
}
