package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// llmSafetySendGuidance is the shared trailing instruction appended to every
// in-loop send-failure error returned by the message tool. Centralising it
// here keeps the four guard branches consistent and prevents drift when the
// LLM-facing wording evolves — the substring assertions in message_test.go
// pin only the *prefix* of each branch ("in-loop send is not wired" /
// "in-loop send has no delivery target"), so tweaking this suffix does not
// break tests.
//
// react branches use their own short "skip the reaction and continue"
// suffix — they do not produce a user-visible deliverable and do not need
// the longer "do not say the message failed to send" framing.
const llmSafetySendGuidance = " Do not tell the user the channel is down, do not say the message failed to send, do not ask them to retry; just produce the deliverable text and end the turn."

// autoDeliverySkipNotice is returned (with a nil error) instead of a
// send-failure error when the run is a scheduled/cron run whose final reply
// text is delivered to the user's channel automatically by the run-completion
// layer (toolctx.AutoDeliveryFromContext is true).
//
// Returning a non-error result is deliberate. A tool *error* makes the LLM
// treat the situation as a problem to surface, and it has historically
// translated an in-loop send guard failure into a user-facing "텔레그램 채널이
// 연결되지 않았다" apology — which then reached the user through that very
// channel via the cron relay, producing a self-contradicting message. By
// reporting a benign no-op, the model simply writes the final result text and
// the run-completion layer delivers it.
const autoDeliverySkipNotice = "Skipped (scheduled run): the final reply text of this run is delivered to the user's channel automatically by the run-completion path — you do not need to send it yourself. The user's channel is NOT down. Just write the final result text and end the turn."

// messageToolSchema returns the JSON Schema for the message tool.
// This is a flattened schema with per-action runtime validation, matching
// the Node.js message-tool.ts approach.

// ToolMessage implements the message tool for proactive channel sends.
// It uses context values to access the ReplyFunc and DeliveryContext.
func ToolMessage() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action    string `json:"action"`
			Message   string `json:"message"`
			To        string `json:"to"`
			Channel   string `json:"channel"`
			ReplyTo   string `json:"replyTo"`
			Emoji     string `json:"emoji"`
			MessageID string `json:"messageId"`
			Silent    bool   `json:"silent"`
		}
		if err := jsonutil.UnmarshalInto("message params", input, &p); err != nil {
			return "", err
		}
		if p.Action == "" {
			p.Action = "send"
		}

		switch p.Action {
		case "send", "reply", "thread-reply":
			if p.Message == "" {
				return "", fmt.Errorf("message is required for action=%q", p.Action)
			}

			// Get reply function from context.
			replyFn := toolctx.ReplyFuncFromContext(ctx)
			if replyFn == nil {
				// Scheduled/cron run: the deliverable is auto-delivered by the
				// run-completion layer, so an unwired in-loop send is a no-op,
				// not a failure. Report it benignly (nil error) so the model
				// does not invent an outage report. See autoDeliverySkipNotice.
				if toolctx.AutoDeliveryFromContext(ctx) {
					return autoDeliverySkipNotice, nil
				}
				// IMPORTANT — phrasing matters: the LLM reads this error
				// verbatim and historically translated "channel not connected"
				// into a Korean "텔레그램이 끊겼어요" report to the user, which
				// then *did* reach the user via the cron proactive-relay path —
				// producing a self-contradicting message ("the channel is down"
				// delivered through that same channel). The wording below tells
				// the model exactly what failed (in-loop send) and what did not
				// (the user's channel), so it does not invent an outage report.
				return "", fmt.Errorf("in-loop send is not wired in this run (no ReplyFunc on this context); this does NOT mean the user's channel is offline — the final assistant text of this run may still be delivered through the run-completion path if one is configured." + llmSafetySendGuidance)
			}

			// Build delivery context: use explicit params or fall back to current session delivery.
			delivery := toolctx.DeliveryFromContext(ctx)
			if delivery == nil {
				delivery = &toolctx.DeliveryContext{}
			}

			// Override with explicit params if provided.
			sendDelivery := &toolctx.DeliveryContext{
				Channel:   delivery.Channel,
				To:        delivery.To,
				AccountID: delivery.AccountID,
				ThreadID:  delivery.ThreadID,
			}
			if p.Channel != "" {
				sendDelivery.Channel = p.Channel
			}
			if p.To != "" {
				sendDelivery.To = p.To
			}
			if sendDelivery.Channel == "" || sendDelivery.To == "" {
				// Scheduled/cron run: same benign-no-op handling as the
				// ReplyFunc-nil branch above — the run-completion layer
				// delivers the final text, so a missing in-loop target is
				// not an outage.
				if toolctx.AutoDeliveryFromContext(ctx) {
					return autoDeliverySkipNotice, nil
				}
				// Same LLM-safety wording rationale as the ReplyFunc-nil branch
				// above: do not let the model translate this into a "channel
				// down" user-facing apology.
				return "", fmt.Errorf("in-loop send has no delivery target on this context (no Channel/To); this does NOT mean the user's channel is offline." + llmSafetySendGuidance)
			}

			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := replyFn(sendCtx, sendDelivery, p.Message); err != nil {
				return "", fmt.Errorf("message delivery failed and was not confirmed; do not claim the message is visible anywhere: %w", err)
			}
			return "Message sent successfully.", nil

		case "react":
			if p.Emoji == "" {
				return "", fmt.Errorf("emoji is required for react action")
			}
			if p.MessageID == "" {
				return "", fmt.Errorf("messageId is required for react action")
			}

			replyFn := toolctx.ReplyFuncFromContext(ctx)
			if replyFn == nil {
				return "", fmt.Errorf("in-loop reaction send is not wired in this run; the user channel itself is fine — skip the reaction and continue")
			}

			delivery := toolctx.DeliveryFromContext(ctx)
			if delivery == nil || delivery.Channel == "" || delivery.To == "" {
				return "", fmt.Errorf("in-loop reaction send has no delivery target on this context; the user channel itself is fine — skip the reaction and continue")
			}

			// Send reaction as a special marker text that the channel adapter interprets.
			reactPayload := fmt.Sprintf("__react:%s:%s", p.MessageID, p.Emoji)
			reactCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := replyFn(reactCtx, delivery, reactPayload); err != nil {
				return "", fmt.Errorf("reaction delivery failed and was not confirmed: %w", err)
			}
			return fmt.Sprintf("Reaction %s sent to message %s.", p.Emoji, p.MessageID), nil

		default:
			return fmt.Sprintf("Unknown message action: %q. Supported: send, reply, react.", p.Action), nil
		}
	}
}
