package server

import (
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

func newPromptStore(denebDir string) *prompts.Store {
	return prompts.NewStore(filepath.Join(denebDir, "prompt-overrides.json"), []prompts.Template{
		{
			ID:          gmailpoll.PromptIDAutoMailAnalysis,
			Title:       "자동 메일 분석",
			Description: "LMTP/Gmail 자동 분석과 메일 상세 수동 분석이 함께 사용하는 업무 메일 분석 지침",
			Category:    "메일",
			DefaultText: gmailpoll.DefaultPrompt,
			Editable:    true,
		},
	})
}

func (s *Server) promptOverride(id string) (string, bool) {
	if s == nil || s.promptStore == nil {
		return "", false
	}
	return s.promptStore.OverrideText(id)
}

func (s *Server) mailAnalysisPrompt() string {
	if s == nil || s.promptStore == nil {
		return gmailpoll.DefaultPrompt
	}
	return s.promptStore.Text(gmailpoll.PromptIDAutoMailAnalysis)
}
