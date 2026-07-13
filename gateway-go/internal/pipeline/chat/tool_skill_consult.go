package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Type aliases — canonical definitions are in toolport/.

// SkillConsultLog records skills the agent consulted during a run so the run
// loop can attribute each turn's outcome to them (genesis usage signal).
type SkillConsultLog = toolport.SkillConsultLog

// NewSkillConsultLog creates an empty consult log for a new agent run.
func NewSkillConsultLog() *SkillConsultLog { return toolport.NewSkillConsultLog() }

// WithSkillConsultLog attaches a SkillConsultLog to ctx for the skills tool.
func WithSkillConsultLog(ctx context.Context, l *SkillConsultLog) context.Context {
	return toolport.WithSkillConsultLog(ctx, l)
}
