package relaylog

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// Writer is the behavior-log sink used by proactive relay decisions.
type Writer = agentlog.Writer

// Decision records one proactive.relay funnel decision.
func Decision(w *Writer, decision, reason string, contentLen int, preview string) {
	agentlog.LogTyped(w, agentlog.SessionProactive, agentlog.TypeProactiveRelay, agentlog.ProactiveRelayData{
		Decision:   decision,
		Reason:     reason,
		ContentLen: contentLen,
		Preview:    preview,
	})
}
