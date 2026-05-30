// wiki_merge.go — wiring for miniapp.memory.merge (project/page merge).
//
// The handler (handlerminiapp/memory.go) does the deterministic structural
// merge via wiki.Store.MergePage; this layer supplies the one piece that needs
// a model — synthesizing a single combined body from the two page bodies. It
// runs a direct, non-streaming lightweight-model call (not the chat pipeline),
// so it has no chatHandler dependency and can be wired in the early phase.
//
// Body synthesis is best-effort: makeWikiMergeBodies returns a closure that may
// error (no model configured, request fails), and the handler falls back to a
// labeled concatenation in that case so the merge always completes.

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// errMergeNoLLM surfaces from the merge body synthesizer when no lightweight
// model is configured. The handler treats a synthesis error as "use the
// concatenation fallback", so this never blocks a merge.
var errMergeNoLLM = errors.New("lightweight model not configured for wiki merge")

const wikiMergeSystemPrompt = `너는 두 개의 위키 문서를 하나로 통합하는 편집자다.
같은 주제를 다룬 두 문서(A=유지, B=병합)가 주어진다. 정보 손실 없이 하나의 깔끔한 마크다운 문서로 합쳐라.

규칙:
- 중복되는 내용은 한 번만 남겨라. 두 문서가 상충하면 양쪽을 모두 보존하되 차이를 분명히 드러내라.
- 사실·숫자·날짜·이름은 그대로 유지하라. 없는 내용을 지어내지 마라.
- 마크다운 구조(제목, 목록, 표)를 유지하고 논리적으로 재배치하라.
- frontmatter(--- 사이의 메타데이터)는 출력하지 마라. 본문 마크다운만 출력하라.
- "다음은…" 같은 서두나 맺음말 없이 통합된 본문만 출력하라.`

// wikiMergeMaxTokens caps the synthesized body. Project pages are reference
// notes, not essays; this comfortably holds two merged pages.
const wikiMergeMaxTokens = 4096

// makeWikiMergeBodies returns the MergeBodies callback wired into MemoryDeps.
// It resolves the lightweight model per-call (lazy) so a gateway started before
// the model registry is ready, or with no provider configured, simply yields
// errMergeNoLLM and the handler falls back to concatenation.
func (s *Server) makeWikiMergeBodies() func(context.Context, string, string, string, string) (string, error) {
	return func(ctx context.Context, targetTitle, targetBody, sourceTitle, sourceBody string) (string, error) {
		if s.modelRegistry == nil {
			return "", errMergeNoLLM
		}
		client := s.modelRegistry.Client(modelrole.RoleLightweight)
		model := s.modelRegistry.Model(modelrole.RoleLightweight)
		if client == nil || strings.TrimSpace(model) == "" {
			return "", errMergeNoLLM
		}

		user := fmt.Sprintf("# 문서 A (유지): %s\n\n%s\n\n---\n\n# 문서 B (병합): %s\n\n%s",
			strings.TrimSpace(targetTitle), strings.TrimSpace(targetBody),
			strings.TrimSpace(sourceTitle), strings.TrimSpace(sourceBody))

		return client.Complete(ctx, llm.ChatRequest{
			Model:     model,
			System:    llm.SystemString(wikiMergeSystemPrompt),
			Messages:  []llm.Message{llm.NewTextMessage("user", user)},
			MaxTokens: wikiMergeMaxTokens,
		})
	}
}
