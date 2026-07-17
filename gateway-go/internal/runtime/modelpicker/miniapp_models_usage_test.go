package modelpicker

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// The 24h usage map keys stats by provider/model AND bare model name (agent
// logs may omit the provider), and memoizes the aggregation — the underlying
// fold reads every agent-log file, far too heavy per picker render.
func TestUsage24hKeysAndMemoizes(t *testing.T) {
	calls := 0
	s := &Controller{usageStats: func(sinceMs int64) []agentlog.ModelStat {
		calls++
		if since := time.Now().Add(-25 * time.Hour).UnixMilli(); sinceMs < since {
			t.Errorf("cutoff older than 24h: %d", sinceMs)
		}
		return []agentlog.ModelStat{
			{Model: "k3[1m]", Provider: "kimi", Runs: 12, InputTokens: 1000, OutputTokens: 50, CacheReadTokens: 900},
			{Model: "glm-5.2", Runs: 3, InputTokens: 200, OutputTokens: 20},
		}
	}}

	usage := s.usage24h()
	if st := usage["kimi/k3[1m]"]; st.Runs != 12 || st.CacheReadTokens != 900 {
		t.Fatalf("provider-qualified key = %+v", st)
	}
	if st := usage["k3[1m]"]; st.Runs != 12 {
		t.Fatalf("bare-name fallback key = %+v", st)
	}
	if st := usage["glm-5.2"]; st.Runs != 3 {
		t.Fatalf("provider-less stat = %+v", st)
	}

	if s.usage24h(); calls != 1 {
		t.Fatalf("aggregation calls = %d, want memoized 1", calls)
	}
}

// A nil UsageStats (writer absent) must disable enrichment, not panic.
func TestUsage24hNilSource(t *testing.T) {
	s := &Controller{}
	if got := s.usage24h(); got != nil {
		t.Fatalf("usage24h without source = %v, want nil", got)
	}
}
