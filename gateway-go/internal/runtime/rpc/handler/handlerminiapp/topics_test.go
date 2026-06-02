package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func topicsTestDeps(m map[string]string) TopicsDeps {
	return TopicsDeps{TopicMap: func() (map[string]string, error) { return m, nil }}
}

func TestTopicsMethods_RegistersAll(t *testing.T) {
	got := TopicsMethods(topicsTestDeps(nil))
	for _, name := range []string{"miniapp.topics.list", "miniapp.topics.resolve"} {
		if _, ok := got[name]; !ok {
			t.Errorf("Methods() missing %q", name)
		}
	}
	if TopicsMethods(TopicsDeps{}) != nil {
		t.Error("nil TopicMap factory should yield nil methods")
	}
}

func TestTopicsList_DefaultWhenUnconfigured(t *testing.T) {
	h := TopicsMethods(topicsTestDeps(nil))["miniapp.topics.list"]
	got := decodePayload(t, h(authedCtx(), newReq(t, "miniapp.topics.list")))

	if got["defaultSessionKey"] != defaultNativeTopicSessionKey {
		t.Errorf("defaultSessionKey = %v, want %s", got["defaultSessionKey"], defaultNativeTopicSessionKey)
	}
	topics, _ := got["topics"].([]any)
	if len(topics) != 1 {
		t.Fatalf("len(topics) = %d, want 1: %#v", len(topics), got["topics"])
	}
	row := topics[0].(map[string]any)
	if row["key"] != "main" || row["label"] != "업무" || row["sessionKey"] != defaultNativeTopicSessionKey {
		t.Errorf("default topic mismatch: %#v", row)
	}
	if row["isDefault"] != true {
		t.Errorf("isDefault = %v, want true", row["isDefault"])
	}
}

func TestTopicsList_FromConfiguredMap(t *testing.T) {
	h := TopicsMethods(topicsTestDeps(map[string]string{
		"0":  "general",
		"42": "coding",
		"57": "personal notes",
	}))["miniapp.topics.list"]
	got := decodePayload(t, h(authedCtx(), newReq(t, "miniapp.topics.list")))

	topics, _ := got["topics"].([]any)
	byKey := map[string]map[string]any{}
	for _, item := range topics {
		row := item.(map[string]any)
		byKey[row["key"].(string)] = row
	}
	if byKey["general"]["sessionKey"] != defaultNativeTopicSessionKey {
		t.Errorf("general sessionKey = %v", byKey["general"]["sessionKey"])
	}
	if byKey["general"]["isDefault"] != true {
		t.Errorf("general isDefault = %v, want true", byKey["general"]["isDefault"])
	}
	if byKey["coding"]["label"] != "코딩" || byKey["coding"]["sessionKey"] != "client:coding" {
		t.Errorf("coding topic mismatch: %#v", byKey["coding"])
	}
	if byKey["personal notes"]["sessionKey"] != "client:personal-notes" {
		t.Errorf("personal notes sessionKey = %v", byKey["personal notes"]["sessionKey"])
	}
}

func TestTopicsResolve_ByKeyAndSessionKey(t *testing.T) {
	methods := TopicsMethods(topicsTestDeps(map[string]string{"0": "general", "42": "coding"}))

	got := decodePayload(t, methods["miniapp.topics.resolve"](
		authedCtx(),
		reqWith(t, "miniapp.topics.resolve", map[string]any{"key": "coding"}),
	))
	topic := got["topic"].(map[string]any)
	if topic["sessionKey"] != "client:coding" {
		t.Errorf("sessionKey = %v, want client:coding", topic["sessionKey"])
	}

	got = decodePayload(t, methods["miniapp.topics.resolve"](
		authedCtx(),
		reqWith(t, "miniapp.topics.resolve", map[string]any{"sessionKey": defaultNativeTopicSessionKey}),
	))
	topic = got["topic"].(map[string]any)
	if topic["key"] != "general" {
		t.Errorf("key = %v, want general", topic["key"])
	}
}

func TestTopicsResolve_NotFound(t *testing.T) {
	h := TopicsMethods(topicsTestDeps(map[string]string{"42": "coding"}))["miniapp.topics.resolve"]
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.resolve", map[string]any{"key": "missing"}))
	if resp.OK {
		t.Fatal("expected error for missing topic")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

func TestTopics_FactoryErrorUnavailable(t *testing.T) {
	methods := TopicsMethods(TopicsDeps{
		TopicMap: func() (map[string]string, error) {
			return nil, errors.New("boom")
		},
	})
	resp := methods["miniapp.topics.list"](authedCtx(), newReq(t, "miniapp.topics.list"))
	if resp.OK {
		t.Fatal("expected error for factory failure")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnavailable)
	}
}

func TestTopics_RequiresAuth(t *testing.T) {
	methods := TopicsMethods(topicsTestDeps(map[string]string{"42": "coding"}))
	for name, h := range methods {
		resp := h(context.Background(), newReq(t, name))
		if resp.OK {
			t.Errorf("%s allowed without auth", name)
		}
		if resp.Error.Code != protocol.ErrUnauthorized {
			t.Errorf("%s code = %s, want %s", name, resp.Error.Code, protocol.ErrUnauthorized)
		}
	}
}
