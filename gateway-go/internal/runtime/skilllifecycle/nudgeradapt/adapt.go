package nudgeradapt

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/review"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type chatNudgerAdapter struct {
	inner *review.Nudger
}

// New adapts a review nudger to the stable chat port.
func New(n *review.Nudger) chatport.SkillNudger {
	return &chatNudgerAdapter{inner: n}
}

func (a *chatNudgerAdapter) Enabled() bool { return a.inner.Enabled() }

func (a *chatNudgerAdapter) OnToolCalls(ctx context.Context, sessionKey string, delta int, snap chatport.SkillNudgeSnapshot) {
	activities := make([]generation.ToolActivity, 0, len(snap.ToolActivities))
	for _, t := range snap.ToolActivities {
		activities = append(activities, generation.ToolActivity{
			Name: t.Name, IsError: t.IsError,
		})
	}
	a.inner.OnToolCalls(ctx, sessionKey, delta, generation.SessionContext{
		Key:            sessionKey,
		Label:          snap.Label,
		Model:          snap.Model,
		Turns:          snap.Turns,
		ToolActivities: activities,
		AllText:        snap.AllText,
	})
}

func (a *chatNudgerAdapter) Reset(sessionKey string) { a.inner.Reset(sessionKey) }
