// commands_handlers_btw.go — /btw side-question handler.
//
// /btw <question> answers a quick side question through the chat.btw RPC
// path. It runs in an ephemeral session (the main session's transcript is
// cloned read-only, then the ephemeral session is discarded) so nothing the
// user asks via /btw bleeds back into the main conversation context. See
// gateway-go/internal/pipeline/chat/rpc.go:HandleBtw.
package handlers

import (
	"context"
	"strings"
	"time"
)

// btwCommandTimeout caps the wall-clock for one /btw turn. The underlying
// HandleBtw uses 30s; we give a small headroom for queue + network so the
// user sees a real answer or a real timeout, not a context-deadline race.
const btwCommandTimeout = 35 * time.Second

// btwUsage is shown when /btw is invoked with no question text.
const btwUsage = "사용법: `/btw <질문>` — 메인 대화를 건드리지 않고 옆에서 빠르게 답합니다.\n\n예: `/btw 지금 환율 얼마야?`"

func handleBtwCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Deps == nil || ctx.Deps.BtwFn == nil {
		return &CommandResult{
			Reply:     "⚠️ 옆질문(BTW) 서비스를 사용할 수 없습니다.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	question := strings.TrimSpace(argRaw(ctx.Args))
	if question == "" {
		return &CommandResult{
			Reply:     btwUsage,
			SkipAgent: true,
		}, nil
	}

	// chat.btw clones the parent transcript for context, so passing the
	// active sessionKey gives the side question awareness of what was
	// just discussed without writing anything back.
	execCtx, cancel := context.WithTimeout(context.Background(), btwCommandTimeout)
	defer cancel()
	answer, err := ctx.Deps.BtwFn(execCtx, ctx.SessionKey, question)
	if err != nil {
		return &CommandResult{
			Reply:     "⚠️ 옆질문 실패: " + err.Error(),
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
	if strings.TrimSpace(answer) == "" {
		return &CommandResult{
			Reply:     "⚠️ 빈 응답을 받았습니다.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	return &CommandResult{
		Reply:     answer,
		SkipAgent: true,
	}, nil
}
