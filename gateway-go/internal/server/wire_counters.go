// Wire I/O counter instrumentation.
//
// wrapWireCounters decorates all chat handler wire callbacks with metrics
// tracking (call count + bytes transferred). This provides visibility into
// each wire's throughput via the monitoring.wire_stats RPC method and
// the /metrics Prometheus endpoint.
package server

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/metrics"
)

// wrapWireCounters wraps all Set*Func wire callbacks on the chat handler
// with I/O counting. Must be called after all wire callbacks are set.
func wrapWireCounters(h *chat.Handler) {
	// reply: text output to channel.
	if orig := h.ReplyFunc(); orig != nil {
		h.SetReplyFunc(func(ctx context.Context, delivery *chat.DeliveryContext, text string) error {
			metrics.WireBytesTotal.Add(int64(len(text)), "reply", "out")
			err := orig(ctx, delivery, text)
			if err != nil {
				metrics.WireCallsTotal.Inc("reply", "error")
			} else {
				metrics.WireCallsTotal.Inc("reply", "ok")
			}
			return err
		})
	}

	// media_send: file output to channel.
	if orig := h.MediaSendFunc(); orig != nil {
		h.SetMediaSendFunc(func(ctx context.Context, delivery *chat.DeliveryContext, filePath, mediaType, caption string, silent bool) error {
			err := orig(ctx, delivery, filePath, mediaType, caption, silent)
			if err != nil {
				metrics.WireCallsTotal.Inc("media_send", "error")
			} else {
				metrics.WireCallsTotal.Inc("media_send", "ok")
			}
			return err
		})
	}

	// typing: typing indicator to channel.
	if orig := h.TypingFunc(); orig != nil {
		h.SetTypingFunc(func(ctx context.Context, delivery *chat.DeliveryContext) error {
			err := orig(ctx, delivery)
			if err != nil {
				metrics.WireCallsTotal.Inc("typing", "error")
			} else {
				metrics.WireCallsTotal.Inc("typing", "ok")
			}
			return err
		})
	}

	// reaction: emoji reaction set.
	if orig := h.ReactionFunc(); orig != nil {
		h.SetReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
			err := orig(ctx, delivery, emoji)
			if err != nil {
				metrics.WireCallsTotal.Inc("reaction", "error")
			} else {
				metrics.WireCallsTotal.Inc("reaction", "ok")
			}
			return err
		})
	}

	// remove_reaction: emoji reaction removal.
	if orig := h.RemoveReactionFunc(); orig != nil {
		h.SetRemoveReactionFunc(func(ctx context.Context, delivery *chat.DeliveryContext, emoji string) error {
			err := orig(ctx, delivery, emoji)
			if err != nil {
				metrics.WireCallsTotal.Inc("remove_reaction", "error")
			} else {
				metrics.WireCallsTotal.Inc("remove_reaction", "ok")
			}
			return err
		})
	}

	// tool_progress: tool execution progress events.
	if orig := h.ToolProgressFunc(); orig != nil {
		h.SetToolProgressFunc(func(ctx context.Context, delivery *chat.DeliveryContext, event chat.ToolProgressEvent) {
			metrics.WireCallsTotal.Inc("tool_progress", "ok")
			orig(ctx, delivery, event)
		})
	}

	// draft_edit: streaming draft message sends/edits.
	if orig := h.DraftEditFunc(); orig != nil {
		h.SetDraftEditFunc(func(ctx context.Context, delivery *chat.DeliveryContext, msgID string, text string) (string, error) {
			metrics.WireBytesTotal.Add(int64(len(text)), "draft_edit", "out")
			newID, err := orig(ctx, delivery, msgID, text)
			if err != nil {
				metrics.WireCallsTotal.Inc("draft_edit", "error")
			} else {
				metrics.WireCallsTotal.Inc("draft_edit", "ok")
			}
			return newID, err
		})
	}

	// draft_delete: streaming draft message deletion.
	if orig := h.DraftDeleteFunc(); orig != nil {
		h.SetDraftDeleteFunc(func(ctx context.Context, delivery *chat.DeliveryContext, msgID string) error {
			err := orig(ctx, delivery, msgID)
			if err != nil {
				metrics.WireCallsTotal.Inc("draft_delete", "error")
			} else {
				metrics.WireCallsTotal.Inc("draft_delete", "ok")
			}
			return err
		})
	}
}
