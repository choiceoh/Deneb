package chat

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Type aliases — canonical definitions are in toolport/.

// skillConsultLog records skills the agent consulted during a run so the run
// loop can attribute each turn's outcome to them (genesis usage signal).
type skillConsultLog = toolport.SkillConsultLog

// newSkillConsultLog creates an empty consult log for a new agent run.
func newSkillConsultLog() *skillConsultLog { return toolport.NewSkillConsultLog() }

// withSkillConsultLog attaches a skillConsultLog to ctx for the skills tool.
func withSkillConsultLog(ctx context.Context, l *skillConsultLog) context.Context {
	return toolport.WithSkillConsultLog(ctx, l)
}
