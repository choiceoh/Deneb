package nativesync

import "github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"

type TranscriptAppendedPayload struct {
	SessionKey  string `json:"sessionKey"`
	Role        string `json:"role"`
	Preview     string `json:"preview"`
	TimestampMs int64  `json:"timestampMs"`
}

type WorkFeedItemPayload struct {
	Item workfeed.Item `json:"item"`
}

type WorkFeedActionPayload struct {
	Item           workfeed.Item   `json:"item"`
	Action         workfeed.Action `json:"action"`
	SessionKey     string          `json:"sessionKey,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	Message        string          `json:"message,omitempty"`
	RemoveFromFeed bool            `json:"removeFromFeed,omitempty"`
}

func TranscriptAppended(sessionKey, role, preview string, timestampMs int64) AppendInput {
	return AppendInput{
		Type:       TypeTranscriptAppended,
		EntityID:   sessionKey,
		SessionKey: sessionKey,
		Payload: TranscriptAppendedPayload{
			SessionKey:  sessionKey,
			Role:        role,
			Preview:     preview,
			TimestampMs: timestampMs,
		},
	}
}

func WorkFeedCreated(item workfeed.Item) AppendInput {
	return workFeedItem(TypeWorkFeedCreated, item)
}

func WorkFeedUpdated(item workfeed.Item) AppendInput {
	return workFeedItem(TypeWorkFeedUpdated, item)
}

func WorkFeedActionRun(result workfeed.ActionResult) AppendInput {
	return AppendInput{
		Type:           TypeWorkFeedActionRun,
		EntityID:       result.Item.ID,
		SessionKey:     result.SessionKey,
		WorkFeedItemID: result.Item.ID,
		Payload: WorkFeedActionPayload{
			Item:           result.Item,
			Action:         result.Action,
			SessionKey:     result.SessionKey,
			Prompt:         result.Prompt,
			Message:        result.Message,
			RemoveFromFeed: result.RemoveFromFeed,
		},
	}
}

func workFeedItem(typ string, item workfeed.Item) AppendInput {
	return AppendInput{
		Type:           typ,
		EntityID:       item.ID,
		SessionKey:     item.SessionKey,
		WorkFeedItemID: item.ID,
		Payload:        WorkFeedItemPayload{Item: item},
	}
}
