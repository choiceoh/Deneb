// slash_model_info.go — /model with no arguments renders the model line-up:
// current model, role table with health markers, capability/profile summary,
// auto-tuning state, and the latest scorecard window for the current model.
// All of it reads the capability/profile/health layers introduced with the
// model registry work, so the operator can see "what model am I on and how is
// it doing" without grepping logs.
package chat

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
)

// renderModelInfo builds the /model (no args) reply.
func (h *Handler) renderModelInfo() string {
	if h.registry == nil {
		return "모델 레지스트리가 초기화되지 않았습니다."
	}
	current := h.DefaultModel()
	if current == "" {
		current = h.registry.FullModelID(modelrole.RoleMain)
	}
	providerID, modelName := modelrole.ParseModelID(current)

	var b strings.Builder
	fmt.Fprintf(&b, "🤖 **현재 모델:** %s\n", current)

	b.WriteString("\n**역할:**\n")
	for _, role := range []modelrole.Role{
		modelrole.RoleMain, modelrole.RoleTiny, modelrole.RoleLightweight,
		modelrole.RoleAnalysis, modelrole.RoleFallback,
	} {
		fid := h.registry.FullModelID(role)
		marker := ""
		if fid == current {
			marker = " ←"
		}
		health := ""
		if h.registry.ModelUnhealthy(h.registry.Model(role)) {
			health = " ⚠️연속실패"
		}
		fmt.Fprintf(&b, "- %s: %s%s%s\n", string(role), fid, health, marker)
	}

	if attrs := modelAttributes(h.registry, providerID, modelName); len(attrs) > 0 {
		b.WriteString("\n**특성:** " + strings.Join(attrs, " · ") + "\n")
	}

	appendScorecardSummary(&b, modelName)

	b.WriteString("\n변경: `/model <model-id|역할명>`")
	return b.String()
}

// modelAttributes summarizes the capability + profile + tuning layers for one
// model. Only attributes that deviate from the permissive default are shown.
func modelAttributes(reg *modelrole.Registry, providerID, modelName string) []string {
	caps := reg.CapabilityForModel(providerID, modelName)
	prof := reg.ProfileForModel(providerID, modelName)

	var attrs []string
	if caps.ContextWindow > 0 {
		attrs = append(attrs, fmt.Sprintf("컨텍스트 %dK", caps.ContextWindow/1024))
	}
	if caps.Reasoning {
		attrs = append(attrs, "reasoning 엔드포인트")
	} else if prof.Reasoning {
		attrs = append(attrs, "reasoning 채널")
	}
	if caps.RejectsCacheControl {
		attrs = append(attrs, "프롬프트 캐시 미지원")
	}
	if caps.NoVision {
		attrs = append(attrs, "vision 비활성")
	}
	if prof.Temperature != nil {
		attrs = append(attrs, fmt.Sprintf("temp %.1f", *prof.Temperature))
	}
	if floor := reg.TunedMaxTokens(modelName); floor > 0 {
		attrs = append(attrs, fmt.Sprintf("출력 floor %d (자동 튜닝)", floor))
	}
	return attrs
}

// appendScorecardSummary adds the latest tuner window for modelName, plus any
// open recommendations. Silent when no scorecard exists yet (tuner has not
// completed a cycle) or the model has no recorded runs.
func appendScorecardSummary(b *strings.Builder, modelName string) {
	sc := modeltuner.LoadScorecard(modeltuner.DefaultStatePath())
	if sc.GeneratedAtMs == 0 {
		return
	}
	for _, m := range sc.Models {
		if m.Model != modelName || m.Runs == 0 {
			continue
		}
		line := fmt.Sprintf("\n**최근 %dh:** %d런 · p95 %.0f초", sc.WindowHours, m.Runs, float64(m.P95Ms)/1000)
		if denom := m.CacheReadTokens + m.InputTokens; denom > 0 && m.CacheCreationTokens > 0 {
			line += fmt.Sprintf(" · 캐시 히트 %.0f%%", float64(m.CacheReadTokens)/float64(denom)*100)
		}
		if m.FallbackRuns > 0 {
			line += fmt.Sprintf(" · 폴백 %d회", m.FallbackRuns)
		}
		if m.TimeoutRuns > 0 {
			line += fmt.Sprintf(" · 스톨 %d회", m.TimeoutRuns)
		}
		b.WriteString(line + "\n")
		break
	}
	var open []string
	for _, r := range sc.Recommendations {
		if r.Model == modelName {
			open = append(open, "- "+r.Message)
		}
	}
	if len(open) > 0 {
		b.WriteString("\n**튜너 권고:**\n" + strings.Join(open, "\n") + "\n")
	}
}
