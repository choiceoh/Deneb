package server

import (
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

// promptIDSystemPersona matches pipeline/chat/prompt.PromptIDSystemPersona.
// Duplicated here so package server does not import chat/prompt solely for an ID
// constant (composition-root fan-out).
const promptIDSystemPersona = "system.persona"

func newPromptStore(denebDir string) *prompts.Store {
	return prompts.NewStore(filepath.Join(denebDir, "prompt-overrides.json"), []prompts.Template{
		{
			ID:          mailanalysis.PromptIDAutoMailAnalysis,
			Title:       "자동 메일 분석",
			Description: "LMTP/Gmail 자동 분석과 메일 상세 수동 분석이 함께 사용하는 업무 메일 분석 지침",
			Category:    "메일",
			DefaultText: mailanalysis.DefaultPrompt,
			Editable:    true,
		},
		{
			ID:          promptIDSystemPersona,
			Title:       "시스템 페르소나 (Nev 정체성·역할)",
			Description: "시스템 프롬프트 최상단의 Nev 정체성과 비서실장 역할 지침. 편집하면 다음 턴부터 반영된다.",
			Category:    "시스템",
			// Empty default: chat pipeline falls back to prompt.DefaultPersona when
			// no operator override is stored.
			DefaultText: "",
			Editable:    true,
		},
	})
}

// personaOverride returns the operator-edited 업무 persona text, or "" when there
// is no override (the chat pipeline then renders prompt.DefaultPersona). Wired
// into the chat Config as Ambient.PersonaOverride (chat_pipeline.go) so the chat
// package reads the override without importing the prompt store.
func (s *Server) personaOverride() string {
	if s == nil || s.promptStore == nil {
		return ""
	}
	text, ok := s.promptStore.OverrideText(promptIDSystemPersona)
	if !ok {
		return ""
	}
	return text
}

func (s *Server) promptOverride(id string) (string, bool) {
	if s == nil || s.promptStore == nil {
		return "", false
	}
	return s.promptStore.OverrideText(id)
}

func (s *Server) mailAnalysisPrompt() string {
	if s == nil || s.promptStore == nil {
		return mailanalysis.DefaultPrompt
	}
	return s.promptStore.Text(mailanalysis.PromptIDAutoMailAnalysis)
}
