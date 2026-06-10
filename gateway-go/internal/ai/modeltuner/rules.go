package modeltuner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
)

// Rule thresholds. All rules require minRunsForRules completed runs in the
// stats window so a single bad run cannot trigger a recommendation. The only
// auto-applied adjustment is the max-tokens floor (raise-only, bounded by
// tunedMaxTokensFloor); everything else is operator-facing advice — the
// project principle is "자동 조정은 임계가 명확한 규칙만".
const (
	minRunsForRules = 5

	// truncationRateThreshold: max-tokens recoveries per run above which the
	// model clearly needs a larger output budget.
	truncationRateThreshold = 0.2
	// tunedMaxTokensFloor is the output budget floor auto-applied for models
	// that keep hitting the ceiling — 2× the chat default (8192).
	tunedMaxTokensFloor = 16_384

	// timeoutRateThreshold: fraction of runs ending in a stall ("timeout"
	// with no output) above which the model is flagged.
	timeoutRateThreshold = 0.2

	// cacheReadRatioFloor: for providers that participate in prompt caching
	// (cacheCreation > 0), a read ratio below this across the window means
	// the cache prefix is breaking — something is mutating the prompt.
	cacheReadRatioFloor = 0.2

	// p95LatencyThresholdMs flags models whose slow tail hurts the
	// chief-of-staff use case (imminent-meeting prep can't wait minutes).
	p95LatencyThresholdMs = 120_000

	// toolErrorRateThreshold flags models that mis-call tools.
	toolErrorRateThreshold = 0.15
	minToolCallsForRule    = 20

	// fallbackRateThreshold: fraction of runs answered by a different model
	// (fallback rescues) above which the requested model is flagged — it is
	// effectively delegating its job.
	fallbackRateThreshold = 0.3
)

// Recommendation is one rule firing for one model. TunedMaxTokens, when
// non-zero, is auto-applied via modelrole.Registry.SetTunedMaxTokens; all
// other recommendations are delivered as advice only.
type Recommendation struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
	Rule     string `json:"rule"` // max_tokens | stall | cache_break | latency | tool_errors
	Message  string `json:"message"`
	// TunedMaxTokens is the auto-applied output-token floor (raise-only).
	TunedMaxTokens int `json:"tunedMaxTokens,omitempty"`
}

// Analyze evaluates the tuning rules against per-model stats and returns
// recommendations sorted by model then rule for a stable fingerprint.
func Analyze(stats []agentlog.ModelStat) []Recommendation {
	var recs []Recommendation
	for _, s := range stats {
		if s.Runs < minRunsForRules {
			continue
		}
		runs := float64(s.Runs)

		if float64(s.MaxTokensRecoveries)/runs >= truncationRateThreshold {
			recs = append(recs, Recommendation{
				Model: s.Model, Provider: s.Provider, Rule: "max_tokens",
				TunedMaxTokens: tunedMaxTokensFloor,
				Message: fmt.Sprintf("출력 한도 초과 복구가 %d회/%d런 발생 — 출력 토큰 한도를 %d으로 자동 상향했습니다.",
					s.MaxTokensRecoveries, s.Runs, tunedMaxTokensFloor),
			})
		}

		if float64(s.TimeoutRuns)/runs >= timeoutRateThreshold {
			recs = append(recs, Recommendation{
				Model: s.Model, Provider: s.Provider, Rule: "stall",
				Message: fmt.Sprintf("스톨(무출력 타임아웃)이 %d/%d런 — 모델 상태 점검 또는 역할 교체를 검토하세요.",
					s.TimeoutRuns, s.Runs),
			})
		}

		// Cache health only judged where caching is actually in play.
		if s.CacheCreationTokens > 0 {
			denom := float64(s.CacheReadTokens + s.InputTokens)
			if denom > 0 && float64(s.CacheReadTokens)/denom < cacheReadRatioFloor {
				recs = append(recs, Recommendation{
					Model: s.Model, Provider: s.Provider, Rule: "cache_break",
					Message: fmt.Sprintf("프롬프트 캐시 히트율 %.0f%% — 캐시 prefix가 깨지고 있습니다 (.claude/rules/prompt-cache.md 참조).",
						float64(s.CacheReadTokens)/denom*100),
				})
			}
		}

		if s.P95Ms > p95LatencyThresholdMs {
			msg := fmt.Sprintf("p95 레이턴시 %.0f초 — 경량 역할 재배치를 검토하세요.", float64(s.P95Ms)/1000)
			if s.ThinkingRuns > 0 {
				msg = fmt.Sprintf("p95 레이턴시 %.0f초 (thinking 런 %d/%d) — thinking 레벨 하향 또는 경량 역할 재배치를 검토하세요.",
					float64(s.P95Ms)/1000, s.ThinkingRuns, s.Runs)
			}
			recs = append(recs, Recommendation{
				Model: s.Model, Provider: s.Provider, Rule: "latency", Message: msg,
			})
		}

		if float64(s.FallbackRuns)/runs >= fallbackRateThreshold {
			recs = append(recs, Recommendation{
				Model: s.Model, Provider: s.Provider, Rule: "fallback",
				Message: fmt.Sprintf("폴백 구조가 %d/%d런 — 이 모델이 사실상 일을 위임하고 있습니다. 역할 교체 또는 서버 점검을 검토하세요.",
					s.FallbackRuns, s.Runs),
			})
		}

		if s.ToolCalls >= minToolCallsForRule &&
			float64(s.ToolErrors)/float64(s.ToolCalls) >= toolErrorRateThreshold {
			recs = append(recs, Recommendation{
				Model: s.Model, Provider: s.Provider, Rule: "tool_errors",
				Message: fmt.Sprintf("도구 호출 에러율 %.0f%% (%d/%d) — 도구 스키마 호환성을 점검하세요.",
					float64(s.ToolErrors)/float64(s.ToolCalls)*100, s.ToolErrors, s.ToolCalls),
			})
		}
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Model != recs[j].Model {
			return recs[i].Model < recs[j].Model
		}
		return recs[i].Rule < recs[j].Rule
	})
	return recs
}

// Fingerprint identifies a recommendation set so the tuner notifies only on
// change (over-notification 금지). Messages are excluded — counts inside a
// message shifting slightly between cycles is not a new situation.
func Fingerprint(recs []Recommendation) string {
	keys := make([]string, 0, len(recs))
	for _, r := range recs {
		keys = append(keys, r.Provider+"/"+r.Model+":"+r.Rule)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}
