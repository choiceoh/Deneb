package nativesync

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func TestTranscriptAppendedBuildsContract(t *testing.T) {
	in := TranscriptAppended("client:main", "assistant", "brief", 123)
	if in.Type != TypeTranscriptAppended || in.EntityID != "client:main" || in.SessionKey != "client:main" {
		t.Fatalf("metadata = %+v", in)
	}

	payload := decodePayload[TranscriptAppendedPayload](t, in.Payload)
	if payload.SessionKey != "client:main" || payload.Role != "assistant" || payload.Preview != "brief" || payload.TimestampMs != 123 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestWorkFeedCreatedBuildsContract(t *testing.T) {
	item := workfeed.Item{ID: "wf_1", SessionKey: "client:main", Title: "업무 리포트"}
	in := WorkFeedCreated(item)
	if in.Type != TypeWorkFeedCreated || in.EntityID != "wf_1" || in.SessionKey != "client:main" || in.WorkFeedItemID != "wf_1" {
		t.Fatalf("metadata = %+v", in)
	}

	payload := decodePayload[WorkFeedItemPayload](t, in.Payload)
	if payload.Item.ID != "wf_1" || payload.Item.Title != "업무 리포트" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestWorkFeedActionRunBuildsContract(t *testing.T) {
	result := workfeed.ActionResult{
		Item:           workfeed.Item{ID: "wf_2", SessionKey: "client:main"},
		Action:         workfeed.Action{ID: "ack", Kind: workfeed.ActionAck},
		SessionKey:     "client:main",
		Message:        "acked",
		RemoveFromFeed: true,
	}
	in := WorkFeedActionRun(result)
	if in.Type != TypeWorkFeedActionRun || in.EntityID != "wf_2" || in.SessionKey != "client:main" || in.WorkFeedItemID != "wf_2" {
		t.Fatalf("metadata = %+v", in)
	}

	payload := decodePayload[WorkFeedActionPayload](t, in.Payload)
	if payload.Item.ID != "wf_2" || payload.Action.ID != "ack" || payload.Message != "acked" || !payload.RemoveFromFeed {
		t.Fatalf("payload = %+v", payload)
	}
}

func decodePayload[T any](t *testing.T, payload any) T {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return out
}
