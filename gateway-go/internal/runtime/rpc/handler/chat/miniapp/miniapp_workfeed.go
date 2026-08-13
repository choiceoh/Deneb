package miniapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// handleMiniappWorkfeedFeedback records a user's correction on a work-feed card
// and runs one agent turn to reconcile the durable knowledge — the native
// client's "long-press a feed card → 정정·피드백" path, where the user teaches the
// agent something it got wrong or didn't know. Two effects, by design (both):
//
//  1. The card is annotated in place with the user's verbatim correction (an
//     on-card erratum), so the wrong analysis is never shown unqualified. This
//     happens first, so the correction is never lost even if the turn below fails.
//  2. One agent turn — with the wiki tool — updates the durable knowledge base
//     (인물/프로젝트/거래처/시스템 pages) so future analysis and recall reflect the fix.
//
// The turn runs ephemeral (EphemeralUser+EphemeralAssistant): a correction made
// from the feed must not land as visible messages in the client:main chat
// transcript, but the wiki write (a tool side effect) still persists.
//
// Params:
//   - itemId   (string, required): the work-feed card id
//   - feedback (string, required): the user's correction / teaching text
func handleMiniappWorkfeedFeedback(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ItemID   string `json:"itemId"`
			Feedback string `json:"feedback"`
		}](req)
		if errResp != nil {
			return errResp
		}
		itemID := strings.TrimSpace(p.ItemID)
		feedback := strings.TrimSpace(p.Feedback)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if feedback == "" {
			return rpcerr.MissingParam("feedback").Response(req.ID)
		}
		// Locate the card so the turn can reconcile against the exact analysis the
		// user is correcting. List(0, true) returns every retained item (no limit,
		// includes acked/snoozed) — the card may have been acked before correcting.
		card, found := findWorkFeedItem(deps, itemID)
		if !found {
			return rpcerr.NotFound("work feed item").Response(req.ID)
		}
		// 1) Annotate the card immediately (built from the pre-correction card body
		// below, so the turn still sees the original analysis).
		updated, cerr := deps.WorkFeed.Correct(itemID, feedback)
		if cerr != nil {
			return rpcerr.WrapDependencyFailed("work feed correct failed", cerr).Response(req.ID)
		}
		sessionKey := chatport.DefaultNativeSessionKey(card.SessionKey)
		// 2) One agent turn updates the durable knowledge (wiki) from the correction.
		message := buildWorkfeedFeedbackMessage(card, feedback)
		res, serr := deps.Chat.RunSync(ctx, chatport.SyncRequest{
			SessionKey:          sessionKey,
			Message:             message,
			Delivery:            &chatport.DeliveryContext{Channel: chatport.NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			// A feed correction is a side action, not a chat message — keep it out of
			// the client:main transcript (the wiki write still persists).
			EphemeralUser:      true,
			EphemeralAssistant: true,
			// The card body can carry untrusted mail/doc content: block exec/gmail send.
			GateUntrustedTools: true,
		})
		if serr != nil {
			// The card annotation already succeeded; surface the knowledge-turn
			// failure softly but still return the annotated item so the client
			// reflects the on-card correction.
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":         true,
				"item":       updated,
				"text":       "정정 내용을 카드에 반영했습니다. (지식 업데이트는 일시적으로 실패했어요.)",
				"sessionKey": sessionKey,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"item":       updated,
			"text":       res.BestText,
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// buildWorkfeedFeedbackMessage assembles the one-turn instruction: take the user's
// correction as ground truth, fix the durable wiki knowledge, and report briefly.
// The card's on-card erratum is handled by the store, so the turn is told not to
// repeat it.
func buildWorkfeedFeedbackMessage(card workfeed.Item, feedback string) string {
	var b strings.Builder
	b.WriteString("사용자가 아래 업무 피드 카드의 분석 내용에 대해 정정·보강 피드백을 보냈다. ")
	b.WriteString("[사용자 피드백]이 사용자가 직접 알려준 정확한 지식이니 사실로 받아들여라.\n\n")
	b.WriteString("할 일:\n")
	b.WriteString("1. 관련 위키 지식(인물·프로젝트·거래처·시스템 등)을 wiki 도구로 정정하거나 보강하라. ")
	b.WriteString("기존 페이지가 있으면 고치고 없으면 적절한 카테고리에 새로 만들되, 바뀐 사실을 정확히 반영하고 ")
	b.WriteString("출처가 '사용자 직접 정정(업무 피드 피드백)'임을 남겨라.\n")
	b.WriteString("2. 무엇을 어떻게 반영했는지 한국어로 1~3줄로 간단히 보고하라. ")
	b.WriteString("(이 카드 자체의 정정 표기는 시스템이 이미 처리했으니 다시 하지 마라.)\n\n")
	writeOriginalWorkFeedCard(&b, card)
	b.WriteString("\n## 사용자 피드백\n")
	b.WriteString(feedback)
	return b.String()
}

// handleMiniappWorkfeedRewrite regenerates a work-feed card's analysis and
// replaces the card body in place — the native "다시 작성" path. One ephemeral
// agent turn rewrites the analysis from the card's current content; its reply
// becomes the new body (title/summary stay, so the row preview is stable). The
// turn is ephemeral so the rewrite never lands in the client:main transcript; a
// blank rewrite is rejected so a failed turn never wipes the card.
//
// Params:
//   - itemId (string, required): the work-feed card id
func handleMiniappWorkfeedRewrite(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ItemID string `json:"itemId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		itemID := strings.TrimSpace(p.ItemID)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		card, found := findWorkFeedItem(deps, itemID)
		if !found {
			return rpcerr.NotFound("work feed item").Response(req.ID)
		}
		sessionKey := chatport.DefaultNativeSessionKey(card.SessionKey)
		message := buildWorkfeedRewriteMessage(card)
		res, serr := deps.Chat.RunSync(ctx, chatport.SyncRequest{
			SessionKey:          sessionKey,
			Message:             message,
			Delivery:            &chatport.DeliveryContext{Channel: chatport.NativeClientChannel, To: sessionKey},
			AutoDeliveredOutput: true,
			EphemeralUser:       true,
			EphemeralAssistant:  true,
			GateUntrustedTools:  true,
		})
		if serr != nil {
			return rpcerr.WrapDependencyFailed("chat send failed", serr).Response(req.ID)
		}
		newBody := strings.TrimSpace(res.BestText)
		if newBody == "" {
			// Never wipe the card on an empty regeneration; report softly.
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":         true,
				"item":       card,
				"text":       "다시 작성에 실패했어요. 카드는 그대로 두었습니다.",
				"sessionKey": sessionKey,
			})
		}
		updated, rerr := deps.WorkFeed.Rewrite(itemID, newBody)
		if rerr != nil {
			return rpcerr.WrapDependencyFailed("work feed rewrite failed", rerr).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":         true,
			"item":       updated,
			"text":       "카드를 다시 작성했습니다.",
			"model":      res.Model,
			"sessionKey": sessionKey,
		})
	}
}

// buildWorkfeedRewriteMessage instructs the turn to regenerate the card's analysis
// from its current content — same facts, clearer structure — and to output ONLY the
// rewritten body (no preamble) so the reply can be stored directly as the new card.
func buildWorkfeedRewriteMessage(card workfeed.Item) string {
	var b strings.Builder
	b.WriteString("아래 업무 피드 카드의 분석을 다시 작성하라. 같은 사실·정보를 기반으로 하되, ")
	b.WriteString("더 명확하고 정돈된 구조로 — 핵심 상황, 근거·숫자, 지금 할 다음 행동이 잘 드러나게 한국어로 다시 써라. ")
	b.WriteString("필요하면 wiki 등 도구로 맥락을 보강해도 좋다. ")
	b.WriteString("출력은 **다시 쓴 분석 본문만** 내라 — '다시 작성했습니다' 같은 머리말·맺음말이나 메타 설명 없이 본문 마크다운만.\n\n")
	writeOriginalWorkFeedCard(&b, card)
	return b.String()
}

func findWorkFeedItem(deps Deps, itemID string) (workfeed.Item, bool) {
	items, _, err := deps.WorkFeed.List(0, true)
	if err != nil {
		return workfeed.Item{}, false
	}
	for _, item := range items {
		if item.ID == itemID {
			return item, true
		}
	}
	return workfeed.Item{}, false
}

func writeOriginalWorkFeedCard(builder *strings.Builder, card workfeed.Item) {
	builder.WriteString("## 원본 카드\n")
	if t := strings.TrimSpace(card.Title); t != "" {
		builder.WriteString("제목: ")
		builder.WriteString(t)
		builder.WriteByte('\n')
	}
	if body := strings.TrimSpace(card.Body); body != "" {
		builder.WriteString(body)
		builder.WriteByte('\n')
	} else if s := strings.TrimSpace(card.Summary); s != "" {
		builder.WriteString(s)
		builder.WriteByte('\n')
	}
}

// contactsSummary renders a short Korean summary of an address-book sync for the
// native client to show inline. The store save is the headline; wiki enrichment,
// when any people were updated, is appended as a parenthetical bonus.
func contactsSummary(saved int, enrich wiki.ContactEnrichResult) string {
	msg := fmt.Sprintf("📇 주소록 %d개를 저장했습니다. 이제 '이 번호 누구?' 검색과 회의 전사 고유명사 교정에 활용됩니다.", saved)
	if enrich.Updated == 0 {
		return msg
	}
	const maxShown = 6
	shown := enrich.Names
	extra := 0
	if len(shown) > maxShown {
		extra = len(shown) - maxShown
		shown = shown[:maxShown]
	}
	tail := ""
	if extra > 0 {
		tail = fmt.Sprintf(" 외 %d명", extra)
	}
	return msg + fmt.Sprintf(" (위키 인물 %d명 보강: %s%s)", enrich.Updated, strings.Join(shown, ", "), tail)
}
