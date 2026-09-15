package mailanalysis

import (
	"sync/atomic"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// localUsageTag holds the process-level sink for local-model helper usage. The
// stage-1 extractors (wiki facts, action items, deals, thread context, attachment
// gate) call the local model directly through callLocalTargetsJSON — never through the
// agent run loop — so their tokens carry no run.start/run.end and were invisible
// to per-model usage aggregation. Recording them as agentlog helper.llm events is
// how local models surface in the native usage screen.
type localUsageTag struct {
	writer   *agentlog.Writer
	role     string
	provider string
}

// pkgLocalUsage is set once at startup via SetLocalUsageLog. Every stage-1 call
// is made for one role (RoleTiny), so a single package-level tag is sufficient
// rather than threading a sink through every extractor. A fallback target names
// its own provider; the tag's is the stage-1 model's.
var pkgLocalUsage atomic.Pointer[localUsageTag]

// SetLocalUsageLog installs the agent-log writer plus the role and provider that
// stage-1 local calls should be attributed to. A nil writer clears nothing and is
// ignored so a mis-ordered startup never drops an already-installed sink.
func SetLocalUsageLog(w *agentlog.Writer, role, provider string) {
	if w == nil {
		return
	}
	pkgLocalUsage.Store(&localUsageTag{writer: w, role: role, provider: provider})
}

// emitLocalHelperUsage records one stage-1 local call's token usage as a helper.llm
// event. No-op when no sink is installed, the model is unknown, or the call
// reported no tokens (nothing to attribute).
func emitLocalHelperUsage(target LocalTarget, usage llm.TokenUsage) {
	tag := pkgLocalUsage.Load()
	if tag == nil || target.Model == "" || (usage.InputTokens == 0 && usage.OutputTokens == 0) {
		return
	}
	provider := tag.provider
	if target.Provider != "" {
		provider = target.Provider
	}
	agentlog.LogTyped(tag.writer, agentlog.SessionHelper, agentlog.TypeHelperLLM, agentlog.HelperLLMData{
		Model:           target.Model,
		Provider:        provider,
		Role:            tag.role,
		Purpose:         "mail-extract",
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.CacheReadInputTokens,
	})
}
