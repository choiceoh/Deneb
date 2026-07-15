package nativesync

import "github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"

// TranscriptAppendedPayload is the compact transcript update sent to clients.
type TranscriptAppendedPayload struct {
	SessionKey  string `json:"sessionKey"`
	Role        string `json:"role"`
	Preview     string `json:"preview"`
	TimestampMs int64  `json:"timestampMs"`
}

// WorkFeedItemPayload wraps a created or updated work-feed item.
type WorkFeedItemPayload struct {
	Item workfeed.Item `json:"item"`
}

// WorkFeedActionPayload wraps a completed work-feed action.
type WorkFeedActionPayload struct {
	Item           workfeed.Item   `json:"item"`
	Action         workfeed.Action `json:"action"`
	SessionKey     string          `json:"sessionKey,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	Message        string          `json:"message,omitempty"`
	RemoveFromFeed bool            `json:"removeFromFeed,omitempty"`
}

// TranscriptAppended builds a transcript synchronization event.
func TranscriptAppended(sessionKey, role, preview string, timestampMs int64) AppendInput {
	return AppendInput{
		Type:       TypeTranscriptAppended,
		EntityID:   sessionKey,
		SessionKey: sessionKey,
		Payload: mustRawJSON(TranscriptAppendedPayload{
			SessionKey:  sessionKey,
			Role:        role,
			Preview:     preview,
			TimestampMs: timestampMs,
		}),
	}
}

// CalendarChanged signals that a locally-stored calendar event was created,
// edited, or deleted server-side (agent tool, mail-proposal accept, cron, or the
// client's own RPC). The client refetches the calendar on this event, so it
// carries no per-field payload — only the event ID for traceability.
func CalendarChanged(eventID string) AppendInput {
	return AppendInput{
		Type:     TypeCalendarChanged,
		EntityID: eventID,
	}
}

// WorkFeedCreated builds a work-feed creation event.
func WorkFeedCreated(item workfeed.Item) AppendInput {
	return workFeedItem(TypeWorkFeedCreated, item)
}

// WorkFeedUpdated builds a work-feed update event.
func WorkFeedUpdated(item workfeed.Item) AppendInput {
	return workFeedItem(TypeWorkFeedUpdated, item)
}

// WorkFeedActionRun builds a work-feed action completion event.
func WorkFeedActionRun(result workfeed.ActionResult) AppendInput {
	return AppendInput{
		Type:           TypeWorkFeedActionRun,
		EntityID:       result.Item.ID,
		SessionKey:     result.SessionKey,
		WorkFeedItemID: result.Item.ID,
		Payload: mustRawJSON(WorkFeedActionPayload{
			Item:           result.Item,
			Action:         result.Action,
			SessionKey:     result.SessionKey,
			Prompt:         result.Prompt,
			Message:        result.Message,
			RemoveFromFeed: result.RemoveFromFeed,
		}),
	}
}

func workFeedItem(typ string, item workfeed.Item) AppendInput {
	return AppendInput{
		Type:           typ,
		EntityID:       item.ID,
		SessionKey:     item.SessionKey,
		WorkFeedItemID: item.ID,
		Payload:        mustRawJSON(WorkFeedItemPayload{Item: item}),
	}
}
