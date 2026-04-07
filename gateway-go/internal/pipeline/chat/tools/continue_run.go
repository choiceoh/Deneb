// continue_run.go — Tool that signals the agent wants a new run to start
// after the current one completes. Used for autonomous multi-run continuation
// when the current run's tool-call budget is nearly exhausted but the task
// is not yet complete.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolContinueRun returns a tool that sets the ContinuationSignal on the
// context, signaling that a new agent run should start after this one ends.
func ToolContinueRun() toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Reason          string `json:"reason"`
			ProgressSummary string `json:"progress_summary"`
		}
		if err := jsonutil.UnmarshalInto("continue_run params", input, &p); err != nil {
			return "", err
		}
		if p.Reason == "" {
			return "", fmt.Errorf("reason is required")
		}

		sig := toolctx.ContinuationSignalFromContext(ctx)
		if sig == nil {
			return "연속 실행을 사용할 수 없습니다 (signal not configured).", nil
		}

		// Send progress report to the user via Telegram.
		if p.ProgressSummary != "" {
			if replyFn := toolctx.ReplyFuncFromContext(ctx); replyFn != nil {
				if delivery := toolctx.DeliveryFromContext(ctx); delivery != nil {
					msg := fmt.Sprintf("📋 %s\n⏩ %s", p.ProgressSummary, p.Reason)
					_ = replyFn(ctx, delivery, msg)
				}
			}
		}

		sig.Request(p.Reason)
		return "연속 실행이 예약되었습니다. 현재 작업을 마무리하세요.", nil
	}
}
