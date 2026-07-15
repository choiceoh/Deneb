package server

import (
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
)

func newPromptStore(denebDir string) *domainbind.PromptsStore {
	return domainbind.NewPromptsStore(filepath.Join(denebDir, "prompt-overrides.json"), []domainbind.Template{
		{
			ID:          platbind.PromptIDAutoMailAnalysis,
			Title:       "자동 메일 분석",
			Description: "LMTP/Gmail 자동 분석과 메일 상세 수동 분석이 함께 사용하는 업무 메일 분석 지침",
			Category:    "메일",
			DefaultText: platbind.DefaultPrompt,
			Editable:    true,
		},
		{
			ID:          pipebind.PromptIDSystemPersona,
			Title:       "시스템 페르소나 (Nev 정체성·역할)",
			Description: "시스템 프롬프트 최상단의 Nev 정체성과 비서실장 역할 지침. 편집하면 다음 턴부터 반영된다.",
			Category:    "시스템",
			DefaultText: pipebind.DefaultPersona,
			Editable:    true,
		},
	})
}

// personaOverride returns the operator-edited 업무 persona text, or "" when there
// is no override (the chat pipeline then renders pipebind.DefaultPersona). Wired
// into the chat Config as Ambient.PersonaOverride (chat_pipeline.go) so the chat
// package reads the override without importing the prompt store.
func (s *Server) personaOverride() string {
	if s == nil || s.promptStore == nil {
		return ""
	}
	text, ok := s.promptStore.OverrideText(pipebind.PromptIDSystemPersona)
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
		return platbind.DefaultPrompt
	}
	return s.promptStore.Text(platbind.PromptIDAutoMailAnalysis)
}
