// wiki_merge.go — background worker for miniapp.memory.merge (project merge).
//
// The handler (handlerminiapp/memory.go) validates and replies "started"; this
// layer does the actual work OFF the request path so the Mini App never blocks
// on the slow part (synthesizing the combined body with the lightweight model).
// When the merge finishes — combined body written, references repointed, source
// deleted, or a lossless concatenation fallback when the model is unavailable —
// the user gets a proactive Telegram completion notice via proactiveRelay (the
// same delivery path cron uses).
//
// The deterministic structural merge lives in wiki.Store.MergePage; this file
// only adds the model call, the background goroutine, and the notification.

package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

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

// wikiMergeJobTimeout bounds the whole background merge (model synthesis +
// write). Generous on purpose: the work runs off the request path — the user
// already got an acknowledgement and will be notified on completion — so a slow
// local model gets room to finish before we fall back to concatenation, rather
// than the tight per-request budget that made the synchronous version time out.
const wikiMergeJobTimeout = 4 * time.Minute

// makeWikiMergeStarter returns the MemoryDeps.StartMerge callback. It launches
// the merge on a background goroutine (panic-guarded, cancelled at shutdown) and
// notifies the active home chat when done. Lazy field access (hub.WikiStore,
// s.modelRegistry, s.proactiveRelay) is fine — by the time a user triggers a
// merge, all are wired, even though MemoryDeps is assembled in the early phase.
func (s *Server) makeWikiMergeStarter(hub *rpcutil.GatewayHub) func(targetPath, sourcePath string, target, source *wiki.Page) {
	return func(targetPath, sourcePath string, target, source *wiki.Page) {
		safego.GoWithSlog(s.logger, "miniapp-wiki-merge", func() {
			ctx, cancel := context.WithTimeout(s.ShutdownCtx(), wikiMergeJobTimeout)
			defer cancel()

			store := hub.WikiStore()
			if store == nil {
				s.notifyMergeResult(ctx, "⚠️ 프로젝트 병합 실패\n위키 저장소를 사용할 수 없습니다.")
				return
			}

			// Slow step: combine the two bodies with the lightweight model.
			// Falls back to a lossless concatenation when the model is
			// unavailable / slow / errors, so the merge always completes.
			body, usedLLM := s.synthesizeMergeBody(ctx, target, source)

			res, err := store.MergePage(targetPath, sourcePath, body, wiki.MergeOptions{})
			if err != nil {
				s.logger.Error("miniapp wiki merge failed",
					"target", targetPath, "source", sourcePath, "error", err)
				s.notifyMergeResult(ctx, fmt.Sprintf("⚠️ 프로젝트 병합 실패\n「%s」 → 「%s」\n%v",
					titleOrPath(source, sourcePath), titleOrPath(target, targetPath), err))
				return
			}

			msg := fmt.Sprintf("✅ 프로젝트 병합 완료\n「%s」에 「%s」를 통합했습니다.",
				titleOrPath(target, targetPath), titleOrPath(source, sourcePath))
			if res.RewriteCount > 0 {
				msg += fmt.Sprintf("\n· 연결된 페이지 %d개의 링크를 새로 이었습니다.", res.RewriteCount)
			}
			if !usedLLM {
				msg += "\n· 모델이 응답하지 않아 AI 통합 대신 두 본문을 이어붙였습니다."
			}
			s.logger.Info("miniapp wiki merge done",
				"target", targetPath, "source", sourcePath,
				"rewrites", res.RewriteCount, "usedLLM", usedLLM)
			s.notifyMergeResult(ctx, msg)
		})
	}
}

// synthesizeMergeBody asks the lightweight model to combine the two bodies.
// Returns (synthesized, true) on success, or (concatenation, false) when the
// model is unavailable / slow / errors — keeping the merge lossless either way.
func (s *Server) synthesizeMergeBody(ctx context.Context, target, source *wiki.Page) (string, bool) {
	concat := concatMergeBody(target, source)
	if s.modelRegistry == nil {
		return concat, false
	}
	client := s.modelRegistry.Client(modelrole.RoleLightweight)
	model := s.modelRegistry.Model(modelrole.RoleLightweight)
	if client == nil || strings.TrimSpace(model) == "" {
		return concat, false
	}

	user := fmt.Sprintf("# 문서 A (유지): %s\n\n%s\n\n---\n\n# 문서 B (병합): %s\n\n%s",
		strings.TrimSpace(target.Meta.Title), strings.TrimSpace(target.Body),
		strings.TrimSpace(source.Meta.Title), strings.TrimSpace(source.Body))

	out, err := client.Complete(ctx, llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(wikiMergeSystemPrompt),
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens: wikiMergeMaxTokens,
	})
	if err != nil || strings.TrimSpace(out) == "" {
		if err != nil {
			s.logger.Warn("wiki merge: model synthesis failed, using concatenation", "error", err)
		}
		return concat, false
	}
	return strings.TrimSpace(out), true
}

// concatMergeBody is the lossless fallback body: target then source under a
// divider. Keeps every line of both pages.
func concatMergeBody(target, source *wiki.Page) string {
	srcTitle := strings.TrimSpace(source.Meta.Title)
	if srcTitle == "" {
		srcTitle = "병합된 페이지"
	}
	return strings.TrimSpace(
		strings.TrimSpace(target.Body) +
			"\n\n---\n\n## (병합: " + srcTitle + ")\n\n" +
			strings.TrimSpace(source.Body))
}

// notifyMergeResult delivers a completion notice to the active home chat via
// the same proactive relay cron uses. Best-effort: a delivery failure is logged
// (the merge itself already succeeded or failed on its own merits).
func (s *Server) notifyMergeResult(ctx context.Context, message string) {
	if _, err := s.proactiveRelay.relay(ctx, nativeWorkSessionKey, message); err != nil {
		s.logger.Error("wiki merge: completion notify failed", "error", err)
	}
}

// titleOrPath prefers a page's title, falling back to its path so notices are
// never blank for an untitled page.
func titleOrPath(p *wiki.Page, path string) string {
	if p != nil {
		if t := strings.TrimSpace(p.Meta.Title); t != "" {
			return t
		}
	}
	return path
}
