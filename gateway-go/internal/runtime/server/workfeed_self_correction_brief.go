package server

// Korean decision brief for a self-correction card — the FALLBACK half of the
// "operator must be able to read what they approve" contract.
//
// The primary fix is upstream: every deterministic producer (health/deadcode/
// branch-rot/tool-quality/sop miners, the evolver and runtime-error drafts)
// now authors its Title/Candidate/ProposedChange/Risk in Korean, and the
// skill-lifecycle tool schema asks the model for Korean. What remains English
// is what Deneb does not author: the codebase-health bench observation copied
// verbatim into a health-finding candidate, and prose from a model that
// ignored the schema. Those records still arrive undecidable, so this renders
// a three-line Korean brief (무엇/이유/승인하면) above the record.
//
// The English original stays verbatim below it — evidence lines are file paths
// and identifiers, and a translated path is a broken one. Best-effort by
// construction: an already-Korean record skips the call entirely, and no model,
// a timeout, or a malformed answer simply yields the previous body, never a
// blocked card.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

const (
	// selfCorrectionBriefTimeout bounds the call. The card is posted from a
	// background watch, but an unbounded call there still parks a task.
	selfCorrectionBriefTimeout = 45 * time.Second
	// selfCorrectionBriefMaxTokens is generous on purpose: dual-mode models
	// spend budget on residual reasoning even with thinking disabled, and a
	// truncated brief is an empty brief (genesis LLM budget, 2026-08).
	selfCorrectionBriefMaxTokens = 1200
	// selfCorrectionBriefFieldMax bounds each English field fed to the model.
	selfCorrectionBriefFieldMax = 1200
)

const selfCorrectionBriefSystemPrompt = `너는 한국어 비서다. 아래는 Deneb의 자율 개선 루프가 만든 "자기교정 후보" 기록이며 영어로 쓰여 있다.
운영자가 승인/거절을 판단할 수 있도록 한국어로 짧게 정리하라.

형식 (이 세 줄만, 각 줄 한 문장, 머리말 그대로):
무엇: <무엇을 바꾸자는 제안인가 — 구체적으로>
이유: <왜 이 제안이 올라왔는가 — 관측된 근거>
승인하면: <승인하면 실제로 무슨 일이 일어나는가>

규칙:
- 파일 경로·심볼명·식별자는 원문 그대로 둔다 (번역·의역 금지).
- 원문에 없는 사실을 만들지 않는다. 모르는 항목은 "원문에 없음"이라고 쓴다.
- 과장·평가 없이 사실만. 세 줄 외에는 아무것도 출력하지 않는다.`

// selfCorrectionBrief renders the Korean decision brief, or "" when it cannot.
func (s *Server) selfCorrectionBrief(record genesis.SelfCorrectionCandidateRecord) string {
	// ChatManager is nil until the chat pipeline is assembled (and in tests
	// that build a bare Server) — the brief is optional, never a panic.
	if s == nil || s.ChatManager == nil || s.modelRegistry == nil {
		return ""
	}
	client := s.modelRegistry.Client(modelrole.RoleTiny)
	model := s.modelRegistry.Model(modelrole.RoleTiny)
	if client == nil || model == "" {
		return ""
	}
	source := selfCorrectionBriefSource(record)
	if source == "" || selfCorrectionAlreadyKorean(record) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), selfCorrectionBriefTimeout)
	defer cancel()
	out, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(selfCorrectionBriefSystemPrompt),
		Messages:  []llm.Message{llm.NewTextMessage("user", source)},
		MaxTokens: selfCorrectionBriefMaxTokens,
		Thinking:  &llm.ThinkingConfig{Type: "disabled"},
	})
	if err != nil {
		s.logger.Warn("self-correction 한국어 요약 실패", "id", record.ID, "error", err)
		return ""
	}
	return sanitizeSelfCorrectionBrief(out)
}

// selfCorrectionBriefSource assembles the English fields worth summarizing.
// Evidence is deliberately excluded: it is paths, counts, and ledger keys that
// read the same in both languages and would dominate the model's attention.
func selfCorrectionBriefSource(record genesis.SelfCorrectionCandidateRecord) string {
	var b strings.Builder
	add := func(label, value string) {
		if value = strings.TrimSpace(value); value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, truncateRunes(value, selfCorrectionBriefFieldMax))
		}
	}
	add("Title", record.Title)
	add("Scope", record.Scope)
	add("Skill", record.SkillName)
	add("Candidate", record.Candidate)
	add("ProposedChange", record.ProposedChange)
	add("Risk", record.Risk)
	if len(record.TargetFiles) > 0 {
		add("TargetFiles", strings.Join(record.TargetFiles, ", "))
	}
	if strings.TrimSpace(record.Candidate) == "" && strings.TrimSpace(record.Title) == "" {
		return ""
	}
	return b.String()
}

// sanitizeSelfCorrectionBrief keeps only the three contracted lines. A model
// that answered in some other shape yields "" — a malformed brief above the
// English body would confuse the decision it exists to enable.
func sanitizeSelfCorrectionBrief(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		switch {
		case strings.HasPrefix(line, "무엇:"), strings.HasPrefix(line, "이유:"), strings.HasPrefix(line, "승인하면:"):
			kept = append(kept, "- "+line)
		}
	}
	if len(kept) < 3 {
		return ""
	}
	return strings.Join(kept[:3], "\n")
}

// selfCorrectionAlreadyKorean reports whether the record's decision-bearing
// fields are already Korean, in which case a brief would restate what the card
// says. Title alone is not enough — a Korean title over an English body is
// exactly the half-translated card the operator could not act on.
func selfCorrectionAlreadyKorean(record genesis.SelfCorrectionCandidateRecord) bool {
	for _, field := range []string{record.Title, record.Candidate, record.ProposedChange} {
		if strings.TrimSpace(field) != "" && !containsHangul(field) {
			return false
		}
	}
	return containsHangul(record.Title) || containsHangul(record.Candidate)
}

func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}
