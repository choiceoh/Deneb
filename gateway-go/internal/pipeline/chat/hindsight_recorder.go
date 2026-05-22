// hindsight_recorder.go is the write path for the Hindsight memory provider.
// It retains completed conversation turns into the Hindsight bank so future
// sessions can recall them. The write is fire-and-forget: a turn's memory
// value never justifies blocking the user's reply, and the Hindsight server
// ingests asynchronously anyway.
package chat

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/hindsight"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// hindsightRetainTimeout bounds the background retain request.
	hindsightRetainTimeout = 15 * time.Second
	// hindsightMaxTurnChars caps each side of a retained turn so a runaway
	// message cannot push a huge document into the memory bank.
	hindsightMaxTurnChars = 6000
)

// retainTurnToHindsight stores the just-completed turn into the memory bank in
// a background goroutine. Safe to call with a nil client or with the write
// path disabled — both make it a no-op.
func retainTurnToHindsight(client *hindsight.Client, params RunParams, assistantText string, logger *slog.Logger) {
	if !client.RetainEnabled() {
		return
	}
	item, ok := buildHindsightRetainItem(params, assistantText)
	if !ok {
		return
	}
	safego.GoWithSlog(logger, "hindsight-retain", func() {
		// Post-run: the request context is already done, so derive a fresh
		// bounded context. The Hindsight server ingests asynchronously, so
		// this only covers the time to hand the document off.
		ctx, cancel := context.WithTimeout(context.Background(), hindsightRetainTimeout)
		defer cancel()
		if err := client.Retain(ctx, []hindsight.RetainItem{item}); err != nil && logger != nil {
			logger.Warn("hindsight retain failed", "session", params.SessionKey, "error", err)
		}
	})
}

// buildHindsightRetainItem renders a completed turn into a single memory item.
// Returns ok=false when the turn has nothing worth storing. The turn is
// grouped under document_id=SessionKey so the bank can relate a session's
// turns to each other.
func buildHindsightRetainItem(params RunParams, assistantText string) (hindsight.RetainItem, bool) {
	userMsg := truncateRecallText(params.Message, hindsightMaxTurnChars)
	assistant := truncateRecallText(assistantText, hindsightMaxTurnChars)
	if userMsg == "" || assistant == "" {
		return hindsight.RetainItem{}, false
	}

	metadata := map[string]string{
		"source":      "deneb",
		"retained_at": strconv.FormatInt(time.Now().Unix(), 10),
	}
	if params.Delivery != nil && params.Delivery.Channel != "" {
		metadata["channel"] = params.Delivery.Channel
	}

	return hindsight.RetainItem{
		Content:    "User: " + userMsg + "\n\nAssistant: " + assistant,
		Context:    "Deneb conversation turn",
		DocumentID: params.SessionKey,
		Metadata:   metadata,
	}, true
}
